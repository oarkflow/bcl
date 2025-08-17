package bcl

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/goccy/go-reflect"
	"github.com/oarkflow/date"
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
	cleanStructure(intermediate)
	return nodes, convertMap(intermediate, v)
}

func cleanStructure(data any) {
	switch t := data.(type) {
	case map[string]any:
		for _, v := range t {
			cleanStructure(v)
		}
		type blockInfo struct {
			typeName string
			props    map[string]any
		}
		var blocks []blockInfo
		var keysToDelete []string
		for k, v := range t {
			if m, ok := v.(map[string]any); ok {
				if typeName, exists := m["__type"].(string); exists {
					label, _ := m["__label"].(string)
					props := make(map[string]any)
					props["name"] = label
					if propsMap, ok := m["props"].(map[string]any); ok {
						for pk, pv := range propsMap {
							props[pk] = pv
						}
					}
					for pk, pv := range m {
						if pk == "__type" || pk == "__label" || pk == "props" {
							continue
						}
						props[pk] = pv
					}

					blocks = append(blocks, blockInfo{typeName, props})
					keysToDelete = append(keysToDelete, k)
				}
			}
		}

		// Remove original block keys
		for _, k := range keysToDelete {
			delete(t, k)
		}

		// Reinsert blocks under their type keys, checking for duplicates
		for _, block := range blocks {
			existing := t[block.typeName]
			switch val := existing.(type) {
			case nil:
				// New entry
				t[block.typeName] = block.props
			case map[string]any:
				// Check if existing has the same name
				if val["name"] == block.props["name"] {
					// Merge or replace (here we replace for simplicity)
					t[block.typeName] = block.props
				} else {
					// Convert to slice
					t[block.typeName] = []any{val, block.props}
				}
			case []any:
				// Check if any element has the same name
				found := -1
				for i, item := range val {
					if m, ok := item.(map[string]any); ok && m["name"] == block.props["name"] {
						found = i
						break
					}
				}
				if found >= 0 {
					// Replace existing entry
					val[found] = block.props
					t[block.typeName] = val
				} else {
					t[block.typeName] = append(val, block.props)
				}
			default:
				t[block.typeName] = block.props
			}
		}

	case []any:
		// Process slice elements recursively
		for i := range t {
			cleanStructure(t[i])
		}
	}
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

func flattenBlocksRecursively(vars any) {
	switch t := vars.(type) {
	case map[string]any:
		for _, v := range t {
			flattenBlocksRecursively(v)
		}
		for key, val := range t {
			if container, ok := val.(map[string]any); ok {
				allBlocks := true
				var blockSlice []any
				for _, item := range container {
					if m, ok := item.(map[string]any); ok {
						if _, exists := m["__type"]; !exists {
							allBlocks = false
							break
						} else {
							blockSlice = append(blockSlice, m)
						}
					} else {
						allBlocks = false
						break
					}
				}
				if allBlocks && len(blockSlice) > 0 {
					t[key] = blockSlice
					for _, m := range blockSlice {
						if bm, ok := m.(map[string]any); ok {
							if label, exists := bm["__label"].(string); exists {
								if label != "name" {
									delete(t, label)
								}
							}
						}
					}
				}
			}
		}
	case []any:
		for i, item := range t {
			flattenBlocksRecursively(item)
			if container, ok := item.(map[string]any); ok {
				if props, exists := container["props"]; exists {
					t[i] = props
					flattenBlocksRecursively(props)
				}
			}
		}
	}
}

func convertMap(src map[string]any, v any) error {
	destVal := reflect.ValueOf(v)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() {
		return errors.New("v must be a non-nil pointer")
	}
	return assignValue(reflect.ValueOf(src), destVal.Elem())
}

func assignValue(src, dest reflect.Value) error {
	if !dest.IsValid() {
		return errors.New("invalid destination")
	}
	if !src.IsValid() || (src.Kind() == reflect.Interface && src.IsNil()) {
		dest.Set(reflect.Zero(dest.Type()))
		return nil
	} else if dest.Kind() == reflect.Ptr {
		if dest.IsNil() {
			dest.Set(reflect.New(dest.Type().Elem()))
		}
		return assignValue(src, dest.Elem())
	}
	// NEW: Special handling for time.Time (struct) conversion from string
	if dest.Kind() == reflect.Struct && dest.Type() == reflect.TypeOf(time.Time{}) {
		if str, ok := src.Interface().(string); ok {
			t, err := date.Parse(str)
			if err != nil {
				return fmt.Errorf("cannot parse time: %v", err)
			}
			dest.Set(reflect.ValueOf(t))
			return nil
		}
		return fmt.Errorf("expected string for time conversion but got %T", src.Interface())
	}
	switch dest.Kind() {
	case reflect.Struct:
		srcVal := src
		if src.Kind() == reflect.Slice {
			if src.Len() == 0 {
				return nil
			}
			srcVal = src.Index(src.Len() - 1)
		}
		srcMap, ok := srcVal.Interface().(map[string]any)
		if !ok {
			return fmt.Errorf("expected map for struct assignment but got %T", srcVal.Interface())
		}
		destType := dest.Type()
		for i := 0; i < dest.NumField(); i++ {
			field := destType.Field(i)
			if field.PkgPath != "" {
				continue
			}
			// Support json, bcl, hcl tags and filter parsing
			tag := field.Tag.Get("json")
			if tag == "" {
				tag = field.Tag.Get("bcl")
			}
			if tag == "" {
				tag = field.Tag.Get("hcl")
			}
			fieldName := field.Name
			filter := ""
			if tag != "" {
				parts := strings.Split(tag, ",")
				if len(parts) > 0 && parts[0] != "" {
					fieldName = parts[0]
				}
				// Accept filter as either : or , separator
				filterIdx := strings.Index(fieldName, ":")
				if filterIdx == -1 && len(parts) > 1 {
					filter = parts[1]
				} else if filterIdx != -1 {
					filter = fieldName[filterIdx+1:]
					fieldName = fieldName[:filterIdx]
				}
			}
			if strings.HasPrefix(fieldName, "__") {
				continue
			}
			if value, exists := srcMap[fieldName]; exists {
				fieldVal := dest.Field(i)
				val := reflect.ValueOf(value)
				if filter != "" && val.Kind() == reflect.Slice {
					val = applyFilter(val, filter)
					if strings.HasPrefix(filter, "name:") && fieldVal.Kind() == reflect.Struct {
						if val.Len() == 0 {
							fieldVal.Set(reflect.Zero(fieldVal.Type()))
							continue
						}
						val = val.Index(0)
					}
				}
				if fieldVal.Kind() == reflect.Slice {
					if val.Kind() != reflect.Slice {
						slice := reflect.MakeSlice(fieldVal.Type(), 1, 1)
						if err := assignValue(val, slice.Index(0)); err != nil {
							return fmt.Errorf("field %s: %v", field.Name, err)
						}
						fieldVal.Set(slice)
					} else {
						if err := assignValue(val, fieldVal); err != nil {
							return fmt.Errorf("field %s: %v", field.Name, err)
						}
					}
				} else if fieldVal.Kind() == reflect.Struct {
					if val.Kind() == reflect.Slice && val.Len() > 0 {
						val = val.Index(val.Len() - 1)
					}
					if err := assignValue(val, fieldVal); err != nil {
						return fmt.Errorf("field %s: %v", field.Name, err)
					}
				} else {
					if err := assignValue(val, fieldVal); err != nil {
						return fmt.Errorf("field %s: %v", field.Name, err)
					}
				}
			}
		}
	case reflect.Map:
		if src.Kind() != reflect.Map {
			return fmt.Errorf("expected map for assignment but got %T", src.Interface())
		}
		newMap := reflect.MakeMap(dest.Type())
		for _, key := range src.MapKeys() {
			// Skip metadata keys if key is string.
			if key.Kind() == reflect.String && strings.HasPrefix(key.String(), "__") {
				continue
			}
			srcVal := src.MapIndex(key)
			destKey := reflect.New(dest.Type().Key()).Elem()
			if err := assignValue(key, destKey); err != nil {
				return err
			}
			destVal := reflect.New(dest.Type().Elem()).Elem()
			if err := assignValue(srcVal, destVal); err != nil {
				return err
			}
			newMap.SetMapIndex(destKey, destVal)
		}
		dest.Set(newMap)
	case reflect.Slice:
		// If src is not a slice, wrap it as a slice
		if src.Kind() != reflect.Slice {
			slice := reflect.MakeSlice(dest.Type(), 1, 1)
			if err := assignValue(src, slice.Index(0)); err != nil {
				return err
			}
			dest.Set(slice)
			return nil
		}
		srcSlice, ok := src.Interface().([]any)
		if !ok {
			return fmt.Errorf("expected slice for assignment but got %T", src.Interface())
		}
		slice := reflect.MakeSlice(dest.Type(), len(srcSlice), len(srcSlice))
		for i, item := range srcSlice {
			if err := assignValue(reflect.ValueOf(item), slice.Index(i)); err != nil {
				return fmt.Errorf("slice index %d: %v", i, err)
			}
		}
		dest.Set(slice)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch v := src.Interface().(type) {
		case float64:
			dest.SetInt(int64(v))
		case int:
			dest.SetInt(int64(v))
		default:
			return fmt.Errorf("cannot convert %T to int", src.Interface())
		}
	case reflect.Float32, reflect.Float64:
		switch v := src.Interface().(type) {
		case float64:
			dest.SetFloat(v)
		case int:
			dest.SetFloat(float64(v))
		default:
			return fmt.Errorf("cannot convert %T to float", src.Interface())
		}
	case reflect.Bool:
		switch v := src.Interface().(type) {
		case bool:
			dest.SetBool(v)
		case int:
			dest.SetBool(v != 0)
		case float64:
			dest.SetBool(v != 0)
		default:
			return fmt.Errorf("cannot convert %T to bool", src.Interface())
		}
	case reflect.String:
		if v, ok := src.Interface().(string); ok {
			dest.SetString(v)
		} else {
			return fmt.Errorf("cannot convert %T to string", src.Interface())
		}
	default:
		if src.Type().AssignableTo(dest.Type()) {
			dest.Set(src)
		} else {
			return fmt.Errorf("unsupported destination type: %s", dest.Kind())
		}
	}
	return nil
}

// Filter helpers for struct tags
func applyFilter(val reflect.Value, filter string) reflect.Value {
	if val.Kind() != reflect.Slice || val.Len() == 0 {
		return val
	}
	switch {
	case filter == "first":
		return val.Slice(0, 1)
	case filter == "last":
		return val.Slice(val.Len()-1, val.Len())
	case filter == "all":
		return val
	case strings.HasPrefix(filter, "name:"):
		name := filter[5:]
		zero := reflect.MakeSlice(val.Type(), 0, 0)
		for i := 0; i < val.Len(); i++ {
			item := val.Index(i)
			if item.Kind() == reflect.Map {
				// Try direct "name"
				if n := item.MapIndex(reflect.ValueOf("name")); n.IsValid() && n.Interface() == name {
					return reflect.Append(zero, item)
				}
				// Try "props" sub-map
				if props := item.MapIndex(reflect.ValueOf("props")); props.IsValid() {
					propsVal := props
					if propsVal.Kind() == reflect.Interface {
						propsVal = propsVal.Elem()
					}
					if propsVal.Kind() == reflect.Map {
						if n := propsVal.MapIndex(reflect.ValueOf("name")); n.IsValid() && n.Interface() == name {
							return reflect.Append(zero, item)
						}
					}
				}
			} else if item.Kind() == reflect.Struct {
				f := item.FieldByName("Name")
				if f.IsValid() && f.Kind() == reflect.String && f.String() == name {
					return reflect.Append(zero, item)
				}
			}
		}
		return zero
	case strings.Contains(filter, "-"):
		parts := strings.Split(filter, "-")
		if len(parts) == 2 {
			from, to := parseIndex(parts[0]), parseIndex(parts[1])
			if from < 0 {
				from = 0
			}
			if to > val.Len() {
				to = val.Len()
			}
			if from < to {
				return val.Slice(from, to)
			}
		}
	case isNumber(filter):
		idx := parseIndex(filter)
		if idx >= 0 && idx < val.Len() {
			return val.Slice(idx, idx+1)
		}
	}
	return val
}

func isNumber(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

func parseIndex(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func writeAssignments(indent string, assignments []*AssignmentNode) string {
	if len(assignments) == 0 {
		return ""
	}

	sb := getBuilder(len(assignments) * 32)
	defer putBuilder(sb)

	for _, a := range assignments {
		sb.WriteString(indent)
		sb.WriteString("    ")
		sb.WriteString(a.VarName)
		sb.WriteString(" = ")
		sb.WriteString(a.Value.ToBCL(""))
		sb.WriteByte('\n')
	}
	return sb.String()
}

type Function func(args ...any) (any, error)
