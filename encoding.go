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
	return assignGoValue(rv.Elem(), n.Body)
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
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			if name != "" {
				fmt.Fprintf(b, "%s%s null\n", pad(indent), name)
			}
			return nil
		}
		rv = rv.Elem()
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
				if err := writeGoValue(b, fv, indent, ""); err != nil {
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
			if err := writeGoValue(b, fv, indent, fieldName); err != nil {
				return err
			}
		}
		if name != "" {
			fmt.Fprintf(b, "%s}\n", pad(indent-1))
		}
	case reflect.Map:
		if name != "" {
			fmt.Fprintf(b, "%s%s {\n", pad(indent), name)
			indent++
		}
		for _, k := range rv.MapKeys() {
			if err := writeGoValue(b, rv.MapIndex(k), indent, fmt.Sprint(k.Interface())); err != nil {
				return err
			}
		}
		if name != "" {
			fmt.Fprintf(b, "%s}\n", pad(indent-1))
		}
	case reflect.Slice, reflect.Array:
		fmt.Fprintf(b, "%s%s [", pad(indent), name)
		for i := 0; i < rv.Len(); i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			writeScalar(b, rv.Index(i))
		}
		b.WriteString("]\n")
	default:
		fmt.Fprintf(b, "%s%s ", pad(indent), name)
		writeScalar(b, rv)
		b.WriteByte('\n')
	}
	return nil
}

func writeScalar(b *bytes.Buffer, rv reflect.Value) {
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			b.WriteString("null")
			return
		}
		rv = rv.Elem()
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
		}
	}
	return t
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
