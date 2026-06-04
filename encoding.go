package bcl

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

func Marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := writeGoValue(&b, reflect.ValueOf(v), 0, ""); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func Unmarshal(data []byte, v any) error {
	return UnmarshalWithOptions(data, v, &Options{AllowEnv: true})
}

func UnmarshalWithOptions(data []byte, v any, opts *Options) error {
	if opts == nil {
		opts = &Options{}
	}
	n, err := CompileBytes(data, opts)
	if err != nil {
		return err
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("bcl: Unmarshal target must be a non-nil pointer")
	}
	src := make(map[string]any, len(n.Body)+1)
	for k, v := range n.Body {
		src[k] = v
	}
	if len(n.Blocks) > 0 {
		src["$blocks"] = n.Blocks
	}
	return assignGoValue(rv.Elem(), src)
}

type Encoder struct {
	w io.Writer
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

func (e *Encoder) Encode(v any) error {
	b, err := Marshal(v)
	if err != nil {
		return err
	}
	_, err = e.w.Write(b)
	return err
}

type Decoder struct {
	r io.Reader
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

func (d *Decoder) Decode(v any) error {
	b, err := io.ReadAll(d.r)
	if err != nil {
		return err
	}
	return Unmarshal(b, v)
}

func Encode(w io.Writer, v any) error { return NewEncoder(w).Encode(v) }
func Decode(r io.Reader, v any) error { return NewDecoder(r).Decode(v) }

func EncodeFile(path string, v any) error {
	b, err := Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

func DecodeFile(path string, v any) error {
	return DecodeFileWithOptions(path, v, &Options{AllowEnv: true})
}

func DecodeFileWithOptions(path string, v any, opts *Options) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if opts == nil {
		opts = &Options{}
	}
	if opts.BaseDir == "" {
		opts.BaseDir = filepath.Dir(path)
	}
	return UnmarshalWithOptions(b, v, opts)
}

func writeGoValue(b *bytes.Buffer, rv reflect.Value, indent int, name string) error {
	if !rv.IsValid() {
		if name != "" {
			fmt.Fprintf(b, "%s%s null\n", pad(indent), name)
		}
		return nil
	}
	rv = indirectValue(rv)
	if !rv.IsValid() {
		if name != "" {
			fmt.Fprintf(b, "%s%s null\n", pad(indent), name)
		}
		return nil
	}
	if text, ok := textMarshaler(rv); ok {
		if name != "" {
			fmt.Fprintf(b, "%s%s ", pad(indent), name)
		}
		writeTextMarshaler(b, text)
		if name != "" {
			b.WriteByte('\n')
		}
		return nil
	}
	switch rv.Kind() {
	case reflect.Struct:
		if name != "" {
			fmt.Fprintf(b, "%s%s {\n", pad(indent), name)
			indent++
		}
		if err := writeStructFields(b, rv, indent); err != nil {
			return err
		}
		if name != "" {
			fmt.Fprintf(b, "%s}\n", pad(indent-1))
		}
	case reflect.Map:
		if writeSpecialMapValue(b, rv, indent, name) {
			return nil
		}
		if name != "" {
			fmt.Fprintf(b, "%s%s {\n", pad(indent), name)
			indent++
		}
		for _, k := range sortedReflectMapKeys(rv) {
			if err := writeGoValue(b, rv.MapIndex(k), indent, formatBCLName(fmt.Sprint(k.Interface()))); err != nil {
				return err
			}
		}
		if name != "" {
			fmt.Fprintf(b, "%s}\n", pad(indent-1))
		}
	case reflect.Slice, reflect.Array:
		if name != "" {
			fmt.Fprintf(b, "%s%s ", pad(indent), name)
		} else {
			b.WriteString(pad(indent))
		}
		writeInlineValue(b, rv, indent)
		b.WriteByte('\n')
	default:
		fmt.Fprintf(b, "%s%s ", pad(indent), name)
		writeScalar(b, rv)
		b.WriteByte('\n')
	}
	return nil
}

func writeStructFields(b *bytes.Buffer, rv reflect.Value, indent int) error {
	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		sf := rt.Field(i)
		if sf.PkgPath != "" {
			continue
		}
		tag := parseTag(sf.Tag.Get("bcl"))
		if tag.skip || tag.id {
			continue
		}
		fv := rv.Field(i)
		if tag.omitEmpty && isZero(fv) {
			continue
		}
		fieldName := tag.name
		if fieldName == "" {
			fieldName = lowerFirst(sf.Name)
		}
		if tag.inline {
			if err := writeGoValue(b, fv, indent, ""); err != nil {
				return err
			}
			continue
		}
		if tag.block {
			if err := writeBlockValues(b, fv, indent, fieldName); err != nil {
				return err
			}
			continue
		}
		if tag.sensitive {
			fmt.Fprintf(b, "%s%s sensitive(", pad(indent), fieldName)
			writeScalar(b, fv)
			b.WriteString(")\n")
			continue
		}
		if tag.ident {
			fmt.Fprintf(b, "%s%s %s\n", pad(indent), fieldName, identValue(fv))
			continue
		}
		if err := writeGoValue(b, fv, indent, fieldName); err != nil {
			return err
		}
	}
	return nil
}

func writeBlockValues(b *bytes.Buffer, rv reflect.Value, indent int, blockType string) error {
	rv = indirectValue(rv)
	if !rv.IsValid() {
		return nil
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if err := writeBlockValue(b, rv.Index(i), indent, blockType); err != nil {
				return err
			}
			if i+1 < rv.Len() {
				b.WriteByte('\n')
			}
		}
	default:
		return writeBlockValue(b, rv, indent, blockType)
	}
	return nil
}

func writeBlockValue(b *bytes.Buffer, rv reflect.Value, indent int, blockType string) error {
	rv = indirectValue(rv)
	if !rv.IsValid() {
		return nil
	}
	id := blockStructID(rv)
	if id != "" {
		fmt.Fprintf(b, "%s%s %s {\n", pad(indent), blockType, quoteBCLString(id))
	} else {
		fmt.Fprintf(b, "%s%s {\n", pad(indent), blockType)
	}
	if rv.Kind() == reflect.Struct {
		if err := writeStructFields(b, rv, indent+1); err != nil {
			return err
		}
	} else {
		if err := writeGoValue(b, rv, indent+1, ""); err != nil {
			return err
		}
	}
	fmt.Fprintf(b, "%s}\n", pad(indent))
	return nil
}

func blockStructID(rv reflect.Value) string {
	rv = indirectValue(rv)
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return ""
	}
	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		sf := rt.Field(i)
		if sf.PkgPath != "" {
			continue
		}
		tag := parseTag(sf.Tag.Get("bcl"))
		if tag.id {
			return fmt.Sprint(indirectValue(rv.Field(i)).Interface())
		}
	}
	return ""
}

func writeInlineValue(b *bytes.Buffer, rv reflect.Value, indent int) {
	rv = indirectValue(rv)
	if !rv.IsValid() {
		b.WriteString("null")
		return
	}
	if text, ok := textMarshaler(rv); ok {
		writeTextMarshaler(b, text)
		return
	}
	switch rv.Kind() {
	case reflect.Struct:
		b.WriteString("{\n")
		rt := rv.Type()
		for i := 0; i < rv.NumField(); i++ {
			sf := rt.Field(i)
			if sf.PkgPath != "" {
				continue
			}
			tag := parseTag(sf.Tag.Get("bcl"))
			if tag.skip {
				continue
			}
			fv := rv.Field(i)
			if tag.omitEmpty && isZero(fv) {
				continue
			}
			fieldName := tag.name
			if fieldName == "" {
				fieldName = lowerFirst(sf.Name)
			}
			if tag.inline {
				_ = writeGoValue(b, fv, indent+1, "")
				continue
			}
			if tag.sensitive {
				fmt.Fprintf(b, "%s%s sensitive(", pad(indent+1), fieldName)
				writeScalar(b, fv)
				b.WriteString(")\n")
				continue
			}
			_ = writeGoValue(b, fv, indent+1, fieldName)
		}
		b.WriteString(pad(indent))
		b.WriteByte('}')
	case reflect.Map:
		if writeSpecialMapValue(b, rv, indent, "") {
			return
		}
		b.WriteString("{\n")
		for _, k := range sortedReflectMapKeys(rv) {
			_ = writeGoValue(b, rv.MapIndex(k), indent+1, formatBCLName(fmt.Sprint(k.Interface())))
		}
		b.WriteString(pad(indent))
		b.WriteByte('}')
	case reflect.Slice, reflect.Array:
		writeInlineList(b, rv, indent)
	default:
		writeScalar(b, rv)
	}
}

func writeInlineList(b *bytes.Buffer, rv reflect.Value, indent int) {
	if !hasCompositeListItem(rv) {
		b.WriteByte('[')
		for i := 0; i < rv.Len(); i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			writeInlineValue(b, rv.Index(i), indent)
		}
		b.WriteByte(']')
		return
	}
	b.WriteString("[\n")
	for i := 0; i < rv.Len(); i++ {
		b.WriteString(pad(indent + 1))
		writeInlineValue(b, rv.Index(i), indent+1)
		if i+1 < rv.Len() {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(pad(indent))
	b.WriteByte(']')
}

func writeSpecialMapValue(b *bytes.Buffer, rv reflect.Value, indent int, name string) bool {
	m, ok := reflectMapToStringAny(rv)
	if !ok {
		return false
	}
	if _, ok := m["$call"]; !ok {
		if _, ok := m["$ref"]; !ok {
			if _, ok := m["$expr"]; !ok {
				return false
			}
		}
	}
	if name != "" {
		fmt.Fprintf(b, "%s%s ", pad(indent), name)
	}
	writeSpecialAnyValue(b, m, indent)
	if name != "" {
		b.WriteByte('\n')
	}
	return true
}

func writeSpecialAnyValue(b *bytes.Buffer, m map[string]any, indent int) {
	if call, ok := m["$call"].(string); ok {
		b.WriteString(call)
		b.WriteByte('(')
		for i, arg := range listFromAny(m["args"]) {
			if i > 0 {
				b.WriteString(", ")
			}
			writeInlineValue(b, reflect.ValueOf(arg), indent)
		}
		b.WriteByte(')')
		return
	}
	if ref, ok := m["$ref"].(string); ok {
		b.WriteString(ref)
		return
	}
	if expr, ok := m["$expr"].(string); ok {
		b.WriteString(expr)
		return
	}
}

func reflectMapToStringAny(rv reflect.Value) (map[string]any, bool) {
	rv = indirectValue(rv)
	if !rv.IsValid() || rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	m := make(map[string]any, rv.Len())
	for _, key := range rv.MapKeys() {
		m[key.String()] = rv.MapIndex(key).Interface()
	}
	return m, true
}

func hasCompositeListItem(rv reflect.Value) bool {
	for i := 0; i < rv.Len(); i++ {
		item := indirectValue(rv.Index(i))
		if !item.IsValid() {
			continue
		}
		switch item.Kind() {
		case reflect.Struct, reflect.Map:
			return true
		}
	}
	return false
}

func writeScalar(b *bytes.Buffer, rv reflect.Value) {
	rv = indirectValue(rv)
	if !rv.IsValid() {
		b.WriteString("null")
		return
	}
	if text, ok := textMarshaler(rv); ok {
		writeTextMarshaler(b, text)
		return
	}
	switch rv.Kind() {
	case reflect.String:
		b.WriteString(quoteBCLString(rv.String()))
	case reflect.Bool:
		fmt.Fprintf(b, "%t", rv.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fmt.Fprintf(b, "%d", rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		fmt.Fprintf(b, "%d", rv.Uint())
	case reflect.Float32, reflect.Float64:
		fmt.Fprintf(b, "%g", rv.Float())
	default:
		j, _ := json.Marshal(rv.Interface())
		b.Write(j)
	}
}

func indirectValue(rv reflect.Value) reflect.Value {
	for rv.IsValid() && (rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface) {
		if rv.IsNil() {
			return reflect.Value{}
		}
		rv = rv.Elem()
	}
	return rv
}

func textMarshaler(rv reflect.Value) (encoding.TextMarshaler, bool) {
	if !rv.IsValid() {
		return nil, false
	}
	if rv.CanInterface() {
		if text, ok := rv.Interface().(encoding.TextMarshaler); ok {
			return text, true
		}
	}
	if rv.CanAddr() {
		if text, ok := rv.Addr().Interface().(encoding.TextMarshaler); ok {
			return text, true
		}
	}
	return nil, false
}

func writeTextMarshaler(b *bytes.Buffer, text encoding.TextMarshaler) {
	raw, err := text.MarshalText()
	if err != nil {
		b.WriteString("null")
		return
	}
	b.WriteString(quoteBCLString(string(raw)))
}

type tagInfo struct {
	name      string
	skip      bool
	inline    bool
	omitEmpty bool
	sensitive bool
	block     bool
	id        bool
	ident     bool
}

func parseTag(s string) tagInfo {
	if s == "-" {
		return tagInfo{skip: true}
	}
	parts := strings.Split(s, ",")
	t := tagInfo{name: parts[0]}
	for _, p := range parts[1:] {
		switch p {
		case "inline":
			t.inline = true
		case "omitempty":
			t.omitEmpty = true
		case "sensitive":
			t.sensitive = true
		case "block":
			t.block = true
		case "id":
			t.id = true
		case "ident":
			t.ident = true
		}
	}
	return t
}

func identValue(rv reflect.Value) string {
	rv = indirectValue(rv)
	if !rv.IsValid() {
		return "null"
	}
	s := fmt.Sprint(rv.Interface())
	if isBCLIdent(s) {
		return s
	}
	return quoteBCLString(s)
}

func parseJSONName(s string) string {
	if s == "" || s == "-" {
		return ""
	}
	if i := strings.IndexByte(s, ','); i >= 0 {
		return s[:i]
	}
	return s
}

func assignGoValue(dst reflect.Value, src any) error {
	if !dst.CanSet() {
		return nil
	}
	if dst.Kind() == reflect.Pointer {
		if src == nil {
			dst.SetZero()
			return nil
		}
		if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		return assignGoValue(dst.Elem(), src)
	}
	if src == nil {
		dst.SetZero()
		return nil
	}
	if text, ok := textUnmarshaler(dst); ok {
		return text.UnmarshalText([]byte(fmt.Sprint(unwrapTypedScalar(src))))
	}
	switch dst.Kind() {
	case reflect.Struct:
		m, ok := src.(map[string]any)
		if !ok {
			return nil
		}
		rt := dst.Type()
		for i := 0; i < dst.NumField(); i++ {
			sf := rt.Field(i)
			if sf.PkgPath != "" {
				continue
			}
			tag := parseTag(sf.Tag.Get("bcl"))
			if tag.skip {
				continue
			}
			if tag.id {
				if value, ok := m["$id"]; ok {
					if err := assignGoValue(dst.Field(i), value); err != nil {
						return err
					}
				}
				continue
			}
			if tag.inline {
				if err := assignGoValue(dst.Field(i), src); err != nil {
					return err
				}
				continue
			}
			name := tag.name
			if name == "" {
				name = parseJSONName(sf.Tag.Get("json"))
			}
			if name == "" {
				name = lowerFirst(sf.Name)
			}
			if tag.block {
				if err := assignGoValue(dst.Field(i), blockValues(m, name)); err != nil {
					return err
				}
				continue
			}
			if value, ok := m[name]; ok {
				if err := assignGoValue(dst.Field(i), value); err != nil {
					return err
				}
			}
		}
	case reflect.Map:
		m, ok := src.(map[string]any)
		if !ok {
			return nil
		}
		if dst.IsNil() {
			dst.Set(reflect.MakeMapWithSize(dst.Type(), len(m)))
		}
		for k, v := range m {
			key := reflect.New(dst.Type().Key()).Elem()
			if err := assignGoValue(key, k); err != nil {
				return err
			}
			val := reflect.New(dst.Type().Elem()).Elem()
			if err := assignGoValue(val, v); err != nil {
				return err
			}
			dst.SetMapIndex(key, val)
		}
	case reflect.Slice:
		xs, ok := src.([]any)
		if !ok {
			return nil
		}
		out := reflect.MakeSlice(dst.Type(), len(xs), len(xs))
		for i, item := range xs {
			if err := assignGoValue(out.Index(i), item); err != nil {
				return err
			}
		}
		dst.Set(out)
	case reflect.String:
		src = unwrapTypedScalar(src)
		dst.SetString(fmt.Sprint(src))
	case reflect.Bool:
		src = unwrapTypedScalar(src)
		if v, ok := src.(bool); ok {
			dst.SetBool(v)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		src = unwrapTypedScalar(src)
		if n, ok := numericInt(src); ok {
			dst.SetInt(n)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		src = unwrapTypedScalar(src)
		if n, ok := numericInt(src); ok && n >= 0 {
			dst.SetUint(uint64(n))
		}
	case reflect.Float32, reflect.Float64:
		src = unwrapTypedScalar(src)
		if n, ok := numericFloat(src); ok {
			dst.SetFloat(n)
		}
	case reflect.Interface:
		dst.Set(reflect.ValueOf(src))
	}
	return nil
}

func blockValues(m map[string]any, name string) []any {
	var out []any
	if blocks, ok := m["$blocks"].([]map[string]any); ok {
		for _, block := range blocks {
			if stringValue(block["type"]) == name {
				out = append(out, blockBodyWithID(block))
			}
		}
	}
	if blocks, ok := m["$blocks"].([]any); ok {
		for _, item := range blocks {
			block := mapFromAny(item)
			if stringValue(block["type"]) == name {
				out = append(out, blockBodyWithID(block))
			}
		}
	}
	if v, ok := m[name]; ok {
		for _, block := range listFromAny(v) {
			out = append(out, blockBodyWithID(mapFromAny(block)))
		}
	}
	return out
}

func mapFromAny(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func listFromAny(v any) []any {
	if xs, ok := v.([]any); ok {
		return xs
	}
	if xs, ok := v.([]map[string]any); ok {
		out := make([]any, 0, len(xs))
		for _, x := range xs {
			out = append(out, x)
		}
		return out
	}
	return nil
}

func blockBodyWithID(block map[string]any) map[string]any {
	body := mapFromAny(block["body"])
	out := make(map[string]any, len(body)+1)
	for k, v := range body {
		out[k] = v
	}
	if id, ok := block["id"]; ok {
		out["$id"] = id
	}
	return out
}

func textUnmarshaler(dst reflect.Value) (encoding.TextUnmarshaler, bool) {
	if !dst.CanAddr() {
		return nil, false
	}
	text, ok := dst.Addr().Interface().(encoding.TextUnmarshaler)
	return text, ok
}

func unwrapTypedScalar(v any) any {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 1 {
		return v
	}
	for k, x := range m {
		if strings.HasPrefix(k, "$") {
			return x
		}
	}
	return v
}

func numericInt(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		return int64(x), true
	case float32:
		return int64(x), true
	default:
		return 0, false
	}
}

func numericFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	default:
		return 0, false
	}
}

func isZero(v reflect.Value) bool {
	return v.IsZero()
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func pad(n int) string {
	return strings.Repeat("  ", n)
}

func sortedReflectMapKeys(rv reflect.Value) []reflect.Value {
	keys := rv.MapKeys()
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface())
	})
	return keys
}

func formatBCLName(s string) string {
	if isBCLIdent(s) {
		return s
	}
	return quoteBCLString(s)
}

func isBCLIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !isIdentStart(r) {
				return false
			}
			continue
		}
		if !(isIdentPart(r) || r == '*' || r == ':' || r == '-' || r == '/') {
			return false
		}
	}
	return true
}
