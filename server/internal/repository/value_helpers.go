package repository

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func toString(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func toInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case int32:
		return int64(typed), nil
	case int16:
		return int64(typed), nil
	case int8:
		return int64(typed), nil
	case uint:
		return int64(typed), nil
	case uint64:
		return int64(typed), nil
	case uint32:
		return int64(typed), nil
	case uint16:
		return int64(typed), nil
	case uint8:
		return int64(typed), nil
	case float64:
		return int64(typed), nil
	case float32:
		return int64(typed), nil
	case bool:
		if typed {
			return 1, nil
		}
		return 0, nil
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0, fmt.Errorf("empty string")
		}
		return strconv.ParseInt(text, 10, 64)
	case []byte:
		text := strings.TrimSpace(string(typed))
		if text == "" {
			return 0, fmt.Errorf("empty bytes")
		}
		return strconv.ParseInt(text, 10, 64)
	default:
		return 0, fmt.Errorf("unsupported type %T", value)
	}
}

func toIntWithDefault(value any, fallback int) int {
	parsed, err := toInt64(value)
	if err != nil {
		return fallback
	}
	return int(parsed)
}

func toFloat64WithDefault(value any, fallback float64) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		if typed == "" {
			return fallback
		}
		parsed, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return fallback
		}
		return parsed
	default:
		return fallback
	}
}

func siteStringListFromAny(value any) []string {
	result := make([]string, 0)
	appendValue := func(item any) {
		text := strings.TrimSpace(toString(item, ""))
		if text != "" {
			result = append(result, text)
		}
	}
	switch typed := value.(type) {
	case nil:
		return []string{}
	case []string:
		for _, item := range typed {
			appendValue(item)
		}
	case []any:
		for _, item := range typed {
			appendValue(item)
		}
	case []byte:
		return siteStringListFromAny(string(typed))
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return []string{}
		}
		parsedString := []string{}
		if strings.HasPrefix(trimmed, "[") && json.Unmarshal([]byte(trimmed), &parsedString) == nil {
			for _, item := range parsedString {
				appendValue(item)
			}
			return dedupeSiteStringList(result)
		}
		parsedAny := []any{}
		if strings.HasPrefix(trimmed, "[") && json.Unmarshal([]byte(trimmed), &parsedAny) == nil {
			for _, item := range parsedAny {
				appendValue(item)
			}
			return dedupeSiteStringList(result)
		}
		for _, item := range strings.Split(trimmed, ",") {
			appendValue(item)
		}
	default:
		appendValue(typed)
	}
	return dedupeSiteStringList(result)
}

func encodeSiteStringList(value any) string {
	items := siteStringListFromAny(value)
	if len(items) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func dedupeSiteStringList(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
