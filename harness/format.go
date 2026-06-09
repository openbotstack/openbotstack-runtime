package harness

import (
	"encoding/json"
	"fmt"
)

// FormatOutput converts a step output to a human-readable string.
// Structured data (maps, slices) is JSON-serialized for readability.
func FormatOutput(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case map[string]any, []any:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	default:
		return fmt.Sprintf("%v", val)
	}
}
