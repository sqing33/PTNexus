package stats

import (
	"fmt"
	"strings"
)

func (s *Service) enabledDownloaders() []map[string]any {
	cfg := s.cfg.Get()
	downloaders := toSlice(cfg["downloaders"])
	result := make([]map[string]any, 0, len(downloaders))
	for _, raw := range downloaders {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if !toBool(item["enabled"], true) {
			continue
		}
		result = append(result, map[string]any{
			"id":   toString(item["id"], ""),
			"name": toString(item["name"], ""),
		})
	}
	return result
}

func toSlice(value any) []any {
	if value == nil {
		return []any{}
	}
	if typed, ok := value.([]any); ok {
		return typed
	}
	return []any{}
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

func toBool(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		lower := strings.ToLower(strings.TrimSpace(typed))
		if lower == "true" || lower == "1" || lower == "yes" {
			return true
		}
		if lower == "false" || lower == "0" || lower == "no" {
			return false
		}
		return fallback
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return fallback
	}
}
