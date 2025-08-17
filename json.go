package bcl

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// MarshalJSON converts BCL data to JSON format
func MarshalJSON(v interface{}) ([]byte, error) {
	// Clean up BCL-specific metadata before JSON marshaling
	cleaned := cleanForJSON(v)
	return json.Marshal(cleaned)
}

// MarshalJSONIndent converts BCL data to indented JSON format
func MarshalJSONIndent(v interface{}, prefix, indent string) ([]byte, error) {
	cleaned := cleanForJSON(v)
	return json.MarshalIndent(cleaned, prefix, indent)
}

// WriteJSON writes BCL data as JSON to a writer
func WriteJSON(w io.Writer, v interface{}) error {
	cleaned := cleanForJSON(v)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(cleaned)
}

// cleanForJSON removes BCL-specific metadata from data structures
func cleanForJSON(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		cleaned := make(map[string]interface{})
		for k, v := range val {
			// Skip BCL metadata fields
			if strings.HasPrefix(k, "__") {
				continue
			}
			// Special handling for props field
			if k == "props" {
				if props, ok := v.(map[string]interface{}); ok {
					// Flatten props into the parent object
					for pk, pv := range props {
						if !strings.HasPrefix(pk, "__") {
							cleaned[pk] = cleanForJSON(pv)
						}
					}
					continue
				}
			}
			cleaned[k] = cleanForJSON(v)
		}
		return cleaned

	case []interface{}:
		cleaned := make([]interface{}, 0, len(val))
		for _, item := range val {
			cleaned = append(cleaned, cleanForJSON(item))
		}
		return cleaned

	case Undefined:
		return nil

	default:
		return val
	}
}

// UnmarshalJSON parses JSON data into BCL structures
func UnmarshalJSON(data []byte, v interface{}) error {
	// First unmarshal to intermediate structure
	var intermediate interface{}
	if err := json.Unmarshal(data, &intermediate); err != nil {
		return err
	}

	// Convert to BCL format
	bclData := jsonToBCL(intermediate)

	// Use reflection to assign to target
	return convertMap(bclData.(map[string]interface{}), v)
}

// jsonToBCL converts JSON structures to BCL format
func jsonToBCL(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, v := range val {
			result[k] = jsonToBCL(v)
		}
		return result

	case []interface{}:
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = jsonToBCL(item)
		}
		return result

	case float64:
		// JSON numbers are always float64, convert to int if possible
		if val == float64(int64(val)) {
			return int(val)
		}
		return val

	default:
		return val
	}
}

// JSONCompatible checks if a BCL structure can be safely converted to JSON
func JSONCompatible(v interface{}) error {
	// For now, we'll do a simple type check without circular reference detection
	// as maps and slices cannot be used as map keys in Go
	return checkJSONCompatible(v)
}

func checkJSONCompatible(v interface{}) error {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, v := range val {
			if err := checkJSONCompatible(v); err != nil {
				return fmt.Errorf("key %s: %w", k, err)
			}
		}

	case []interface{}:
		for i, item := range val {
			if err := checkJSONCompatible(item); err != nil {
				return fmt.Errorf("index %d: %w", i, err)
			}
		}

	case string, bool, nil:
		// These types are always JSON compatible

	case int, int8, int16, int32, int64:
		// Integer types are JSON compatible

	case uint, uint8, uint16, uint32, uint64:
		// Unsigned integer types are JSON compatible

	case float32, float64:
		// Float types are JSON compatible

	case Undefined:
		// Will be converted to null

	default:
		return fmt.Errorf("type %T is not JSON compatible", v)
	}

	return nil
}
