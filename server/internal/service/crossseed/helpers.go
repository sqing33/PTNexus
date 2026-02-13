package crossseed

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseISOTime(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	return time.Parse("2006-01-02 15:04:05", value)
}

func parseStringArray(value any) []string {
	if value == nil {
		return []string{}
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			entry := strings.TrimSpace(toString(item, ""))
			if entry != "" {
				result = append(result, entry)
			}
		}
		return result
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return []string{}
		}
		if strings.HasPrefix(trimmed, "[") {
			parsed := []string{}
			if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
				return parsed
			}
			parsedAny := []any{}
			if err := json.Unmarshal([]byte(trimmed), &parsedAny); err == nil {
				result := make([]string, 0, len(parsedAny))
				for _, item := range parsedAny {
					entry := strings.TrimSpace(toString(item, ""))
					if entry != "" {
						result = append(result, entry)
					}
				}
				return result
			}
		}
		parts := strings.Split(trimmed, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			entry := strings.TrimSpace(part)
			if entry != "" {
				result = append(result, entry)
			}
		}
		return result
	default:
		return []string{}
	}
}

func parseAnyArray(value any) []any {
	if value == nil {
		return []any{}
	}
	if typed, ok := value.([]any); ok {
		return typed
	}
	if typed, ok := value.(string); ok {
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return []any{}
		}
		parsed := []any{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return parsed
		}
	}
	return []any{}
}

func extractUnrecognized(titleComponents []any) string {
	for _, raw := range titleComponents {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if toString(item["key"], "") == "无法识别" {
			return toString(item["value"], "")
		}
	}
	return ""
}

func boolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case *interface{}:
		if typed == nil {
			return false
		}
		return boolFromAny(*typed)
	case sql.NullBool:
		if !typed.Valid {
			return false
		}
		return typed.Bool
	case sql.NullInt64:
		if !typed.Valid {
			return false
		}
		return typed.Int64 != 0
	case []byte:
		// MySQL driver may return TINYINT(1)/BIT(1) as raw bytes like []byte{0x00} / []byte{0x01}
		// (not ASCII "0"/"1"), so handle that explicitly.
		if len(typed) == 1 {
			return typed[0] != 0
		}
		lower := strings.ToLower(strings.TrimSpace(string(typed)))
		if lower == "1" || lower == "true" || lower == "yes" {
			return true
		}
		if lower == "0" || lower == "false" || lower == "no" || lower == "" {
			return false
		}
		if parsed, err := strconv.ParseInt(lower, 10, 64); err == nil {
			return parsed != 0
		}
		return false
	case sql.RawBytes:
		raw := []byte(typed)
		if len(raw) == 1 {
			return raw[0] != 0
		}
		lower := strings.ToLower(strings.TrimSpace(string(raw)))
		if lower == "1" || lower == "true" || lower == "yes" {
			return true
		}
		if lower == "0" || lower == "false" || lower == "no" || lower == "" {
			return false
		}
		if parsed, err := strconv.ParseInt(lower, 10, 64); err == nil {
			return parsed != 0
		}
		return false
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
	case float64:
		return typed != 0
	case string:
		lower := strings.ToLower(strings.TrimSpace(typed))
		if lower == "1" || lower == "true" || lower == "yes" {
			return true
		}
		if lower == "0" || lower == "false" || lower == "no" || lower == "" {
			return false
		}
		if parsed, err := strconv.ParseInt(lower, 10, 64); err == nil {
			return parsed != 0
		}
		return false
	default:
		return false
	}
}

func toSet(values []string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		result[trimmed] = struct{}{}
	}
	return result
}

func toString(value any, fallback string) string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback
		}
		return typed
	case nil:
		return fallback
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text == "" {
			return fallback
		}
		return text
	}
}
