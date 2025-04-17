package bcl

import (
	"errors"
	"fmt"
	"strings"
)

func init() {
	RegisterFunction("isDefined", builtinIsDefined)
	RegisterFunction("isNull", builtinIsNull)
	RegisterFunction("isEmpty", builtinIsEmpty)
	RegisterFunction("upper", upper)
}

func upper(params ...any) (any, error) {
	if len(params) == 0 {
		return nil, errors.New("At least one param required")
	}
	str, ok := params[0].(string)
	if !ok {
		str = fmt.Sprint(params[0])
	}
	return strings.ToUpper(str), nil
}

func builtinIsDefined(params ...any) (any, error) {
	if len(params) == 0 {
		return nil, errors.New("At least one param required")
	}
	_, isUndef := params[0].(Undefined)
	return !isUndef, nil
}

func builtinIsNull(params ...any) (any, error) {
	if len(params) == 0 {
		return nil, errors.New("At least one param required")
	}
	if params[0] == nil {
		return true, nil
	}
	_, isUndef := params[0].(Undefined)
	return isUndef, nil
}

func builtinIsEmpty(params ...any) (any, error) {
	if len(params) == 0 {
		return nil, errors.New("At least one param required")
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
