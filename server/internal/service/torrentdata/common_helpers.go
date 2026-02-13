package torrentdata

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func formatBytes(size int64) string {
	if size < 0 {
		size = 0
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	value := float64(size)
	idx := 0
	for value >= 1024 && idx < len(units)-1 {
		value /= 1024
		idx++
	}
	if idx == 0 {
		return fmt.Sprintf("%d %s", int64(value), units[idx])
	}
	return fmt.Sprintf("%.2f %s", value, units[idx])
}

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
