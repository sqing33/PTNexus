package torrentdata

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pt-nexus/server-go/internal/repository"
)

func collectUniqueStrings(records []repository.TorrentRecord, selector func(repository.TorrentRecord) string) []string {
	set := map[string]struct{}{}
	for _, row := range records {
		value := strings.TrimSpace(selector(row))
		if value == "" {
			continue
		}
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func filterData(items []map[string]any, predicate func(map[string]any) bool) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if predicate(item) {
			result = append(result, item)
		}
	}
	return result
}

func toStringSet(values []string) map[string]struct{} {
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

func stringValue(value any, fallback string) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%f", typed)
	default:
		return fallback
	}
}

func intValue(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return fallback
		}
		var parsed int
		if _, err := fmt.Sscanf(trimmed, "%d", &parsed); err == nil {
			return parsed
		}
		return fallback
	default:
		return fallback
	}
}

func int64Value(value any, fallback int64) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return fallback
		}
		var parsed int64
		if _, err := fmt.Sscanf(trimmed, "%d", &parsed); err == nil {
			return parsed
		}
		return fallback
	default:
		return fallback
	}
}

func numberValue(value any) float64 {
	switch typed := value.(type) {
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float64:
		return typed
	case float32:
		return float64(typed)
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0
		}
		var parsed float64
		if _, err := fmt.Sscanf(trimmed, "%f", &parsed); err == nil {
			return parsed
		}
		return 0
	default:
		return 0
	}
}

func toStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			parsed := stringValue(item, "")
			if parsed != "" {
				result = append(result, parsed)
			}
		}
		return result
	default:
		return []string{}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// customNameLess matches Python utils.formatters.custom_sort_compare():
// letters (a-z) sort before digits (0-9), and both sort before other symbols.
func customNameLess(left, right string) bool {
	na := []rune(strings.ToLower(left))
	nb := []rune(strings.ToLower(right))
	minLen := len(na)
	if len(nb) < minLen {
		minLen = len(nb)
	}
	for i := 0; i < minLen; i++ {
		ta := charType(na[i])
		tb := charType(nb[i])
		if ta != tb {
			return ta < tb
		}
		if na[i] != nb[i] {
			return na[i] < nb[i]
		}
	}
	return len(na) < len(nb)
}

func charType(c rune) int {
	if c >= 'a' && c <= 'z' {
		return 1
	}
	if c >= '0' && c <= '9' {
		return 2
	}
	return 3
}
