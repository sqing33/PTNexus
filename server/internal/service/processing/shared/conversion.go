package shared

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ToString 将任意值转换为字符串，空值时返回 fallback。
func ToString(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback
		}
		return typed
	case fmt.Stringer:
		text := strings.TrimSpace(typed.String())
		if text == "" {
			return fallback
		}
		return text
	case []byte:
		text := strings.TrimSpace(string(typed))
		if text == "" {
			return fallback
		}
		return text
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%v", typed)
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text == "" || text == "<nil>" {
			return fallback
		}
		return text
	}
}

// ToFloat 将任意值转换为 float64，转换失败返回 0。
func ToFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int8:
		return float64(typed)
	case int16:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	case uint:
		return float64(typed)
	case uint8:
		return float64(typed)
	case uint16:
		return float64(typed)
	case uint32:
		return float64(typed)
	case uint64:
		return float64(typed)
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
		return 0
	case []byte:
		trimmed := strings.TrimSpace(string(typed))
		if trimmed == "" {
			return 0
		}
		if parsed, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return parsed
		}
		return 0
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0
		}
		if parsed, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return parsed
		}
		return 0
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text != "" && text != "<nil>" {
			if parsed, err := strconv.ParseFloat(text, 64); err == nil {
				return parsed
			}
		}
		return 0
	}
}

// ToStringSlice 将任意值转换为 []string，无法转换时返回空切片。
func ToStringSlice(value any) []string {
	if value == nil {
		return []string{}
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			entry := strings.TrimSpace(ToString(item, ""))
			if entry != "" {
				result = append(result, entry)
			}
		}
		return result
	default:
		return []string{}
	}
}

// ToBool 将任意值转换为 bool，转换失败返回 false。
func ToBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case int:
		return typed != 0
	case int8:
		return typed != 0
	case int16:
		return typed != 0
	case int32:
		return typed != 0
	case int64:
		return typed != 0
	case uint:
		return typed != 0
	case uint8:
		return typed != 0
	case uint16:
		return typed != 0
	case uint32:
		return typed != 0
	case uint64:
		return typed != 0
	case float32:
		return typed != 0
	case float64:
		return typed != 0
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed != 0
		}
		return false
	case []byte:
		lower := strings.ToLower(strings.TrimSpace(string(typed)))
		return lower == "1" || lower == "true" || lower == "yes" || lower == "ok"
	case string:
		lower := strings.ToLower(strings.TrimSpace(typed))
		return lower == "1" || lower == "true" || lower == "yes" || lower == "ok"
	default:
		text := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", value)))
		return text == "1" || text == "true" || text == "yes" || text == "ok"
	}
}
