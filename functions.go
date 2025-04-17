package bcl

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

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
	result := make([]any, len(parts))
	for i, part := range parts {
		result[i] = part
	}
	return result, nil
}

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

// --- Additional Essential Functions ---

// Helper: convert a value to int.
func toInt(v any) (int, bool) {
	switch value := v.(type) {
	case int:
		return value, true
	case int32:
		return int(value), true
	case int64:
		return int(value), true
	case float32:
		return int(value), true
	case float64:
		return int(value), true
	case string:
		if i, err := strconv.Atoi(value); err == nil {
			return i, true
		}
	}
	return 0, false
}

// add returns the sum of all numeric parameters.
func add(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("add: requires at least two numeric parameters")
	}
	sum := 0.0
	for _, param := range params {
		num, err := toFloat(param)
		if err != nil {
			return nil, errors.New("add: all parameters must be numbers")
		}
		sum += num
	}
	return sum, nil
}

// subtract subtracts all subsequent numbers from the first parameter.
func subtract(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("subtract: requires at least two numeric parameters")
	}
	first, ok := toFloat(params[0])
	if ok != nil {
		return nil, errors.New("subtract: parameters must be numbers")
	}
	result := first
	for _, param := range params[1:] {
		num, ok := toFloat(param)
		if ok != nil {
			return nil, errors.New("subtract: parameters must be numbers")
		}
		result -= num
	}
	return result, nil
}

// multiply returns the product of all numeric parameters.
func multiply(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("multiply: requires at least two numeric parameters")
	}
	result := 1.0
	for _, param := range params {
		num, ok := toFloat(param)
		if ok != nil {
			return nil, errors.New("multiply: parameters must be numbers")
		}
		result *= num
	}
	return result, nil
}

// divide divides the first parameter by the second.
// If more than two parameters are provided, subsequent parameters are ignored.
func divide(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("divide: requires exactly two numeric parameters")
	}
	numerator, ok := toFloat(params[0])
	if ok != nil {
		return nil, errors.New("divide: parameters must be numbers")
	}
	denom, ok := toFloat(params[1])
	if ok != nil {
		return nil, errors.New("divide: parameters must be numbers")
	}
	if denom == 0 {
		return nil, errors.New("divide: division by zero")
	}
	return numerator / denom, nil
}

func dtNow(params ...any) (any, error) {
	now := time.Now()
	if len(params) >= 1 {
		layout, ok := params[0].(string)
		if !ok {
			return nil, errors.New("dtNow: layout must be a string")
		}
		return now.Format(layout), nil
	}
	return now, nil
}

func dtFormat(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("dtFormat: requires two parameters: time value and layout string")
	}
	layout, ok := params[1].(string)
	if !ok {
		return nil, errors.New("dtFormat: layout must be a string")
	}
	var t time.Time
	switch v := params[0].(type) {
	case time.Time:
		t = v
	case string:
		var err error
		t, err = time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, errors.New("dtFormat: unable to parse time string, expected RFC3339 format")
		}
	default:
		return nil, errors.New("dtFormat: unsupported time value")
	}
	return t.Format(layout), nil
}

// dtParse parses a time string using the specified layout and returns a time.Time.
// For example, layout could be "2006-01-02 15:04:05".
func dtParse(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("dtParse: requires two parameters: time string and layout string")
	}
	timeStr, ok := params[0].(string)
	if !ok {
		return nil, errors.New("dtParse: first parameter must be a time string")
	}
	layout, ok := params[1].(string)
	if !ok {
		return nil, errors.New("dtParse: layout must be a string")
	}
	t, err := time.Parse(layout, timeStr)
	if err != nil {
		return nil, fmt.Errorf("dtParse: %v", err)
	}
	return t, nil
}

// dtAdd adds a duration to a given time and returns the resulting time.
// The first parameter is a time value (or string in RFC3339 format),
// and the second parameter is a duration string parseable by time.ParseDuration.
func dtAdd(params ...any) (any, error) {
	if len(params) < 2 {
		return nil, errors.New("dtAdd: requires two parameters: time value and duration string")
	}
	var t time.Time
	switch v := params[0].(type) {
	case time.Time:
		t = v
	case string:
		var err error
		t, err = time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, errors.New("dtAdd: unable to parse time string, expected RFC3339 format")
		}
	default:
		return nil, errors.New("dtAdd: unsupported time value")
	}
	durStr, ok := params[1].(string)
	if !ok {
		return nil, errors.New("dtAdd: duration must be a string")
	}
	dur, err := time.ParseDuration(durStr)
	if err != nil {
		return nil, fmt.Errorf("dtAdd: unable to parse duration: %v", err)
	}
	return t.Add(dur), nil
}

// dtAge calculates the age (in years as a fraction) from a provided date until now.
// The parameter can be a time.Time or a string in RFC3339 format.
// The calculation divides the total duration (in hours) by the number of hours in an average year.
func dtAge(params ...any) (any, error) {
	if len(params) < 1 {
		return nil, errors.New("dtAge: requires one parameter: the birth date as time.Time or RFC3339 string")
	}

	var birth time.Time
	switch v := params[0].(type) {
	case time.Time:
		birth = v
	case string:
		// Expecting RFC3339 format.
		var err error
		birth, err = time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, errors.New("dtAge: unable to parse time string, expected RFC3339 format")
		}
	default:
		return nil, errors.New("dtAge: unsupported time value")
	}

	now := time.Now()
	// Calculate the duration between now and the birth date.
	duration := now.Sub(birth)
	// There are 24 hours in a day and on average 365.2425 days in a year.
	hoursInYear := 24.0 * 365.2425
	ageYears := duration.Hours() / hoursInYear
	return ageYears, nil
}

// mod returns the integer remainder of dividing the first parameter by the second.
func mod(params ...any) (any, error) {
	if len(params) != 2 {
		return nil, errors.New("mod: requires exactly two integer parameters")
	}
	a, ok := toInt(params[0])
	if !ok {
		return nil, errors.New("mod: parameters must be convertible to integers")
	}
	b, ok := toInt(params[1])
	if !ok {
		return nil, errors.New("mod: parameters must be convertible to integers")
	}
	if b == 0 {
		return nil, errors.New("mod: division by zero")
	}
	return a % b, nil
}

// concat concatenates all the parameters as strings.
func concat(params ...any) (any, error) {
	var sb strings.Builder
	for _, p := range params {
		sb.WriteString(fmt.Sprint(p))
	}
	return sb.String(), nil
}

func init() {
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
	RegisterFunction("split", split)
	RegisterFunction("join", join)
	RegisterFunction("length", length)
	RegisterFunction("index", index)
	RegisterFunction("startsWith", startsWith)
	RegisterFunction("endsWith", endsWith)
	RegisterFunction("repeat", repeat)
	RegisterFunction("add", add)
	RegisterFunction("subtract", subtract)
	RegisterFunction("multiply", multiply)
	RegisterFunction("divide", divide)
	RegisterFunction("mod", mod)
	RegisterFunction("concat", concat)
	RegisterFunction("now", dtNow)
	RegisterFunction("date_format", dtFormat)
	RegisterFunction("date_parse", dtParse)
	RegisterFunction("date_add", dtAdd)
	RegisterFunction("date_age", dtAge)
}
