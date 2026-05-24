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
		sectioned := len(x.Options) > 0 || len(x.Sections) > 0
		if len(x.Options) > 0 {
			writeSchemaOptions(b, x.Options, indent+1)
		}
		if sectioned {
			fmt.Fprintf(b, "%s  fields {\n", pad)
			for _, f := range x.Fields {
				writeSchemaSectionField(b, f, indent+2)
			}
			fmt.Fprintf(b, "%s  }\n", pad)
			for _, key := range sortedValueKeys(x.Sections) {
				writeSchemaSection(b, key, x.Sections[key], indent+1)
			}
		} else {
			for _, f := range x.Fields {
				writeSchemaField(b, f, indent+1)
			}
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
	case *Spread:
		fmt.Fprintf(b, "%s&%s", pad, x.Target)
		if len(x.Body) == 0 {
			b.WriteByte('\n')
			return
		}
		b.WriteString(" {\n")
		writeNodes(b, x.Body, indent+1)
		fmt.Fprintf(b, "%s}\n", pad)
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
		writeSchemaFieldBlockClauses(b, f, indent+1)
		for _, child := range f.Fields {
			writeSchemaField(b, child, indent+1)
		}
		fmt.Fprintf(b, "%s}\n", pad)
		return
	}
	writeSchemaFieldInlineClauses(b, f, indent)
	b.WriteByte('\n')
}

func writeSchemaOptions(b *bytes.Buffer, options map[string]Value, indent int) {
	pad := strings.Repeat("  ", indent)
	fmt.Fprintf(b, "%soptions {\n", pad)
	for _, key := range sortedValueKeys(options) {
		fmt.Fprintf(b, "%s  %s ", pad, key)
		writeValue(b, options[key], indent+1)
		b.WriteByte('\n')
	}
	fmt.Fprintf(b, "%s}\n", pad)
}

func writeSchemaSection(b *bytes.Buffer, name string, value Value, indent int) {
	pad := strings.Repeat("  ", indent)
	if obj, ok := value.(*Object); ok {
		fmt.Fprintf(b, "%s%s {\n", pad, name)
		writeNodes(b, obj.Fields, indent+1)
		fmt.Fprintf(b, "%s}\n", pad)
		return
	}
	fmt.Fprintf(b, "%s%s ", pad, name)
	writeValue(b, value, indent)
	b.WriteByte('\n')
}

func writeSchemaSectionField(b *bytes.Buffer, f SchemaField, indent int) {
	pad := strings.Repeat("  ", indent)
	req := "optional"
	if f.Required {
		req = "required"
	}
	fmt.Fprintf(b, "%s%s %s %s", pad, f.Name, f.Type, req)
	if len(f.Fields) > 0 {
		b.WriteString(" {\n")
		writeSchemaFieldBlockClauses(b, f, indent+1)
		for _, child := range f.Fields {
			writeSchemaField(b, child, indent+1)
		}
		fmt.Fprintf(b, "%s}\n", pad)
		return
	}
	writeSchemaFieldInlineClauses(b, f, indent)
	b.WriteByte('\n')
}

func writeSchemaFieldInlineClauses(b *bytes.Buffer, f SchemaField, indent int) {
	if f.Ref != "" {
		writeSchemaInlineString(b, "ref", f.Ref)
	}
	if f.Const != nil {
		writeSchemaInlineValue(b, "const", f.Const, indent)
	}
	if len(f.Enum) > 0 {
		writeSchemaInlineValue(b, "enum", &List{Items: f.Enum}, indent)
	}
	if f.Default != nil {
		writeSchemaInlineValue(b, "default", f.Default, indent)
	}
	writeSchemaInlineStringOptional(b, "title", f.Title)
	writeSchemaInlineStringOptional(b, "description", f.Description)
	writeSchemaInlineStringOptional(b, "deprecated", f.Deprecated)
	writeSchemaInlineFlag(b, "sensitive", f.Sensitive)
	writeSchemaInlineFlag(b, "generated", f.Generated)
	writeSchemaInlineFlag(b, "derived", f.Derived)
	writeSchemaInlineFlag(b, "read_only", f.ReadOnly)
	writeSchemaInlineFlag(b, "write_only", f.WriteOnly)
	writeSchemaInlineFlag(b, "nullable", f.Nullable)
	writeSchemaInlineFlag(b, "unique_items", f.UniqueItems)
	if f.ClosedSet {
		fmt.Fprintf(b, " closed %t", f.Closed)
	}
	if f.AdditionalProperties != nil {
		fmt.Fprintf(b, " additional_properties %t", *f.AdditionalProperties)
	}
	for _, item := range []struct {
		name  string
		value Value
	}{
		{"min", f.Min}, {"max", f.Max}, {"exclusive_min", f.ExclusiveMin}, {"exclusive_max", f.ExclusiveMax},
		{"multiple_of", f.MultipleOf}, {"min_len", f.MinLen}, {"max_len", f.MaxLen}, {"min_items", f.MinItems},
		{"max_items", f.MaxItems}, {"min_props", f.MinProps}, {"max_props", f.MaxProps},
	} {
		if item.value != nil {
			writeSchemaInlineValue(b, item.name, item.value, indent)
		}
	}
	writeSchemaInlineStringOptional(b, "pattern", f.Pattern)
	writeSchemaInlineStringOptional(b, "format", f.Format)
	writeSchemaInlineStringOptional(b, "content_encoding", f.ContentEncoding)
	writeSchemaInlineStringOptional(b, "content_media_type", f.ContentMediaType)
	if len(f.Examples) > 0 {
		writeSchemaInlineValue(b, "examples", &List{Items: f.Examples}, indent)
	}
	writeSchemaInlineTypeOptional(b, "items", f.Items)
	if len(f.PrefixItems) > 0 {
		writeSchemaInlineStringList(b, "prefix_items", f.PrefixItems)
	}
	writeSchemaInlineTypeOptional(b, "contains", f.Contains)
	if f.PatternProperties != nil {
		writeSchemaInlineValue(b, "pattern_properties", f.PatternProperties, indent)
	}
	if f.DependentRequired != nil {
		writeSchemaInlineValue(b, "dependent_required", f.DependentRequired, indent)
	}
	writeSchemaInlineStringOptional(b, "lt_field", f.LTField)
	writeSchemaInlineStringOptional(b, "lte_field", f.LTEField)
	writeSchemaInlineStringOptional(b, "gt_field", f.GTField)
	writeSchemaInlineStringOptional(b, "gte_field", f.GTEField)
	writeSchemaInlineStringOptional(b, "eq_field", f.EqField)
	writeSchemaInlineStringListOptional(b, "all_of", f.AllOf)
	writeSchemaInlineStringListOptional(b, "any_of", f.AnyOf)
	writeSchemaInlineStringListOptional(b, "one_of", f.OneOf)
	writeSchemaInlineStringOptional(b, "not", f.Not)
	writeSchemaInlineStringOptional(b, "if", f.If)
	writeSchemaInlineStringOptional(b, "then", f.Then)
	writeSchemaInlineStringOptional(b, "else", f.Else)
	writeSchemaInlineStringOptional(b, "classification", f.Classification)
	writeSchemaInlineStringOptional(b, "audit", f.Audit)
	writeSchemaInlineStringOptional(b, "explain", f.Explain)
	writeSchemaInlineStringOptional(b, "pii", f.PII)
	writeSchemaInlineStringOptional(b, "policy_tag", f.PolicyTag)
	writeSchemaInlineStringOptional(b, "owner", f.Owner)
	writeSchemaInlineStringOptional(b, "severity", f.Severity)
	for _, k := range sortedValueKeys(f.Extensions) {
		writeSchemaInlineValue(b, k, f.Extensions[k], indent)
	}
}

func schemaFieldHasBlockClauses(f SchemaField) bool {
	return f.Ref != "" || f.Const != nil || f.Default != nil || len(f.Enum) > 0 || f.Description != "" || f.Title != "" ||
		f.Deprecated != "" || f.Sensitive || f.Generated || f.Derived || f.ReadOnly || f.WriteOnly || f.Nullable ||
		f.UniqueItems || f.ClosedSet || f.AdditionalProperties != nil || f.Min != nil || f.Max != nil ||
		f.ExclusiveMin != nil || f.ExclusiveMax != nil || f.MultipleOf != nil || f.MinLen != nil || f.MaxLen != nil ||
		f.MinItems != nil || f.MaxItems != nil || f.MinProps != nil || f.MaxProps != nil || f.Pattern != "" ||
		f.Format != "" || f.ContentEncoding != "" || f.ContentMediaType != "" || len(f.Examples) > 0 ||
		f.PatternProperties != nil || f.DependentRequired != nil || f.LTField != "" || f.LTEField != "" ||
		f.GTField != "" || f.GTEField != "" || f.EqField != "" || len(f.AllOf) > 0 || len(f.AnyOf) > 0 ||
		len(f.OneOf) > 0 || f.Not != "" || f.If != "" || f.Then != "" || f.Else != "" ||
		f.Classification != "" || f.Audit != "" || f.Explain != "" || f.PII != "" || f.PolicyTag != "" ||
		f.Owner != "" || f.Severity != "" || len(f.Extensions) > 0 || f.Items != "" || len(f.PrefixItems) > 0 ||
		f.Contains != ""
}

func writeSchemaFieldBlockClauses(b *bytes.Buffer, f SchemaField, indent int) {
	if f.Ref != "" {
		writeSchemaClauseString(b, indent, "ref", f.Ref)
	}
	if f.Const != nil {
		writeSchemaClauseValue(b, indent, "const", f.Const)
	}
	if len(f.Enum) > 0 {
		writeSchemaClauseValue(b, indent, "enum", &List{Items: f.Enum})
	}
	if f.Default != nil {
		writeSchemaClauseValue(b, indent, "default", f.Default)
	}
	writeSchemaClauseStringOptional(b, indent, "title", f.Title)
	writeSchemaClauseStringOptional(b, indent, "description", f.Description)
	writeSchemaClauseStringOptional(b, indent, "deprecated", f.Deprecated)
	writeSchemaClauseFlag(b, indent, "sensitive", f.Sensitive)
	writeSchemaClauseFlag(b, indent, "generated", f.Generated)
	writeSchemaClauseFlag(b, indent, "derived", f.Derived)
	writeSchemaClauseFlag(b, indent, "read_only", f.ReadOnly)
	writeSchemaClauseFlag(b, indent, "write_only", f.WriteOnly)
	writeSchemaClauseFlag(b, indent, "nullable", f.Nullable)
	writeSchemaClauseFlag(b, indent, "unique_items", f.UniqueItems)
	if f.ClosedSet {
		writeSchemaClauseBool(b, indent, "closed", f.Closed)
	}
	if f.AdditionalProperties != nil {
		writeSchemaClauseBool(b, indent, "additional_properties", *f.AdditionalProperties)
	}
	for _, item := range []struct {
		name  string
		value Value
	}{
		{"min", f.Min}, {"max", f.Max}, {"exclusive_min", f.ExclusiveMin}, {"exclusive_max", f.ExclusiveMax},
		{"multiple_of", f.MultipleOf}, {"min_len", f.MinLen}, {"max_len", f.MaxLen}, {"min_items", f.MinItems},
		{"max_items", f.MaxItems}, {"min_props", f.MinProps}, {"max_props", f.MaxProps},
	} {
		if item.value != nil {
			writeSchemaClauseValue(b, indent, item.name, item.value)
		}
	}
	writeSchemaClauseStringOptional(b, indent, "pattern", f.Pattern)
	writeSchemaClauseStringOptional(b, indent, "format", f.Format)
	writeSchemaClauseStringOptional(b, indent, "content_encoding", f.ContentEncoding)
	writeSchemaClauseStringOptional(b, indent, "content_media_type", f.ContentMediaType)
	if len(f.Examples) > 0 {
		writeSchemaClauseValue(b, indent, "examples", &List{Items: f.Examples})
	}
	writeSchemaClauseTypeOptional(b, indent, "items", f.Items)
	if len(f.PrefixItems) > 0 {
		writeSchemaClauseStringList(b, indent, "prefix_items", f.PrefixItems)
	}
	writeSchemaClauseTypeOptional(b, indent, "contains", f.Contains)
	if f.PatternProperties != nil {
		writeSchemaClauseValue(b, indent, "pattern_properties", f.PatternProperties)
	}
	if f.DependentRequired != nil {
		writeSchemaClauseValue(b, indent, "dependent_required", f.DependentRequired)
	}
	writeSchemaClauseStringOptional(b, indent, "lt_field", f.LTField)
	writeSchemaClauseStringOptional(b, indent, "lte_field", f.LTEField)
	writeSchemaClauseStringOptional(b, indent, "gt_field", f.GTField)
	writeSchemaClauseStringOptional(b, indent, "gte_field", f.GTEField)
	writeSchemaClauseStringOptional(b, indent, "eq_field", f.EqField)
	writeSchemaClauseStringListOptional(b, indent, "all_of", f.AllOf)
	writeSchemaClauseStringListOptional(b, indent, "any_of", f.AnyOf)
	writeSchemaClauseStringListOptional(b, indent, "one_of", f.OneOf)
	writeSchemaClauseStringOptional(b, indent, "not", f.Not)
	writeSchemaClauseStringOptional(b, indent, "if", f.If)
	writeSchemaClauseStringOptional(b, indent, "then", f.Then)
	writeSchemaClauseStringOptional(b, indent, "else", f.Else)
	writeSchemaClauseStringOptional(b, indent, "classification", f.Classification)
	writeSchemaClauseStringOptional(b, indent, "audit", f.Audit)
	writeSchemaClauseStringOptional(b, indent, "explain", f.Explain)
	writeSchemaClauseStringOptional(b, indent, "pii", f.PII)
	writeSchemaClauseStringOptional(b, indent, "policy_tag", f.PolicyTag)
	writeSchemaClauseStringOptional(b, indent, "owner", f.Owner)
	writeSchemaClauseStringOptional(b, indent, "severity", f.Severity)
	for _, k := range sortedValueKeys(f.Extensions) {
		writeSchemaClauseValue(b, indent, k, f.Extensions[k])
	}
	if schemaFieldHasBlockClauses(f) && len(f.Fields) > 0 {
		b.WriteByte('\n')
	}
}

func writeSchemaClauseValue(b *bytes.Buffer, indent int, name string, value Value) {
	fmt.Fprintf(b, "%s%s ", strings.Repeat("  ", indent), name)
	writeValue(b, value, indent)
	b.WriteByte('\n')
}

func writeSchemaClauseString(b *bytes.Buffer, indent int, name, value string) {
	fmt.Fprintf(b, "%s%s %s\n", strings.Repeat("  ", indent), name, quoteBCLString(value))
}

func writeSchemaClauseStringOptional(b *bytes.Buffer, indent int, name, value string) {
	if value != "" {
		writeSchemaClauseString(b, indent, name, value)
	}
}

func writeSchemaClauseTypeOptional(b *bytes.Buffer, indent int, name, value string) {
	if value != "" {
		fmt.Fprintf(b, "%s%s %s\n", strings.Repeat("  ", indent), name, value)
	}
}

func writeSchemaClauseFlag(b *bytes.Buffer, indent int, name string, enabled bool) {
	if enabled {
		fmt.Fprintf(b, "%s%s\n", strings.Repeat("  ", indent), name)
	}
}

func writeSchemaClauseBool(b *bytes.Buffer, indent int, name string, value bool) {
	fmt.Fprintf(b, "%s%s %t\n", strings.Repeat("  ", indent), name, value)
}

func writeSchemaClauseStringListOptional(b *bytes.Buffer, indent int, name string, values []string) {
	if len(values) > 0 {
		writeSchemaClauseStringList(b, indent, name, values)
	}
}

func writeSchemaClauseStringList(b *bytes.Buffer, indent int, name string, values []string) {
	fmt.Fprintf(b, "%s%s [", strings.Repeat("  ", indent), name)
	for i, value := range values {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(value)
	}
	b.WriteString("]\n")
}

func writeSchemaInlineValue(b *bytes.Buffer, name string, value Value, indent int) {
	b.WriteByte(' ')
	b.WriteString(name)
	b.WriteByte(' ')
	writeValue(b, value, indent)
}

func writeSchemaInlineString(b *bytes.Buffer, name, value string) {
	fmt.Fprintf(b, " %s %s", name, quoteBCLString(value))
}

func writeSchemaInlineStringOptional(b *bytes.Buffer, name, value string) {
	if value != "" {
		writeSchemaInlineString(b, name, value)
	}
}

func writeSchemaInlineTypeOptional(b *bytes.Buffer, name, value string) {
	if value != "" {
		fmt.Fprintf(b, " %s %s", name, value)
	}
}

func writeSchemaInlineFlag(b *bytes.Buffer, name string, enabled bool) {
	if enabled {
		b.WriteByte(' ')
		b.WriteString(name)
	}
}

func writeSchemaInlineStringListOptional(b *bytes.Buffer, name string, values []string) {
	if len(values) > 0 {
		writeSchemaInlineStringList(b, name, values)
	}
}

func writeSchemaInlineStringList(b *bytes.Buffer, name string, values []string) {
	b.WriteByte(' ')
	b.WriteString(name)
	b.WriteString(" [")
	for i, value := range values {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(value)
	}
	b.WriteByte(']')
}

func sortedValueKeys(m map[string]Value) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
