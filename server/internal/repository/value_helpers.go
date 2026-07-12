package repository

import (
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
