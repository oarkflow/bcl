package bcl

import (
	"errors"
	"fmt"
	"strings"
)

func init() {
	// Original functions
	RegisterFunction("isDefined", builtinIsDefined)
	RegisterFunction("isNull", builtinIsNull)
	RegisterFunction("isEmpty", builtinIsEmpty)
	RegisterFunction("upper", upper)
	RegisterFunction("lower", lower)
	RegisterFunction("trim", trim)
	RegisterFunction("contains", contains)
	RegisterFunction("replace", replace)
	RegisterFunction("reverse", reverse)
	RegisterFunction("substring", substring)

	// Additional new functions
	RegisterFunction("split", split)
	RegisterFunction("join", join)
	RegisterFunction("length", length)
	RegisterFunction("index", index)
	RegisterFunction("startsWith", startsWith)
	RegisterFunction("endsWith", endsWith)
	RegisterFunction("repeat", repeat)
}

// --- Original Functions ---

func upper(params ...any) (any, error) {
	if len(params) == 0 {
		return nil, errors.New("at least one param required")
	}
	str, ok := params[0].(string)
	if !ok {
		str = fmt.Sprint(params[0])
	}
	return strings.ToUpper(str), nil
}

func lower(params ...any) (any, error) {
	if len(params) == 0 {
		return nil, errors.New("at least one param required")
	}
	str, ok := params[0].(string)
	if !ok {
		str = fmt.Sprint(params[0])
	}
	return strings.ToLower(str), nil
}

func trim(params ...any) (any, error) {
	if len(params) == 0 {
		return nil, errors.New("at least one param required")
	}
	str, ok := params[0].(string)
	if !ok {
		str = fmt.Sprint(params[0])
	}
	return strings.TrimSpace(str), nil
}

func contains(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("at least two params required")
	}
	haystack, ok1 := params[0].(string)
	needle, ok2 := params[1].(string)
	if !ok1 {
		haystack = fmt.Sprint(params[0])
	}
	if !ok2 {
		needle = fmt.Sprint(params[1])
	}
	return strings.Contains(haystack, needle), nil
}

func replace(params ...any) (any, error) {
	if len(params) < 3 {
		return nil, errors.New("three params required: source, old, new")
	}
	source, ok1 := params[0].(string)
	old, ok2 := params[1].(string)
	newVal, ok3 := params[2].(string)
	if !ok1 {
		source = fmt.Sprint(params[0])
	}
	if !ok2 {
		old = fmt.Sprint(params[1])
	}
	if !ok3 {
		newVal = fmt.Sprint(params[2])
	}
	return strings.ReplaceAll(source, old, newVal), nil
}

func reverse(params ...any) (any, error) {
	if len(params) == 0 {
		return nil, errors.New("at least one param required")
	}
	str, ok := params[0].(string)
	if !ok {
		str = fmt.Sprint(params[0])
	}
	runes := []rune(str)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes), nil
}

func substring(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("at least two params required: source and start index")
	}
	s, ok := params[0].(string)
	if !ok {
		s = fmt.Sprint(params[0])
	}
	start, ok := params[1].(int)
	if !ok {
		return nil, errors.New("start index must be an integer")
	}
	runes := []rune(s)
	if start < 0 || start >= len(runes) {
		return nil, errors.New("start index out of bounds")
	}
	// If length parameter is provided, use it.
	if len(params) >= 3 {
		length, ok := params[2].(int)
		if !ok {
			return nil, errors.New("length must be an integer")
		}
		if length < 0 || start+length > len(runes) {
			return nil, errors.New("length out of bounds")
		}
		return string(runes[start : start+length]), nil
	}
	return string(runes[start:]), nil
}

func builtinIsDefined(params ...any) (any, error) {
	if len(params) == 0 {
		return nil, errors.New("at least one param required")
	}
	_, isUndef := params[0].(Undefined)
	return !isUndef, nil
}

func builtinIsNull(params ...any) (any, error) {
	if len(params) == 0 {
		return nil, errors.New("at least one param required")
	}
	if params[0] == nil {
		return true, nil
	}
	_, isUndef := params[0].(Undefined)
	return isUndef, nil
}

func builtinIsEmpty(params ...any) (any, error) {
	if len(params) == 0 {
		return nil, errors.New("at least one param required")
	}
	switch v := params[0].(type) {
	case string:
		return v == "", nil
	case []any:
		return len(v) == 0, nil
	case map[string]any:
		return len(v) == 0, nil
	}
	return false, nil
}

// --- New Functions ---

// split splits a string into a slice of substrings separated by the given separator.
func split(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("split: requires two parameters: source string and separator")
	}
	source, ok := params[0].(string)
	if !ok {
		source = fmt.Sprint(params[0])
	}
	sep, ok := params[1].(string)
	if !ok {
		sep = fmt.Sprint(params[1])
	}
	parts := strings.Split(source, sep)
	// Convert []string to []any.
	result := make([]any, len(parts))
	for i, part := range parts {
		result[i] = part
	}
	return result, nil
}

// join concatenates the elements of a slice to create a single string with the given separator.
// The first parameter must be a slice of any.
func join(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("join: requires two parameters: slice and separator")
	}
	slice, ok := params[0].([]any)
	if !ok {
		return nil, errors.New("join: first parameter must be a slice of any")
	}
	sep, ok := params[1].(string)
	if !ok {
		sep = fmt.Sprint(params[1])
	}
	// Convert each element of the slice to string.
	strSlice := make([]string, len(slice))
	for i, elem := range slice {
		if s, ok := elem.(string); ok {
			strSlice[i] = s
		} else {
			strSlice[i] = fmt.Sprint(elem)
		}
	}
	return strings.Join(strSlice, sep), nil
}

// length returns the length of a string (in runes), slice, or map.
func length(params ...any) (any, error) {
	if len(params) < 1 {
		return nil, errors.New("length: requires one parameter")
	}
	switch v := params[0].(type) {
	case string:
		return len([]rune(v)), nil
	case []any:
		return len(v), nil
	case map[string]any:
		return len(v), nil
	default:
		return nil, errors.New("length: unsupported type")
	}
}

// index returns the index of the first occurrence of the substring in the source string.
// Returns -1 if not found.
func index(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("index: requires two parameters: source string and substring")
	}
	source, ok := params[0].(string)
	if !ok {
		source = fmt.Sprint(params[0])
	}
	substr, ok := params[1].(string)
	if !ok {
		substr = fmt.Sprint(params[1])
	}
	return strings.Index(source, substr), nil
}

// startsWith returns true if the source string begins with the given prefix.
func startsWith(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("startsWith: requires two parameters: source string and prefix")
	}
	source, ok := params[0].(string)
	if !ok {
		source = fmt.Sprint(params[0])
	}
	prefix, ok := params[1].(string)
	if !ok {
		prefix = fmt.Sprint(params[1])
	}
	return strings.HasPrefix(source, prefix), nil
}

// endsWith returns true if the source string ends with the given suffix.
func endsWith(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("endsWith: requires two parameters: source string and suffix")
	}
	source, ok := params[0].(string)
	if !ok {
		source = fmt.Sprint(params[0])
	}
	suffix, ok := params[1].(string)
	if !ok {
		suffix = fmt.Sprint(params[1])
	}
	return strings.HasSuffix(source, suffix), nil
}

// repeat returns the source string repeated count times.
func repeat(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("repeat: requires two parameters: source string and count")
	}
	source, ok := params[0].(string)
	if !ok {
		source = fmt.Sprint(params[0])
	}
	count, ok := params[1].(int)
	if !ok {
		return nil, errors.New("repeat: count must be an integer")
	}
	if count < 0 {
		return nil, errors.New("repeat: count must be non-negative")
	}
	return strings.Repeat(source, count), nil
}
