package bcl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type ExportOptions struct {
	Fields []string
	Redact []string
	Format string
}

func Export(n *Normalized, opts ExportOptions) ([]byte, error) {
	var v any = n
	if len(opts.Fields) > 0 {
		v = selectFields(normalizedMap(n), opts.Fields)
	}
	if len(opts.Redact) > 0 {
		v = redactPaths(v, opts.Redact)
	}
	switch opts.Format {
	case "yaml", "yml":
		return MarshalYAML(v), nil
	default:
		return json.MarshalIndent(v, "", "  ")
	}
}

func MarshalYAML(v any) []byte {
	var b bytes.Buffer
	writeYAML(&b, v, 0)
	return b.Bytes()
}

func GenerateGoTypes(doc *Document, packageName string) ([]byte, error) {
	if packageName == "" {
		packageName = "config"
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "package %s\n\n", packageName)
	for _, n := range doc.Items {
		s, ok := n.(*SchemaDecl)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "type %s struct {\n", exportName(s.Name))
		for _, f := range s.Fields {
			fmt.Fprintf(&b, "\t%s %s `json:%q bcl:%q`\n", exportName(f.Name), goType(f.Type), f.Name, f.Name)
		}
		b.WriteString("}\n\n")
	}
	return b.Bytes(), nil
}

func GenerateDocs(doc *Document) []byte {
	var b bytes.Buffer
	b.WriteString("# BCL Reference\n\n")
	for _, n := range doc.Items {
		switch x := n.(type) {
		case *SchemaDecl:
			fmt.Fprintf(&b, "## Schema `%s`\n\n", x.Name)
			if x.Description != "" {
				fmt.Fprintf(&b, "%s\n\n", x.Description)
			}
			if x.Command != nil {
				b.WriteString("Command schema")
				var parts []string
				if x.Command.Kind != "" {
					parts = append(parts, "kind `"+x.Command.Kind+"`")
				}
				if x.Command.Phase != "" {
					parts = append(parts, "phase `"+x.Command.Phase+"`")
				}
				if x.Command.Repeatable {
					parts = append(parts, "repeatable")
				}
				if len(parts) > 0 {
					b.WriteString(": " + strings.Join(parts, ", "))
				}
				b.WriteString(".\n\n")
				if len(x.Command.AllowedChildren) > 0 {
					fmt.Fprintf(&b, "- Child commands: `%s`\n", strings.Join(x.Command.AllowedChildren, "`, `"))
				}
				if len(x.Command.RequiredChildren) > 0 {
					fmt.Fprintf(&b, "- Required child commands: `%s`\n", strings.Join(x.Command.RequiredChildren, "`, `"))
				}
				if len(x.Command.AllowedChildren) > 0 || len(x.Command.RequiredChildren) > 0 {
					b.WriteByte('\n')
				}
			}
			for _, f := range x.Fields {
				req := "optional"
				if f.Required {
					req = "required"
				}
				fmt.Fprintf(&b, "- `%s` `%s` %s\n", f.Name, f.Type, req)
			}
			for _, ex := range x.Examples {
				fmt.Fprintf(&b, "- Example: `%v`\n", ex.ToInterface(false))
			}
			b.WriteByte('\n')
		case *TypeDecl:
			fmt.Fprintf(&b, "- Type `%s = %s`\n", x.Name, x.Type)
		case *Block:
			fmt.Fprintf(&b, "## Block `%s", x.Type)
			if x.ID != "" {
				fmt.Fprintf(&b, " %s", x.ID)
			}
			b.WriteString("`\n\n")
		}
	}
	return b.Bytes()
}

func MigrateDocument(doc *Document, targetVersion string) (*Document, []Diagnostic) {
	var diags []Diagnostic
	if targetVersion == "" {
		targetVersion = "1.0"
	}
	var found bool
	for _, n := range doc.Items {
		if b, ok := n.(*Block); ok && b.Type == "bcl" {
			found = true
			setBlockField(b, "version", &Literal{Type: "string", Data: targetVersion, Span: b.Span})
		}
	}
	if !found {
		doc.Items = append([]Node{&Block{Type: "bcl", Body: []Node{
			&Assignment{Name: "version", Value: &Literal{Type: "string", Data: targetVersion}},
			&Assignment{Name: "strict", Value: &Literal{Type: "bool", Data: true}},
		}, Span: doc.Span}}, doc.Items...)
		diags = append(diags, Diagnostic{Severity: "info", Message: "added bcl version declaration", Span: doc.Span})
	}
	return doc, diags
}

type FetchOptions struct {
	CacheDir string
	Offline  bool
}

func FetchRemoteModules(lock *Lockfile, opts FetchOptions) error {
	if lock == nil {
		return nil
	}
	if opts.CacheDir == "" {
		opts.CacheDir = filepath.Join(os.TempDir(), "bcl-mod-cache")
	}
	for _, entry := range lock.Modules {
		switch entry.Kind {
		case "git":
			if opts.Offline {
				return fmt.Errorf("offline mode: cannot fetch git module %s", entry.Source)
			}
			target := filepath.Join(opts.CacheDir, safePath(entry.Source))
			if _, err := os.Stat(target); err == nil {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			source := strings.TrimPrefix(entry.Resolved, "git::")
			if source == "" {
				source = strings.TrimPrefix(entry.Source, "git::")
			}
			if err := exec.Command("git", "clone", source, target).Run(); err != nil {
				return err
			}
			if entry.Revision != "" {
				if err := exec.Command("git", "-C", target, "checkout", entry.Revision).Run(); err != nil {
					return err
				}
			}
		case "registry":
			if opts.Offline {
				return fmt.Errorf("offline mode: cannot fetch registry module %s", entry.Source)
			}
			resp, err := http.Get(entry.Source)
			if err != nil {
				return err
			}
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				return fmt.Errorf("registry fetch %s failed: %s", entry.Source, resp.Status)
			}
		}
	}
	return nil
}

func normalizedMap(n *Normalized) map[string]any {
	m := make(map[string]any, 11)
	if n == nil {
		return m
	}
	if n.Version != "" {
		m["version"] = n.Version
	}
	if len(n.Body) > 0 {
		m["body"] = n.Body
	}
	if len(n.Blocks) > 0 {
		m["blocks"] = n.Blocks
	}
	if len(n.Constants) > 0 {
		m["constants"] = n.Constants
	}
	if len(n.Sets) > 0 {
		m["sets"] = n.Sets
	}
	if len(n.Types) > 0 {
		m["types"] = n.Types
	}
	if len(n.Imports) > 0 {
		m["imports"] = n.Imports
	}
	if len(n.Modules) > 0 {
		m["modules"] = n.Modules
	}
	if len(n.Namespaces) > 0 {
		m["namespaces"] = n.Namespaces
	}
	if len(n.Schemas) > 0 {
		m["schemas"] = n.Schemas
	}
	if len(n.Diagnostics) > 0 {
		m["diagnostics"] = n.Diagnostics
	}
	return m
}

func selectFields(m map[string]any, fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, path := range fields {
		if v, ok := getPath(m, path); ok {
			setPath(out, path, v)
		}
	}
	return out
}

func redactPaths(v any, paths []string) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	cp := cloneMap(m)
	for _, path := range paths {
		setPath(cp, path, "****")
	}
	return cp
}

func writeYAML(b *bytes.Buffer, v any, indent int) {
	pad := strings.Repeat("  ", indent)
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(b, "%s%s:", pad, k)
			if isScalarYAML(x[k]) {
				b.WriteByte(' ')
				writeYAMLScalar(b, x[k])
				b.WriteByte('\n')
			} else {
				b.WriteByte('\n')
				writeYAML(b, x[k], indent+1)
			}
		}
	case []any:
		for _, item := range x {
			fmt.Fprintf(b, "%s-", pad)
			if isScalarYAML(item) {
				b.WriteByte(' ')
				writeYAMLScalar(b, item)
				b.WriteByte('\n')
			} else {
				b.WriteByte('\n')
				writeYAML(b, item, indent+1)
			}
		}
	default:
		b.WriteString(pad)
		writeYAMLScalar(b, x)
		b.WriteByte('\n')
	}
}

func writeYAMLScalar(b *bytes.Buffer, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case string:
		writeYAMLString(b, x)
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	default:
		fmt.Fprintf(b, "%v", x)
	}
}

func writeYAMLString(b *bytes.Buffer, s string) {
	if s == "" || needsYAMLQuote(s) {
		encoded, _ := json.Marshal(s)
		b.Write(encoded)
		return
	}
	b.WriteString(s)
}

func needsYAMLQuote(s string) bool {
	switch s {
	case "null", "true", "false", "~":
		return true
	}
	for i, r := range s {
		switch r {
		case ':', '#', '\n', '\r', '\t', '"', '\'', '[', ']', '{', '}', ',', '&', '*', '!', '|', '>', '@', '`':
			return true
		case ' ':
			if i == 0 || i == len(s)-1 {
				return true
			}
		}
	}
	return false
}

func isScalarYAML(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return false
	default:
		return true
	}
}

func getPath(m map[string]any, path string) (any, bool) {
	var cur any = m
	for _, part := range strings.Split(path, ".") {
		next, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = next[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func setPath(m map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	cur := m
	for _, part := range parts[:len(parts)-1] {
		next, ok := cur[part].(map[string]any)
		if !ok {
			next = make(map[string]any, 1)
			cur[part] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = value
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if child, ok := v.(map[string]any); ok {
			out[k] = cloneMap(child)
		} else {
			out[k] = v
		}
	}
	return out
}

func exportName(s string) string {
	var b strings.Builder
	upperNext := true
	for _, r := range s {
		if r == '_' || r == '-' || r == '.' {
			upperNext = true
			continue
		}
		if upperNext && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		b.WriteRune(r)
		upperNext = false
	}
	return b.String()
}

func goType(t string) string {
	switch {
	case strings.HasPrefix(t, "list"):
		return "[]any"
	case strings.HasPrefix(t, "map"), t == "object":
		return "map[string]any"
	case t == "int":
		return "int64"
	case t == "float":
		return "float64"
	case t == "bool":
		return "bool"
	default:
		return "string"
	}
}

func setBlockField(b *Block, name string, value Value) {
	for _, n := range b.Body {
		if a, ok := n.(*Assignment); ok && a.Name == name {
			a.Value = value
			return
		}
	}
	b.Body = append(b.Body, &Assignment{Name: name, Value: value})
}

func safePath(s string) string {
	s = strings.NewReplacer("/", "_", ":", "_", "\\", "_").Replace(s)
	return strings.Trim(s, "_")
}
