package bcl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
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

func ExportJSONSchema(normalized *Normalized, schemaName string) ([]byte, error) {
	if normalized == nil || normalized.Schemas == nil {
		return nil, fmt.Errorf("schema %q not found", schemaName)
	}
	raw := normalized.Schemas[schemaName]
	if raw == nil {
		return nil, fmt.Errorf("schema %q not found", schemaName)
	}
	out := bclSchemaToJSONSchema(schemaName, raw)
	return json.MarshalIndent(out, "", "  ")
}

func ExportOpenAPIComponents(normalized *Normalized) ([]byte, error) {
	if normalized == nil {
		return nil, fmt.Errorf("normalized document is nil")
	}
	schemas := map[string]any{}
	for _, name := range sortedSchemaNames(normalized.Schemas) {
		schemas[name] = bclSchemaToJSONSchema(name, normalized.Schemas[name])
	}
	return json.MarshalIndent(map[string]any{"components": map[string]any{"schemas": schemas}}, "", "  ")
}

func ImportJSONSchema(name string, data []byte) (*SchemaDecl, []Diagnostic) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, []Diagnostic{{Severity: "error", Message: err.Error()}}
	}
	if name == "" {
		name = strings.TrimPrefix(scalarString(raw["title"]), "#/")
	}
	if name == "" {
		name = "imported"
	}
	decl := &SchemaDecl{Name: name}
	for _, field := range jsonSchemaPropertiesToFields(raw) {
		decl.Fields = append(decl.Fields, field)
	}
	return decl, nil
}

func bclSchemaToJSONSchema(name string, raw any) map[string]any {
	out := map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "title": name, "type": "object"}
	m, _ := raw.(map[string]any)
	fields := schemaFieldsFromAny(m["fields"])
	props := map[string]any{}
	var required []string
	for _, field := range fields {
		fieldName := fieldName(field)
		if fieldName == "" {
			continue
		}
		props[fieldName] = bclFieldToJSONSchema(field)
		if req, _ := field["required"].(bool); req {
			required = append(required, fieldName)
		}
	}
	out["properties"] = props
	if len(required) > 0 {
		sort.Strings(required)
		out["required"] = required
	}
	return out
}

func bclFieldToJSONSchema(field map[string]any) map[string]any {
	out := map[string]any{}
	typ := jsonSchemaTypeName(scalarString(field["type"]))
	if nullable, _ := field["nullable"].(bool); nullable {
		out["type"] = []string{typ, "null"}
	} else if typ != "" && typ != "any" {
		out["type"] = typ
	}
	copySchemaKey(out, field, "title", "title")
	copySchemaKey(out, field, "description", "description")
	copySchemaKey(out, field, "const", "const")
	copySchemaKey(out, field, "enum", "enum")
	copySchemaKey(out, field, "default", "default")
	copySchemaKey(out, field, "examples", "examples")
	copySchemaKey(out, field, "format", "format")
	copySchemaKey(out, field, "pattern", "pattern")
	copySchemaKey(out, field, "min", "minimum")
	copySchemaKey(out, field, "max", "maximum")
	copySchemaKey(out, field, "exclusive_min", "exclusiveMinimum")
	copySchemaKey(out, field, "exclusive_max", "exclusiveMaximum")
	copySchemaKey(out, field, "multiple_of", "multipleOf")
	copySchemaKey(out, field, "min_len", "minLength")
	copySchemaKey(out, field, "max_len", "maxLength")
	copySchemaKey(out, field, "min_items", "minItems")
	copySchemaKey(out, field, "max_items", "maxItems")
	copySchemaKey(out, field, "unique_items", "uniqueItems")
	copySchemaKey(out, field, "min_props", "minProperties")
	copySchemaKey(out, field, "max_props", "maxProperties")
	copySchemaKey(out, field, "content_encoding", "contentEncoding")
	copySchemaKey(out, field, "content_media_type", "contentMediaType")
	copySchemaKey(out, field, "read_only", "readOnly")
	copySchemaKey(out, field, "write_only", "writeOnly")
	if ref := scalarString(field["ref"]); ref != "" {
		out["$ref"] = "#/components/schemas/" + ref
	}
	if itemType := scalarString(field["items"]); itemType != "" {
		out["items"] = map[string]any{"type": jsonSchemaTypeName(itemType)}
	}
	if xs := stringList(field["prefix_items"]); len(xs) > 0 {
		items := make([]any, 0, len(xs))
		for _, itemType := range xs {
			items = append(items, map[string]any{"type": jsonSchemaTypeName(itemType)})
		}
		out["prefixItems"] = items
	}
	if contains := scalarString(field["contains"]); contains != "" {
		out["contains"] = map[string]any{"type": jsonSchemaTypeName(contains)}
	}
	if children := schemaFieldsFromAny(field["fields"]); len(children) > 0 {
		props := map[string]any{}
		var required []string
		for _, child := range children {
			name := fieldName(child)
			props[name] = bclFieldToJSONSchema(child)
			if req, _ := child["required"].(bool); req {
				required = append(required, name)
			}
		}
		out["properties"] = props
		if len(required) > 0 {
			sort.Strings(required)
			out["required"] = required
		}
	}
	if closed, _ := field["closed"].(bool); closed {
		out["additionalProperties"] = false
	}
	if additional, ok := field["additional_properties"].(bool); ok {
		out["additionalProperties"] = additional
	}
	for _, key := range []string{"classification", "audit", "explain", "pii", "policy_tag", "owner", "severity", "derived", "generated", "sensitive"} {
		if value, ok := field[key]; ok {
			out["x-bcl-"+strings.ReplaceAll(key, "_", "-")] = value
		}
	}
	for key, value := range field {
		if strings.HasPrefix(key, "x_") {
			out["x-"+strings.TrimPrefix(key, "x_")] = value
		}
	}
	return out
}

func jsonSchemaTypeName(typ string) string {
	switch resolveBuiltinAlias(typ) {
	case "int":
		return "integer"
	case "float", "number":
		return "number"
	case "bool":
		return "boolean"
	case "list", "array":
		return "array"
	case "map", "object", "block":
		return "object"
	case "any", "":
		return "any"
	default:
		return "string"
	}
}

func copySchemaKey(dst, src map[string]any, from, to string) {
	if value, ok := src[from]; ok {
		dst[to] = value
	}
}

func jsonSchemaPropertiesToFields(raw map[string]any) []SchemaField {
	requiredSet := map[string]bool{}
	for _, name := range stringList(raw["required"]) {
		requiredSet[name] = true
	}
	props, _ := raw["properties"].(map[string]any)
	names := sortedSchemaNames(props)
	fields := make([]SchemaField, 0, len(names))
	for _, name := range names {
		prop, _ := props[name].(map[string]any)
		field := SchemaField{Name: name, Required: requiredSet[name], Type: bclTypeName(prop["type"])}
		field.Title = scalarString(prop["title"])
		field.Description = scalarString(prop["description"])
		field.Pattern = scalarString(prop["pattern"])
		field.Format = scalarString(prop["format"])
		field.ContentEncoding = scalarString(prop["contentEncoding"])
		field.ContentMediaType = scalarString(prop["contentMediaType"])
		if v, ok := prop["const"]; ok {
			field.Const = schemaLiteral(v)
		}
		if enum := asAnySlice(prop["enum"]); len(enum) > 0 {
			for _, item := range enum {
				field.Enum = append(field.Enum, schemaLiteral(item))
			}
		}
		for key, ptr := range map[string]*Value{
			"minimum": &field.Min, "maximum": &field.Max, "exclusiveMinimum": &field.ExclusiveMin,
			"exclusiveMaximum": &field.ExclusiveMax, "multipleOf": &field.MultipleOf,
			"minLength": &field.MinLen, "maxLength": &field.MaxLen, "minItems": &field.MinItems,
			"maxItems": &field.MaxItems, "minProperties": &field.MinProps, "maxProperties": &field.MaxProps,
		} {
			if value, ok := prop[key]; ok {
				*ptr = schemaLiteral(value)
			}
		}
		if unique, _ := prop["uniqueItems"].(bool); unique {
			field.UniqueItems = true
		}
		if additional, ok := prop["additionalProperties"].(bool); ok {
			field.AdditionalProperties = &additional
			field.ClosedSet = !additional
			field.Closed = !additional
		}
		field.Fields = jsonSchemaPropertiesToFields(prop)
		fields = append(fields, field)
	}
	return fields
}

func bclTypeName(raw any) string {
	if s, ok := raw.(string); ok {
		switch s {
		case "integer":
			return "int"
		case "number":
			return "number"
		case "boolean":
			return "bool"
		case "array":
			return "list"
		case "object":
			return "object"
		case "string":
			return "string"
		default:
			return "any"
		}
	}
	if xs, ok := raw.([]any); ok && len(xs) > 0 {
		for _, item := range xs {
			if scalarString(item) != "null" {
				return bclTypeName(item)
			}
		}
		return "any"
	}
	switch scalarString(raw) {
	case "integer":
		return "int"
	case "number":
		return "number"
	case "boolean":
		return "bool"
	case "array":
		return "list"
	case "object":
		return "object"
	case "string":
		return "string"
	default:
		return "any"
	}
}

func schemaLiteral(v any) Value {
	switch x := v.(type) {
	case nil:
		return &Literal{Type: "null", Data: nil}
	case bool:
		return &Literal{Type: "bool", Data: x}
	case string:
		return &Literal{Type: "string", Data: x}
	case float64:
		if math.Trunc(x) == x {
			return &Literal{Type: "int", Data: int64(x)}
		}
		return &Literal{Type: "float", Data: x}
	case int:
		return &Literal{Type: "int", Data: int64(x)}
	case int64:
		return &Literal{Type: "int", Data: x}
	default:
		return &Literal{Type: "string", Data: fmt.Sprint(v)}
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
			for _, f := range x.Fields {
				req := "optional"
				if f.Required {
					req = "required"
				}
				fmt.Fprintf(&b, "- `%s` `%s` %s\n", f.Name, f.Type, req)
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
