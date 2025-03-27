package utils

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
)

func Convert(src any, dst reflect.Value) error {
	if !dst.CanSet() {
		return errors.New("destination cannot be set")
	}
	switch dst.Kind() {
	case reflect.Interface:
		v := reflect.ValueOf(src)
		if v.Type().AssignableTo(dst.Type()) {
			dst.Set(v)
		} else if v.Type().ConvertibleTo(dst.Type()) {
			dst.Set(v.Convert(dst.Type()))
		} else {
			dst.Set(v)
		}
	case reflect.String:
		switch v := src.(type) {
		case string:
			dst.SetString(v)
		default:
			dst.SetString(fmt.Sprintf("%v", v))
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch v := src.(type) {
		case float64:
			dst.SetInt(int64(v))
		case int:
			dst.SetInt(int64(v))
		case string:
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			dst.SetInt(int64(n))
		default:
			return fmt.Errorf("cannot convert %v to int", src)
		}
	case reflect.Float32, reflect.Float64:
		switch v := src.(type) {
		case float64:
			dst.SetFloat(v)
		case int:
			dst.SetFloat(float64(v))
		case string:
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return err
			}
			dst.SetFloat(n)
		default:
			return fmt.Errorf("cannot convert %v to float", src)
		}
	case reflect.Bool:
		switch v := src.(type) {
		case bool:
			dst.SetBool(v)
		case string:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return err
			}
			dst.SetBool(b)
		default:
			return fmt.Errorf("cannot convert %v to bool", src)
		}
	case reflect.Map:
		srcMap, ok := src.(map[string]any)
		if !ok {
			return fmt.Errorf("expected map for conversion to map, got %T", src)
		}
		dst.Set(reflect.MakeMapWithSize(dst.Type(), len(srcMap)))
		for key, val := range srcMap {
			k := reflect.ValueOf(key)
			v := reflect.New(dst.Type().Elem()).Elem()
			if err := Convert(val, v); err != nil {
				return err
			}
			dst.SetMapIndex(k, v)
		}
	case reflect.Struct:
		srcMap, ok := src.(map[string]any)
		if !ok {
			return fmt.Errorf("expected map for conversion to struct, got %T", src)
		}
		for i := 0; i < dst.NumField(); i++ {
			field := dst.Type().Field(i)
			fieldName := field.Tag.Get("bcl")
			if fieldName == "" {
				fieldName = field.Tag.Get("json")
			}
			if fieldName == "" {
				fieldName = field.Name
			}
			if val, exists := srcMap[fieldName]; exists {
				if err := Convert(val, dst.Field(i)); err != nil {
					return err
				}
			}
		}
	case reflect.Slice:
		rv := reflect.ValueOf(src)
		if rv.Kind() != reflect.Slice {
			return fmt.Errorf("expected slice for conversion, got %T", src)
		}
		newSlice := reflect.MakeSlice(dst.Type(), rv.Len(), rv.Len())
		for i := 0; i < rv.Len(); i++ {
			if err := Convert(rv.Index(i).Interface(), newSlice.Index(i)); err != nil {
				return err
			}
		}
		dst.Set(newSlice)
	default:
		return fmt.Errorf("unsupported destination type: %v", dst.Kind())
	}
	return nil
}
