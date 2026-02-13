package settings

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
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

func mergeWithDefault(defaults, overrides map[string]any) map[string]any {
	result := deepCopy(defaults)
	for key, value := range overrides {
		if overrideMap, ok := value.(map[string]any); ok {
			if baseMap, ok := result[key].(map[string]any); ok {
				result[key] = mergeWithDefault(baseMap, overrideMap)
				continue
			}
		}
		result[key] = value
	}
	return result
}

func deepCopy(input map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range input {
		switch typed := value.(type) {
		case map[string]any:
			result[key] = deepCopy(typed)
		case []any:
			cloned := make([]any, len(typed))
			copy(cloned, typed)
			result[key] = cloned
		default:
			result[key] = typed
		}
	}
	return result
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

func toIntWithDefault(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return fallback
		}
		return parsed
	default:
		return fallback
	}
}

func nestedMap(source map[string]any, keys ...string) map[string]any {
	current := source
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			return map[string]any{}
		}
		current = next
	}
	return current
}

func ensureMap(source map[string]any, key string) map[string]any {
	if existing, ok := source[key].(map[string]any); ok {
		return existing
	}
	created := map[string]any{}
	source[key] = created
	return created
}

func nowString() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func sortStringSlice(values []string) []string {
	copied := make([]string, len(values))
	copy(copied, values)
	sort.Strings(copied)
	return copied
}
