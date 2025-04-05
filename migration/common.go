package migration

import (
	"fmt"
	"strings"
)

func ConvertDefault(defVal any, colType string) string {
	var def string
	switch v := defVal.(type) {
	case string:
		def = v
	case nil:
		def = "NULL"
	default:
		def = fmt.Sprintf("%v", v)
	}
	lowerDef := strings.ToLower(def)
	if lowerDef == "now()" {
		return "CURRENT_TIMESTAMP"
	}
	if lowerDef == "null" {
		return "NULL"
	}
	if strings.ToLower(colType) == "string" {
		if !(strings.HasPrefix(def, "'") && strings.HasSuffix(def, "'")) {
			return fmt.Sprintf("'%s'", def)
		}
	}
	return def
}
