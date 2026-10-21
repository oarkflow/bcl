package bcl

import (
	"encoding"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

type simpleField struct {
	index int
	kind  reflect.Kind
	bits  int
}

var simpleStructLayouts sync.Map // map[reflect.Type]map[string]simpleField

type simpleMarshalField struct {
	name      string
	index     int
	kind      reflect.Kind
	omitEmpty bool
	sensitive bool
}

type simpleMarshalLayout struct {
	fields []simpleMarshalField
	ok     bool
}

var simpleMarshalLayouts sync.Map // map[reflect.Type]simpleMarshalLayout

func marshalSimpleStruct(value any) ([]byte, bool) {
	if _, ok := value.(encoding.TextMarshaler); ok {
		return nil, false
	}
	rv := reflect.ValueOf(value)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, false
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return nil, false
	}
	layout := simpleMarshalStructLayout(rv.Type())
	if !layout.ok {
		return nil, false
	}
	out := make([]byte, 0, len(layout.fields)*16+16)
	for _, field := range layout.fields {
		fv := rv.Field(field.index)
		if field.omitEmpty && fv.IsZero() {
			continue
		}
		out = append(out, field.name...)
		out = append(out, ' ')
		if field.sensitive {
			out = append(out, "sensitive("...)
		}
		switch field.kind {
		case reflect.String:
			s := fv.String()
			if strings.ContainsAny(s, "\n\"") {
				return nil, false
			}
			out = strconv.AppendQuote(out, s)
		case reflect.Bool:
			out = strconv.AppendBool(out, fv.Bool())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			out = strconv.AppendInt(out, fv.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			out = strconv.AppendUint(out, fv.Uint(), 10)
		case reflect.Float32, reflect.Float64:
			out = strconv.AppendFloat(out, fv.Float(), 'g', 6, 64)
		}
		if field.sensitive {
			out = append(out, ')')
		}
		out = append(out, '\n')
	}
	if len(out) == 0 {
		return nil, true
	}
	return out, true
}

func simpleMarshalStructLayout(typ reflect.Type) simpleMarshalLayout {
	if cached, ok := simpleMarshalLayouts.Load(typ); ok {
		return cached.(simpleMarshalLayout)
	}
	layout := simpleMarshalLayout{ok: true, fields: make([]simpleMarshalField, 0, typ.NumField())}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		tag := parseTag(field.Tag.Get("bcl"))
		if tag.skip || tag.id {
			continue
		}
		kind := field.Type.Kind()
		if field.Type.PkgPath() != "" || tag.inline || tag.block || tag.ident ||
			kind != reflect.String && kind != reflect.Bool &&
				(kind < reflect.Int || kind > reflect.Int64) &&
				(kind < reflect.Uint || kind > reflect.Uint64) &&
				(kind < reflect.Float32 || kind > reflect.Float64) {
			layout.ok = false
			break
		}
		name := tag.name
		if name == "" {
			name = lowerFirst(field.Name)
		}
		layout.fields = append(layout.fields, simpleMarshalField{name: name, index: i, kind: kind, omitEmpty: tag.omitEmpty, sensitive: tag.sensitive})
	}
	if len(layout.fields) == 0 {
		layout.ok = false
	}
	actual, _ := simpleMarshalLayouts.LoadOrStore(typ, layout)
	return actual.(simpleMarshalLayout)
}

// unmarshalSimpleStruct handles a deliberately small, unambiguous subset of
// BCL. Anything with language semantics is decoded by the normal compiler.
// Validate the entire input before touching the destination so a fallback can
// still produce the same result and diagnostics as before.
func unmarshalSimpleStruct(data []byte, target any) bool {
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return false
	}
	if _, ok := target.(encoding.TextUnmarshaler); ok {
		return false
	}
	dst := rv.Elem()
	layout := simpleStructLayout(dst.Type())
	type entry struct {
		keyStart, keyEnd int
		start, end       int
		field            int
		number           int64
		kind             byte
	}
	var entries [16]entry
	count := 0
	for pos := 0; pos < len(data); {
		for pos < len(data) && (data[pos] == ' ' || data[pos] == '\t' || data[pos] == '\r' || data[pos] == '\n') {
			pos++
		}
		if pos == len(data) {
			break
		}
		if count == len(entries) || !simpleNameStart(data[pos]) {
			return false
		}
		item := &entries[count]
		item.keyStart = pos
		pos++
		for pos < len(data) && simpleNamePart(data[pos]) {
			pos++
		}
		item.keyEnd = pos
		name := string(data[item.keyStart:item.keyEnd])
		if name == "type" || name == "import" || name == "const" || name == "param" || name == "schema" || name == "env_file" || name == "env_files" || name == "not" || isCommandStatement(name) {
			return false
		}
		field, ok := layout[name]
		if !ok || field.index < 0 {
			return false
		}
		item.field = field.index
		for i := 0; i < count; i++ {
			if len(name) == entries[i].keyEnd-entries[i].keyStart && name == string(data[entries[i].keyStart:entries[i].keyEnd]) {
				return false
			}
		}
		if pos == len(data) || (data[pos] != ' ' && data[pos] != '\t') {
			return false
		}
		for pos < len(data) && (data[pos] == ' ' || data[pos] == '\t') {
			pos++
		}
		if pos == len(data) {
			return false
		}
		item.start = pos
		switch data[pos] {
		case '"':
			pos++
			item.start = pos
			for pos < len(data) && data[pos] != '"' {
				if data[pos] == '\\' || data[pos] == '$' || data[pos] == '\n' || data[pos] == '\r' || data[pos] < ' ' {
					return false
				}
				pos++
			}
			if pos == len(data) {
				return false
			}
			item.end = pos
			pos++
			item.kind = 's'
			if field.kind != reflect.String {
				return false
			}
		default:
			for pos < len(data) && data[pos] != '\n' && data[pos] != '\r' && data[pos] != ' ' && data[pos] != '\t' {
				pos++
			}
			item.end = pos
			value := string(data[item.start:item.end])
			switch value {
			case "true", "false":
				if field.kind != reflect.Bool {
					return false
				}
				item.kind = 'b'
			default:
				if field.kind < reflect.Int || field.kind > reflect.Int64 {
					return false
				}
				n, err := strconv.ParseInt(value, 10, field.bits)
				if err != nil {
					return false
				}
				item.number = n
				item.kind = 'i'
			}
		}
		for pos < len(data) && (data[pos] == ' ' || data[pos] == '\t' || data[pos] == '\r') {
			pos++
		}
		if pos < len(data) && data[pos] != '\n' {
			return false
		}
		count++
	}
	if count == 0 {
		return false
	}
	for i := 0; i < count; i++ {
		item := &entries[i]
		field := dst.Field(item.field)
		switch item.kind {
		case 's':
			field.SetString(string(data[item.start:item.end]))
		case 'b':
			field.SetBool(data[item.start] == 't')
		case 'i':
			field.SetInt(item.number)
		}
	}
	return true
}

func simpleNameStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func simpleNamePart(c byte) bool {
	return simpleNameStart(c) || c >= '0' && c <= '9' || c == '-'
}

func simpleStructLayout(typ reflect.Type) map[string]simpleField {
	if cached, ok := simpleStructLayouts.Load(typ); ok {
		return cached.(map[string]simpleField)
	}
	layout := make(map[string]simpleField, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" || field.Type.PkgPath() != "" {
			continue
		}
		tag := field.Tag.Get("bcl")
		if tag == "-" || strings.Contains(tag, ",inline") || strings.Contains(tag, ",block") || strings.Contains(tag, ",id") {
			continue
		}
		fieldName, _, _ := strings.Cut(tag, ",")
		if fieldName == "" {
			fieldName = parseJSONName(field.Tag.Get("json"))
		}
		if fieldName == "" {
			fieldName = lowerFirst(field.Name)
		}
		if existing, ok := layout[fieldName]; ok {
			existing.index = -1
			layout[fieldName] = existing
		} else {
			kind := field.Type.Kind()
			bits := 0
			if kind >= reflect.Int && kind <= reflect.Int64 {
				bits = field.Type.Bits()
			}
			layout[fieldName] = simpleField{index: i, kind: kind, bits: bits}
		}
	}
	actual, _ := simpleStructLayouts.LoadOrStore(typ, layout)
	return actual.(map[string]simpleField)
}
