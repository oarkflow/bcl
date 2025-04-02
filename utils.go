package bcl

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/oarkflow/date"
)

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
								delete(t, label)
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
	if dest.Kind() == reflect.Ptr {
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
		srcMap, ok := src.Interface().(map[string]any)
		if !ok {
			return fmt.Errorf("expected map for struct assignment but got %T", src.Interface())
		}
		destType := dest.Type()
		for i := 0; i < dest.NumField(); i++ {
			field := destType.Field(i)
			if field.PkgPath != "" {
				continue
			}
			tag := field.Tag.Get("json")
			fieldName := field.Name
			if tag != "" {
				parts := strings.Split(tag, ",")
				if parts[0] != "" {
					fieldName = parts[0]
				}
			}
			if value, exists := srcMap[fieldName]; exists {
				if err := assignValue(reflect.ValueOf(value), dest.Field(i)); err != nil {
					return fmt.Errorf("field %s: %v", field.Name, err)
				}
			}
		}
	case reflect.Map:
		if src.Kind() != reflect.Map {
			return fmt.Errorf("expected map for assignment but got %T", src.Interface())
		}
		newMap := reflect.MakeMap(dest.Type())
		for _, key := range src.MapKeys() {
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

func writeAssignments(indent string, entries []*AssignmentNode) string {
	var sb strings.Builder
	for _, entry := range entries {
		sb.WriteString(indent + "    " + entry.ToBCL("") + "\n")
	}
	return sb.String()
}

type Function func(args ...any) (any, error)

var funcRegistry = map[string]Function{}

func RegisterFunction(name string, fn Function) {
	funcRegistry[strings.ToLower(name)] = fn
}

func lookupFunction(name string) (Function, bool) {
	fn, ok := funcRegistry[strings.ToLower(name)]
	return fn, ok
}
