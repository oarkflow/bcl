package bcl

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func Format(src []byte) ([]byte, error) {
	if hasComments(src) {
		out := append([]byte(nil), src...)
		if len(out) == 0 || out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
		return out, nil
	}
	doc, err := Parse(src)
	if err != nil {
		return nil, err
	}
	return formatDocument(doc, len(src)+len(src)/8)
}

func FormatDocument(doc *Document) ([]byte, error) {
	return formatDocument(doc, 0)
}

func formatDocument(doc *Document, capacity int) ([]byte, error) {
	var b bytes.Buffer
	if capacity > 0 {
		b.Grow(capacity)
	}
	writeNodes(&b, doc.Items, 0)
	return b.Bytes(), nil
}

func writeNodes(b *bytes.Buffer, nodes []Node, indent int) {
	for i, n := range nodes {
		if i > 0 {
			b.WriteByte('\n')
		}
		writeNode(b, n, indent)
	}
}

func writeNode(b *bytes.Buffer, n Node, indent int) {
	pad := strings.Repeat("  ", indent)
	switch x := n.(type) {
	case *ImportDecl:
		fmt.Fprintf(b, "%simport %q", pad, x.Path)
		if x.Alias != "" {
			fmt.Fprintf(b, " as %s", x.Alias)
		}
		b.WriteByte('\n')
	case *ParamDecl:
		fmt.Fprintf(b, "%sparam %s %s", pad, x.Name, x.Type)
		if x.Required || x.Default != nil || x.Description != "" {
			b.WriteString(" {\n")
			if x.Required {
				fmt.Fprintf(b, "%s  required true\n", pad)
			}
			if x.Default != nil {
				fmt.Fprintf(b, "%s  default ", pad)
				writeValue(b, x.Default, indent+1)
				b.WriteByte('\n')
			}
			if x.Description != "" {
				fmt.Fprintf(b, "%s  description %s\n", pad, quoteBCLString(x.Description))
			}
			fmt.Fprintf(b, "%s}\n", pad)
		} else {
			b.WriteByte('\n')
		}
	case *ConstDecl:
		fmt.Fprintf(b, "%sconst %s = ", pad, x.Name)
		writeValue(b, x.Value, indent)
		b.WriteByte('\n')
	case *TypeDecl:
		fmt.Fprintf(b, "%stype %s = %s\n", pad, x.Name, x.Type)
	case *SchemaDecl:
		fmt.Fprintf(b, "%sschema %s {\n", pad, x.Name)
		for _, f := range x.Fields {
			writeSchemaField(b, f, indent+1)
		}
		fmt.Fprintf(b, "%s}\n", pad)
	case *Assignment:
		if ref, ok := x.Value.(*Reference); ok && ref.Path == "" {
			fmt.Fprintf(b, "%s%s\n", pad, x.Name)
			return
		}
		fmt.Fprintf(b, "%s%s ", pad, x.Name)
		writeValue(b, x.Value, indent)
		b.WriteByte('\n')
	case *Block:
		if x.ID != "" {
			if x.Type == "when" {
				fmt.Fprintf(b, "%swhen %s {\n", pad, x.ID)
			} else if isBareBlockID(x.ID) {
				fmt.Fprintf(b, "%s%s %s {\n", pad, x.Type, x.ID)
			} else {
				fmt.Fprintf(b, "%s%s %q {\n", pad, x.Type, x.ID)
			}
		} else {
			fmt.Fprintf(b, "%s%s {\n", pad, x.Type)
		}
		writeNodes(b, x.Body, indent+1)
		fmt.Fprintf(b, "%s}\n", pad)
	}
}

func hasComments(src []byte) bool {
	inString := byte(0)
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inString != 0 {
			if c == '\\' && inString != '`' {
				i++
				continue
			}
			if c == inString {
				inString = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			inString = c
		case '#':
			return true
		case '/':
			if i+1 < len(src) && (src[i+1] == '/' || src[i+1] == '*') {
				return true
			}
		}
	}
	return false
}

func writeSchemaField(b *bytes.Buffer, f SchemaField, indent int) {
	pad := strings.Repeat("  ", indent)
	req := "optional"
	if f.Required {
		req = "required"
	}
	fmt.Fprintf(b, "%s%s %s %s", pad, req, f.Name, f.Type)
	if len(f.Fields) > 0 {
		b.WriteString(" {\n")
		for _, child := range f.Fields {
			writeSchemaField(b, child, indent+1)
		}
		fmt.Fprintf(b, "%s}\n", pad)
		return
	}
	if len(f.Enum) > 0 {
		b.WriteString(" enum ")
		writeValue(b, &List{Items: f.Enum}, indent)
	}
	if f.Default != nil {
		b.WriteString(" default ")
		writeValue(b, f.Default, indent)
	}
	if f.Description != "" {
		fmt.Fprintf(b, " description %s", quoteBCLString(f.Description))
	}
	if f.Deprecated != "" {
		fmt.Fprintf(b, " deprecated %s", quoteBCLString(f.Deprecated))
	}
	if f.Sensitive {
		b.WriteString(" sensitive")
	}
	if f.Generated {
		b.WriteString(" generated")
	}
	if f.Min != nil {
		b.WriteString(" min ")
		writeValue(b, f.Min, indent)
	}
	if f.Max != nil {
		b.WriteString(" max ")
		writeValue(b, f.Max, indent)
	}
	if f.Pattern != "" {
		fmt.Fprintf(b, " pattern %s", quoteBCLString(f.Pattern))
	}
	if f.Format != "" {
		fmt.Fprintf(b, " format %s", quoteBCLString(f.Format))
	}
	if len(f.Examples) > 0 {
		b.WriteString(" examples ")
		writeValue(b, &List{Items: f.Examples}, indent)
	}
	b.WriteByte('\n')
}

func writeValue(b *bytes.Buffer, v Value, indent int) {
	switch x := v.(type) {
	case *Literal:
		switch x.Type {
		case "string":
			if s, ok := x.Data.(string); ok {
				b.WriteString(quoteBCLString(s))
			} else {
				fmt.Fprintf(b, "%q", x.Data)
			}
		case "null":
			b.WriteString("null")
		default:
			if x.Raw != "" {
				b.WriteString(x.Raw)
			} else {
				fmt.Fprintf(b, "%v", x.Data)
			}
		}
	case *Reference:
		b.WriteString(x.Path)
	case *Expr:
		b.WriteString(x.Raw)
	case *Call:
		b.WriteString(x.Name)
		b.WriteByte('(')
		for i, a := range x.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			writeValue(b, a, indent)
		}
		b.WriteByte(')')
	case *List:
		b.WriteByte('[')
		for i, item := range x.Items {
			if i > 0 {
				b.WriteString(", ")
			}
			writeValue(b, item, indent)
		}
		b.WriteByte(']')
	case *Object:
		pad := strings.Repeat("  ", indent)
		b.WriteString("{\n")
		writeNodes(b, sortNodes(x.Fields), indent+1)
		fmt.Fprintf(b, "%s}", pad)
	case *Condition:
		if x.Expr != nil {
			b.WriteString(x.Expr.Raw)
			return
		}
		b.WriteString(x.Op)
		b.WriteString(" {")
		for _, child := range x.Children {
			b.WriteByte('\n')
			fmt.Fprintf(b, "%s  ", strings.Repeat("  ", indent))
			writeValue(b, child, indent+1)
		}
		b.WriteByte('\n')
		b.WriteString(strings.Repeat("  ", indent))
		b.WriteByte('}')
	}
}

func quoteBCLString(s string) string {
	if strings.Contains(s, "\n") && !strings.Contains(s, `"""`) {
		return `"""` + s + `"""`
	}
	if strings.Contains(s, `"`) && !strings.Contains(s, "`") {
		return "`" + s + "`"
	}
	if strings.Contains(s, `"`) && !strings.Contains(s, `'`) {
		return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `'`, `\'`) + "'"
	}
	return strconv.Quote(s)
}

func sortNodes(nodes []Node) []Node {
	cp := append([]Node(nil), nodes...)
	sort.SliceStable(cp, func(i, j int) bool {
		return nodeName(cp[i]) < nodeName(cp[j])
	})
	return cp
}

func nodeName(n Node) string {
	switch x := n.(type) {
	case *Assignment:
		return x.Name
	case *Block:
		return x.Type + x.ID
	case *ConstDecl:
		return x.Name
	case *TypeDecl:
		return x.Name
	default:
		return ""
	}
}

func isBareBlockID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !(r == '_' || r == '-' || r == '.' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	switch id {
	case "json", "form", "text", "raw", "load", "validate", "evaluate", "execute", "engine":
		return true
	default:
		return !strings.Contains(id, "-")
	}
}
