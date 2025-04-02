package bcl

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

func MarshalAST(nodes []Node) string {
	var parts []string
	for _, n := range nodes {
		parts = append(parts, n.ToBCL(""))
	}
	return strings.Join(parts, "\n")
}

func UnmarshalBCL(data []byte, v any) ([]Node, error) {
	topEnv := NewEnv(nil)
	parser := NewParser(string(data))
	nodes, err := parser.Parse()
	if err != nil {
		return nodes, err
	}
	for _, node := range nodes {
		_, err := node.Eval(topEnv)
		if err != nil {
			return nodes, err
		}
	}

	for key, val := range topEnv.vars {
		if m, ok := val.(map[string]any); ok {
			var blocks []any
			for _, item := range m {
				if im, ok := item.(map[string]any); ok {
					if _, exists := im["__type"]; exists {
						blocks = append(blocks, im)
					}
				}
			}

			if len(blocks) >= 1 {
				topEnv.vars[key] = blocks
				for _, block := range blocks {
					if bm, ok := block.(map[string]any); ok {
						if label, exists := bm["__label"].(string); exists {
							delete(topEnv.vars, label)
						}
					}
				}
			}
		}
	}
	flattenBlocksRecursively(topEnv.vars)
	destVal := reflect.ValueOf(v)
	if destVal.Kind() != reflect.Ptr {
		return nodes, errors.New("v must be a pointer")
	}
	destVal = destVal.Elem()
	switch destVal.Kind() {
	case reflect.Map:
		if destVal.IsNil() {
			destVal.Set(reflect.MakeMap(destVal.Type()))
		}
		for k, val := range topEnv.vars {
			destVal.SetMapIndex(reflect.ValueOf(k), reflect.ValueOf(val))
		}
	case reflect.Struct:
		typ := destVal.Type()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			tag := field.Tag.Get("json")
			name := field.Name
			if tag != "" {
				parts := strings.Split(tag, ",")
				if len(parts) > 0 && parts[0] != "" {
					name = parts[0]
				}
			}
			if value, ok := topEnv.vars[name]; ok {
				destField := destVal.Field(i)
				if destField.CanSet() {
					destField.Set(reflect.ValueOf(value))
				}
			}
		}
	default:
		return nodes, errors.New("unsupported type for unmarshal")
	}
	return nodes, nil
}

func Unmarshal(data []byte, v any) ([]Node, error) {
	var intermediate map[string]any
	nodes, err := UnmarshalBCL(data, &intermediate)
	if err != nil {
		return nodes, err
	}
	return nodes, convertMap(intermediate, v)
}

func Marshal(v any) (string, error) {
	result, err := marshalValue(reflect.ValueOf(v), "")
	if err != nil {
		return "", err
	}
	return result, nil
}

func marshalValue(val reflect.Value, indent string) (string, error) {
	if !val.IsValid() {
		return "null", nil
	}
	if val.CanInterface() {
		if node, ok := val.Interface().(Node); ok {
			return node.ToBCL(indent), nil
		}
	}
	switch val.Kind() {
	case reflect.Interface:
		if val.IsNil() {
			return "null", nil
		}
		return marshalValue(val.Elem(), indent)
	case reflect.Ptr:
		if val.IsNil() {
			return "null", nil
		}
		return marshalValue(val.Elem(), indent)
	case reflect.Struct:
		var sb strings.Builder
		// estimate capacity based on number of fields and indent length
		sb.Grow((val.NumField() + 1) * (len(indent) + 32))
		sb.WriteString("{\n")
		typ := val.Type()
		for i := 0; i < val.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			fieldName := field.Name
			if tag := field.Tag.Get("json"); tag != "" {
				parts := strings.Split(tag, ",")
				if parts[0] != "" {
					fieldName = parts[0]
				}
			}
			fieldVal, err := marshalValue(val.Field(i), indent+"    ")
			if err != nil {
				return "", err
			}
			sb.WriteString(fmt.Sprintf("%s    %s = %s\n", indent, fieldName, fieldVal))
		}
		sb.WriteString(indent + "}")
		return sb.String(), nil
	case reflect.Map:
		var sb strings.Builder
		sb.Grow(128)
		sb.WriteString("{\n")
		for _, key := range val.MapKeys() {
			k, err := marshalValue(key, "")
			if err != nil {
				return "", err
			}
			v, err := marshalValue(val.MapIndex(key), indent+"    ")
			if err != nil {
				return "", err
			}
			sb.WriteString(fmt.Sprintf("%s    %s = %s\n", indent, k, v))
		}
		sb.WriteString(indent + "}")
		return sb.String(), nil
	case reflect.Slice, reflect.Array:
		if val.Len() == 0 {
			return "[]", nil
		}
		allPrimitive := true
		for i := 0; i < val.Len(); i++ {
			kind := val.Index(i).Kind()
			if kind != reflect.String && kind != reflect.Int && kind != reflect.Float64 &&
				kind != reflect.Float32 && kind != reflect.Bool {
				allPrimitive = false
				break
			}
		}
		if allPrimitive {
			var parts []string
			for i := 0; i < val.Len(); i++ {
				elem, err := marshalValue(val.Index(i), "")
				if err != nil {
					return "", err
				}
				parts = append(parts, elem)
			}
			return fmt.Sprintf("[%s]", strings.Join(parts, ", ")), nil
		}
		var sb strings.Builder
		sb.WriteString("[\n")
		for i := 0; i < val.Len(); i++ {
			elem, err := marshalValue(val.Index(i), indent+"    ")
			if err != nil {
				return "", err
			}
			sb.WriteString(fmt.Sprintf("%s    %s\n", indent, elem))
		}
		sb.WriteString(indent + "]")
		return sb.String(), nil
	case reflect.String:
		str := val.String()
		if strings.Contains(str, " ") {
			return fmt.Sprintf("\"%s\"", str), nil
		}
		return str, nil
	case reflect.Bool:
		return fmt.Sprintf("%t", val.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", val.Int()), nil
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%v", val.Float()), nil
	default:
		return "", fmt.Errorf("unsupported type: %s", val.Kind())
	}
}
