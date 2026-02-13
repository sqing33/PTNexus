package service

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func toSlice(value any) []any {
	if typed, ok := value.([]any); ok {
		return typed
	}
	return []any{}
}

func toString(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case float64:
		if math.Trunc(typed) == typed {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fallback
	}
}

func toBool(value any, fallback bool) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}
	if str, ok := value.(string); ok {
		lower := strings.ToLower(strings.TrimSpace(str))
		if lower == "true" || lower == "1" {
			return true
		}
		if lower == "false" || lower == "0" {
			return false
		}
	}
	return fallback
}
