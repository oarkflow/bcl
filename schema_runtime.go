package bcl

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ValidateSchemaValue validates a runtime value against a normalized BCL schema map.
// The schema shape is intentionally JSON-like so it can be shared by block
// validation, decision input validation, and schema export/import helpers.
func ValidateSchemaValue(schemaName string, schema any, value any) []Diagnostic {
	ctx := &schemaValidationContext{schemaName: schemaName, root: schema}
	ctx.validateSchema(schema, value, "", map[any]bool{})
	return ctx.diags
}

type schemaValidationContext struct {
	schemaName string
	root       any
	diags      []Diagnostic
}

func (c *schemaValidationContext) validateSchema(schema any, value any, path string, stack map[any]bool) bool {
	m, ok := schema.(map[string]any)
	if !ok {
		return true
	}
	before := len(c.diags)
	if fields := schemaFieldsFromAny(m["fields"]); len(fields) > 0 {
		obj, _ := value.(map[string]any)
		if obj == nil {
			c.add(path, "should be object")
			return false
		}
		c.validateFields(fields, obj, path, m)
	}
	return len(c.diags) == before
}

func (c *schemaValidationContext) validateFields(fields []map[string]any, obj map[string]any, path string, owner map[string]any) {
	known := map[string]bool{}
	validateRequired := schemaOptionBool(owner, "validate_required_fields", true)
	validateTypes := schemaOptionBool(owner, "validate_field_types", true)
	for _, field := range fields {
		name := fieldName(field)
		if name == "" {
			continue
		}
		known[name] = true
		fieldPath := joinSchemaPath(path, name)
		v, ok := schemaLookupField(obj, name)
		if !ok || v == nil {
			if required, _ := field["required"].(bool); validateRequired && required {
				if nullable, _ := field["nullable"].(bool); !nullable || !ok {
					c.add(fieldPath, "is required")
				}
			}
			continue
		}
		c.validateFieldWithOptions(field, v, fieldPath, validateTypes)
		c.validateCrossField(field, v, obj, fieldPath)
	}
	c.validateObjectConstraints(owner, obj, path, known)
}

func schemaOptionBool(schema map[string]any, key string, fallback bool) bool {
	options, _ := schema["options"].(map[string]any)
	if options == nil {
		return fallback
	}
	if v, ok := options[key].(bool); ok {
		return v
	}
	return fallback
}

func schemaLookupField(obj map[string]any, name string) (any, bool) {
	if v, ok := obj[name]; ok {
		return v, true
	}
	if strings.Contains(name, ".") {
		v := lookup(obj, name)
		return v, v != nil
	}
	return nil, false
}

func (c *schemaValidationContext) validateField(field map[string]any, v any, path string) bool {
	return c.validateFieldWithOptions(field, v, path, true)
}

func (c *schemaValidationContext) validateFieldWithOptions(field map[string]any, v any, path string, validateTypes bool) bool {
	before := len(c.diags)
	if nullable, _ := field["nullable"].(bool); nullable && v == nil {
		return true
	}
	if ref := scalarString(field["ref"]); validateTypes && ref != "" {
		c.validateTypeRef(ref, v, path)
	}
	if typ := scalarString(field["type"]); validateTypes && typ != "" && !runtimeTypeMatches(typ, v) {
		c.add(path, fmt.Sprintf("should be %s", typ))
		return false
	}
	if constValue, ok := field["const"]; ok && !equalLoose(v, constValue) {
		c.add(path, "does not match const")
	}
	if enum := asAnySlice(field["enum"]); len(enum) > 0 {
		found := false
		for _, item := range enum {
			if equalLoose(v, item) {
				found = true
				break
			}
		}
		if !found {
			c.add(path, "is not in enum")
		}
	}
	c.validateNumberConstraints(field, v, path)
	c.validateStringConstraints(field, v, path)
	c.validateListConstraints(field, v, path)
	if obj, ok := v.(map[string]any); ok {
		c.validateFields(schemaFieldsFromAny(field["fields"]), obj, path, field)
		c.validateDependentRequired(field, obj, path)
	}
	c.validateCompositions(field, v, path)
	c.validateConditional(field, v, path)
	return len(c.diags) == before
}

func (c *schemaValidationContext) validateNumberConstraints(field map[string]any, v any, path string) {
	got, ok := numericFloat(v)
	if !ok {
		return
	}
	if min, ok := numericFloat(field["min"]); ok && got < min {
		c.add(path, "is below minimum")
	}
	if max, ok := numericFloat(field["max"]); ok && got > max {
		c.add(path, "is above maximum")
	}
	if min, ok := numericFloat(field["exclusive_min"]); ok && got <= min {
		c.add(path, "must be greater than exclusive minimum")
	}
	if max, ok := numericFloat(field["exclusive_max"]); ok && got >= max {
		c.add(path, "must be less than exclusive maximum")
	}
	if multiple, ok := numericFloat(field["multiple_of"]); ok && multiple != 0 {
		q := got / multiple
		if math.Abs(q-math.Round(q)) > 1e-9 {
			c.add(path, "is not a multiple of required value")
		}
	}
}

func (c *schemaValidationContext) validateStringConstraints(field map[string]any, v any, path string) {
	s, ok := v.(string)
	if !ok {
		return
	}
	if min, ok := intScalarValue(field["min_len"]); ok && len([]rune(s)) < min {
		c.add(path, "is shorter than minimum length")
	}
	if max, ok := intScalarValue(field["max_len"]); ok && len([]rune(s)) > max {
		c.add(path, "is longer than maximum length")
	}
	if pattern := scalarString(field["pattern"]); pattern != "" {
		re, err := regexp.Compile(pattern)
		if err != nil {
			c.add(path, fmt.Sprintf("has invalid pattern: %v", err))
		} else if !re.MatchString(s) {
			c.add(path, "does not match pattern")
		}
	}
	if format := scalarString(field["format"]); format != "" && !schemaFormatMatches(format, s) {
		c.add(path, fmt.Sprintf("does not match format %s", format))
	}
}

func (c *schemaValidationContext) validateListConstraints(field map[string]any, v any, path string) {
	items, ok := sliceValues(v)
	if !ok {
		return
	}
	if min, ok := intScalarValue(field["min_items"]); ok && len(items) < min {
		c.add(path, "has too few items")
	}
	if max, ok := intScalarValue(field["max_items"]); ok && len(items) > max {
		c.add(path, "has too many items")
	}
	if unique, _ := field["unique_items"].(bool); unique {
		seen := map[string]bool{}
		for _, item := range items {
			key := schemaValueKey(item)
			if seen[key] {
				c.add(path, "has duplicate items")
				break
			}
			seen[key] = true
		}
	}
	if typ := scalarString(field["items"]); typ != "" {
		for i, item := range items {
			c.validateTypeRef(typ, item, fmt.Sprintf("%s[%d]", path, i))
		}
	}
	for i, typ := range stringList(field["prefix_items"]) {
		if i >= len(items) {
			break
		}
		c.validateTypeRef(typ, items[i], fmt.Sprintf("%s[%d]", path, i))
	}
	if typ := scalarString(field["contains"]); typ != "" {
		found := false
		for _, item := range items {
			if c.typeRefMatches(typ, item) {
				found = true
				break
			}
		}
		if !found {
			c.add(path, "does not contain a required item")
		}
	}
}

func (c *schemaValidationContext) validateObjectConstraints(schema map[string]any, obj map[string]any, path string, known map[string]bool) {
	if min, ok := intScalarValue(schema["min_props"]); ok && len(obj) < min {
		c.add(path, "has too few properties")
	}
	if max, ok := intScalarValue(schema["max_props"]); ok && len(obj) > max {
		c.add(path, "has too many properties")
	}
	patterns := schemaPatternProperties(schema["pattern_properties"])
	closed, _ := schema["closed"].(bool)
	additional, hasAdditional := schema["additional_properties"].(bool)
	if closed || hasAdditional && !additional {
		for key := range obj {
			if known[key] || schemaKeyMatchesPatternProperties(key, patterns) {
				continue
			}
			c.add(joinSchemaPath(path, key), "is not allowed by closed schema")
		}
	}
	for key, typ := range patterns {
		re, err := regexp.Compile(key)
		if err != nil {
			c.add(path, fmt.Sprintf("has invalid pattern property %q", key))
			continue
		}
		for prop, value := range obj {
			if re.MatchString(prop) {
				c.validateTypeRef(typ, value, joinSchemaPath(path, prop))
			}
		}
	}
}

func (c *schemaValidationContext) validateDependentRequired(field map[string]any, obj map[string]any, path string) {
	deps := schemaDependentRequired(field["dependent_required"])
	for key, required := range deps {
		if _, ok := obj[key]; !ok {
			continue
		}
		for _, dep := range required {
			if _, ok := obj[dep]; !ok {
				c.add(joinSchemaPath(path, dep), fmt.Sprintf("is required when %s is present", key))
			}
		}
	}
}

func (c *schemaValidationContext) validateCrossField(field map[string]any, v any, obj map[string]any, path string) {
	for key, op := range map[string]string{
		"lt_field": "<", "lte_field": "<=", "gt_field": ">", "gte_field": ">=", "eq_field": "==",
	} {
		otherName := scalarString(field[key])
		if otherName == "" {
			continue
		}
		other, ok := obj[otherName]
		if !ok {
			continue
		}
		if !schemaCompareValues(v, other, op) {
			c.add(path, fmt.Sprintf("must be %s %s", op, otherName))
		}
	}
}

func schemaCompareValues(left, right any, op string) bool {
	if op == "==" {
		return equalLoose(left, right)
	}
	l, lok := numericFloat(left)
	r, rok := numericFloat(right)
	if !lok || !rok {
		return true
	}
	switch op {
	case "<":
		return l < r
	case "<=":
		return l <= r
	case ">":
		return l > r
	case ">=":
		return l >= r
	default:
		return true
	}
}

func (c *schemaValidationContext) validateCompositions(field map[string]any, v any, path string) {
	for _, typ := range stringList(field["all_of"]) {
		c.validateTypeRef(typ, v, path)
	}
	if xs := stringList(field["any_of"]); len(xs) > 0 {
		matches := 0
		for _, typ := range xs {
			if c.typeRefMatches(typ, v) {
				matches++
			}
		}
		if matches == 0 {
			c.add(path, "does not match any allowed schema")
		}
	}
	if xs := stringList(field["one_of"]); len(xs) > 0 {
		matches := 0
		for _, typ := range xs {
			if c.typeRefMatches(typ, v) {
				matches++
			}
		}
		if matches != 1 {
			c.add(path, "must match exactly one schema")
		}
	}
	if typ := scalarString(field["not"]); typ != "" && c.typeRefMatches(typ, v) {
		c.add(path, "matches a forbidden schema")
	}
}

func (c *schemaValidationContext) validateConditional(field map[string]any, v any, path string) {
	cond := scalarString(field["if"])
	if cond == "" {
		return
	}
	if c.typeRefMatches(cond, v) {
		if typ := scalarString(field["then"]); typ != "" {
			c.validateTypeRef(typ, v, path)
		}
		return
	}
	if typ := scalarString(field["else"]); typ != "" {
		c.validateTypeRef(typ, v, path)
	}
}

func (c *schemaValidationContext) validateTypeRef(typ string, v any, path string) {
	if typ == "" || typ == "any" {
		return
	}
	if !c.typeRefMatches(typ, v) {
		c.add(path, fmt.Sprintf("should match %s", typ))
	}
}

func (c *schemaValidationContext) typeRefMatches(typ string, v any) bool {
	if typ == "" || typ == "any" {
		return true
	}
	if runtimeTypeMatches(typ, v) {
		return true
	}
	return false
}

func (c *schemaValidationContext) add(path, msg string) {
	c.diags = append(c.diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("schema %q value %q %s", c.schemaName, path, msg)})
}

func joinSchemaPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func schemaFormatMatches(format, s string) bool {
	switch strings.ToLower(format) {
	case "", "string":
		return true
	case "email":
		_, err := mail.ParseAddress(s)
		return err == nil
	case "uri", "url":
		u, err := url.ParseRequestURI(s)
		return err == nil && u.Scheme != ""
	case "hostname":
		return len(s) <= 253 && regexp.MustCompile(`^[A-Za-z0-9.-]+$`).MatchString(s)
	case "ipv4":
		ip := net.ParseIP(s)
		return ip != nil && ip.To4() != nil
	case "ipv6", "ip":
		return net.ParseIP(s) != nil
	case "date":
		_, err := time.Parse("2006-01-02", s)
		return err == nil
	case "date-time", "datetime":
		_, err := time.Parse(time.RFC3339, s)
		return err == nil
	case "time":
		_, err := time.Parse("15:04:05", s)
		if err == nil {
			return true
		}
		_, err = time.Parse(time.RFC3339, "2000-01-01T"+s+"Z")
		return err == nil
	case "uuid":
		return regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`).MatchString(s)
	default:
		return true
	}
}

func schemaValueKey(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func schemaPatternProperties(v any) map[string]string {
	out := map[string]string{}
	switch x := v.(type) {
	case map[string]any:
		for k, raw := range x {
			out[k] = scalarString(raw)
		}
	case map[string]string:
		for k, raw := range x {
			out[k] = raw
		}
	}
	return out
}

func schemaKeyMatchesPatternProperties(key string, patterns map[string]string) bool {
	for pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err == nil && re.MatchString(key) {
			return true
		}
	}
	return false
}

func schemaDependentRequired(v any) map[string][]string {
	out := map[string][]string{}
	switch x := v.(type) {
	case map[string]any:
		for k, raw := range x {
			out[k] = stringList(raw)
		}
	case map[string][]string:
		for k, raw := range x {
			out[k] = append([]string(nil), raw...)
		}
	}
	return out
}

func sortedSchemaNames(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
