package bcl

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// Validator defines the interface for value validation
type Validator interface {
	Validate(value interface{}) error
}

// ValidationRule represents a validation rule
type ValidationRule struct {
	Field      string
	Validators []Validator
}

// Schema represents a validation schema for BCL data
type Schema struct {
	Rules map[string][]Validator
}

// NewSchema creates a new validation schema
func NewSchema() *Schema {
	return &Schema{
		Rules: make(map[string][]Validator),
	}
}

// AddRule adds a validation rule for a field
func (s *Schema) AddRule(field string, validators ...Validator) {
	s.Rules[field] = append(s.Rules[field], validators...)
}

// Validate validates data against the schema
func (s *Schema) Validate(data interface{}) error {
	var errors MultiError
	s.validateValue("", data, &errors)

	if errors.HasErrors() {
		return &errors
	}
	return nil
}

func (s *Schema) validateValue(path string, value interface{}, errors *MultiError) {
	// First check if the current path has validators
	if path != "" {
		if validators, ok := s.Rules[path]; ok {
			for _, validator := range validators {
				if err := validator.Validate(value); err != nil {
					errors.Add(&ValidationError{
						Field:   path,
						Value:   value,
						Message: err.Error(),
					})
				}
			}
		}
	}

	switch v := value.(type) {
	case map[string]interface{}:
		// Check for missing required fields at this level
		for fieldPath, validators := range s.Rules {
			// Only check direct children of current path
			if path == "" && !strings.Contains(fieldPath, ".") {
				// Top-level field
				if _, exists := v[fieldPath]; !exists {
					// Check if field is required
					for _, validator := range validators {
						if _, isRequired := validator.(RequiredValidator); isRequired {
							errors.Add(&ValidationError{
								Field:   fieldPath,
								Value:   nil,
								Message: "value is required",
							})
						}
					}
				}
			} else if path != "" && strings.HasPrefix(fieldPath, path+".") {
				// Nested field
				childField := strings.TrimPrefix(fieldPath, path+".")
				if !strings.Contains(childField, ".") {
					if _, exists := v[childField]; !exists {
						// Check if field is required
						for _, validator := range validators {
							if _, isRequired := validator.(RequiredValidator); isRequired {
								errors.Add(&ValidationError{
									Field:   fieldPath,
									Value:   nil,
									Message: "value is required",
								})
							}
						}
					}
				}
			}
		}

		// Validate existing fields
		for key, val := range v {
			fieldPath := path
			if fieldPath == "" {
				fieldPath = key
			} else {
				fieldPath = fieldPath + "." + key
			}

			// Check if there are rules for this field
			if validators, ok := s.Rules[fieldPath]; ok {
				for _, validator := range validators {
					if err := validator.Validate(val); err != nil {
						errors.Add(&ValidationError{
							Field:   fieldPath,
							Value:   val,
							Message: err.Error(),
						})
					}
				}
			}

			// Recursively validate nested structures
			s.validateValue(fieldPath, val, errors)
		}

	case []interface{}:
		for i, item := range v {
			itemPath := fmt.Sprintf("%s[%d]", path, i)
			s.validateValue(itemPath, item, errors)
		}
	}
}

// Built-in validators

// RequiredValidator ensures a value is not nil or empty
type RequiredValidator struct{}

func (v RequiredValidator) Validate(value interface{}) error {
	if value == nil {
		return fmt.Errorf("value is required")
	}

	// Check for empty values
	switch val := value.(type) {
	case string:
		if val == "" {
			return fmt.Errorf("value cannot be empty")
		}
	case []interface{}:
		if len(val) == 0 {
			return fmt.Errorf("array cannot be empty")
		}
	case map[string]interface{}:
		if len(val) == 0 {
			return fmt.Errorf("object cannot be empty")
		}
	}

	return nil
}

// TypeValidator ensures a value is of a specific type
type TypeValidator struct {
	Type string
}

func (v TypeValidator) Validate(value interface{}) error {
	if value == nil {
		return nil // nil is valid for any type
	}

	switch v.Type {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
	case "int", "integer":
		switch value.(type) {
		case int, int8, int16, int32, int64:
			// valid
		default:
			return fmt.Errorf("expected integer, got %T", value)
		}
	case "float", "number":
		switch value.(type) {
		case float32, float64, int, int8, int16, int32, int64:
			// valid
		default:
			return fmt.Errorf("expected number, got %T", value)
		}
	case "bool", "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", value)
		}
	case "array", "slice":
		if _, ok := value.([]interface{}); !ok {
			return fmt.Errorf("expected array, got %T", value)
		}
	case "object", "map":
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Errorf("expected object, got %T", value)
		}
	default:
		return fmt.Errorf("unknown type: %s", v.Type)
	}

	return nil
}

// RangeValidator ensures a numeric value is within a range
type RangeValidator struct {
	Min *float64
	Max *float64
}

func (v RangeValidator) Validate(value interface{}) error {
	if value == nil {
		return nil
	}

	var num float64
	switch val := value.(type) {
	case int:
		num = float64(val)
	case int8:
		num = float64(val)
	case int16:
		num = float64(val)
	case int32:
		num = float64(val)
	case int64:
		num = float64(val)
	case float32:
		num = float64(val)
	case float64:
		num = val
	default:
		return fmt.Errorf("value must be numeric for range validation")
	}

	if v.Min != nil && num < *v.Min {
		return fmt.Errorf("value %v is less than minimum %v", num, *v.Min)
	}

	if v.Max != nil && num > *v.Max {
		return fmt.Errorf("value %v is greater than maximum %v", num, *v.Max)
	}

	return nil
}

// LengthValidator ensures string or array length is within bounds
type LengthValidator struct {
	Min *int
	Max *int
}

func (v LengthValidator) Validate(value interface{}) error {
	if value == nil {
		return nil
	}

	var length int
	switch val := value.(type) {
	case string:
		length = len(val)
	case []interface{}:
		length = len(val)
	default:
		return fmt.Errorf("length validation only applies to strings and arrays")
	}

	if v.Min != nil && length < *v.Min {
		return fmt.Errorf("length %d is less than minimum %d", length, *v.Min)
	}

	if v.Max != nil && length > *v.Max {
		return fmt.Errorf("length %d is greater than maximum %d", length, *v.Max)
	}

	return nil
}

// PatternValidator ensures a string matches a regex pattern
type PatternValidator struct {
	Pattern *regexp.Regexp
}

func NewPatternValidator(pattern string) (*PatternValidator, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}
	return &PatternValidator{Pattern: re}, nil
}

func (v PatternValidator) Validate(value interface{}) error {
	if value == nil {
		return nil
	}

	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("pattern validation only applies to strings")
	}

	if !v.Pattern.MatchString(str) {
		return fmt.Errorf("value does not match pattern %s", v.Pattern.String())
	}

	return nil
}

// EnumValidator ensures a value is one of allowed values
type EnumValidator struct {
	Values []interface{}
}

func (v EnumValidator) Validate(value interface{}) error {
	if value == nil {
		return nil
	}

	for _, allowed := range v.Values {
		if reflect.DeepEqual(value, allowed) {
			return nil
		}
	}

	return fmt.Errorf("value %v is not in allowed values: %v", value, v.Values)
}

// CustomValidator allows custom validation logic
type CustomValidator struct {
	ValidateFunc func(value interface{}) error
}

func (v CustomValidator) Validate(value interface{}) error {
	return v.ValidateFunc(value)
}

// Helper functions for creating validators

// Required creates a required validator
func Required() Validator {
	return RequiredValidator{}
}

// Type creates a type validator
func Type(t string) Validator {
	return TypeValidator{Type: t}
}

// Min creates a range validator with minimum value
func Min(min float64) Validator {
	return RangeValidator{Min: &min}
}

// Max creates a range validator with maximum value
func Max(max float64) Validator {
	return RangeValidator{Max: &max}
}

// Range creates a range validator with min and max
func Range(min, max float64) Validator {
	return RangeValidator{Min: &min, Max: &max}
}

// MinLength creates a length validator with minimum length
func MinLength(min int) Validator {
	return LengthValidator{Min: &min}
}

// MaxLength creates a length validator with maximum length
func MaxLength(max int) Validator {
	return LengthValidator{Max: &max}
}

// Length creates a length validator with min and max
func Length(min, max int) Validator {
	return LengthValidator{Min: &min, Max: &max}
}

// Pattern creates a pattern validator
func Pattern(pattern string) (Validator, error) {
	return NewPatternValidator(pattern)
}

// Enum creates an enum validator
func Enum(values ...interface{}) Validator {
	return EnumValidator{Values: values}
}

// Custom creates a custom validator
func Custom(fn func(value interface{}) error) Validator {
	return CustomValidator{ValidateFunc: fn}
}

// ValidateStruct validates a struct against BCL data using struct tags
func ValidateStruct(v interface{}, data map[string]interface{}) error {
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return fmt.Errorf("ValidateStruct requires a struct, got %T", v)
	}

	typ := val.Type()
	var errors MultiError

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Skip unexported fields
		if field.PkgPath != "" {
			continue
		}

		// Get field name from tag
		tag := field.Tag.Get("bcl")
		if tag == "" {
			tag = field.Tag.Get("json")
		}
		if tag == "" {
			tag = field.Name
		}

		// Parse tag for validation rules
		parts := strings.Split(tag, ",")
		fieldName := parts[0]

		// Get value from data
		dataVal, exists := data[fieldName]

		// Check required
		for _, part := range parts[1:] {
			switch part {
			case "required":
				if !exists || dataVal == nil {
					errors.Add(&ValidationError{
						Field:   fieldName,
						Value:   nil,
						Message: "field is required",
					})
				}
			case "omitempty":
				// Skip validation if empty
				if !exists || dataVal == nil {
					continue
				}
			}
		}

		// Validate type if value exists
		if exists && dataVal != nil {
			expectedType := field.Type
			if !isTypeCompatible(dataVal, expectedType) {
				errors.Add(&ValidationError{
					Field:   fieldName,
					Value:   dataVal,
					Message: fmt.Sprintf("type mismatch: expected %s, got %T", expectedType, dataVal),
				})
			}
		}
	}

	if errors.HasErrors() {
		return &errors
	}
	return nil
}

// isTypeCompatible checks if a value is compatible with a reflect.Type
func isTypeCompatible(value interface{}, typ reflect.Type) bool {
	if value == nil {
		return true // nil is compatible with any pointer type
	}

	valType := reflect.TypeOf(value)

	// Direct type match
	if valType == typ {
		return true
	}

	// Check numeric compatibility
	if isNumericType(valType) && isNumericType(typ) {
		return true
	}

	// Check if value can be converted to target type
	if valType.ConvertibleTo(typ) {
		return true
	}

	return false
}

// isNumericType checks if a type is numeric
func isNumericType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}
