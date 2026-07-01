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

func isValidStandardValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "" || (strings.Contains(trimmed, ".") && !strings.HasPrefix(trimmed, ".") && !strings.HasSuffix(trimmed, "."))
}

func hasRestrictedStandardTag(tags []string) bool {
	for _, raw := range tags {
		switch strings.TrimSpace(raw) {
		case "禁转", "tag.禁转", "限转", "tag.限转", "分集", "tag.分集":
			return true
		}
	}
	return false
}

func hasUnrecognizedValue(titleComponents []any) bool {
	return strings.TrimSpace(extractUnrecognized(titleComponents)) != ""
}

func isRowPublishable(item map[string]any, reverseMappings map[string]any) bool {
	if boolFromAny(item["is_deleted"]) || !boolFromAny(item["is_reviewed"]) {
		return false
	}

	fieldCategories := []string{"type", "medium", "video_codec", "audio_codec", "resolution", "team", "source"}
	for _, category := range fieldCategories {
		value := strings.TrimSpace(toString(item[category], ""))
		if value == "" {
			continue
		}
		if !isValidStandardValue(value) || !isStandardValueMapped(reverseMappings, category, value) {
			return false
		}
	}

	tags := parseStringArray(item["tags"])
	if hasRestrictedStandardTag(tags) {
		return false
	}
	for _, tag := range tags {
		if !isValidStandardValue(tag) || !isStandardValueMapped(reverseMappings, "tags", tag) {
			return false
		}
	}

	return !hasUnrecognizedValue(parseAnyArray(item["title_components"]))
}

func isStandardValueMapped(reverseMappings map[string]any, category string, standardValue string) bool {
	categoryMap := toStringMapAny(reverseMappings[category])
	if len(categoryMap) == 0 {
		return false
	}
	_, ok := categoryMap[standardValue]
	return ok
}

func toStringMapAny(value any) map[string]string {
	result := map[string]string{}
	switch typed := value.(type) {
	case map[string]string:
		return typed
	case map[string]any:
		for key, item := range typed {
			result[strings.TrimSpace(key)] = strings.TrimSpace(toString(item, ""))
		}
	case map[any]any:
		for key, item := range typed {
			result[strings.TrimSpace(toString(key, ""))] = strings.TrimSpace(toString(item, ""))
		}
	}
	return result
}

func normalizeQueryRow(item map[string]any, reverseMappings map[string]any) {
	item["tags"] = parseStringArray(item["tags"])
	item["is_deleted"] = boolFromAny(item["is_deleted"])
	item["is_reviewed"] = boolFromAny(item["is_reviewed"])

	titleComponents := parseAnyArray(item["title_components"])
	item["unrecognized"] = extractUnrecognized(titleComponents)
	item["is_publishable"] = isRowPublishable(item, reverseMappings)
}

func paginateMaps(items []map[string]any, offset int, pageSize int) []map[string]any {
	if offset >= len(items) {
		return []map[string]any{}
	}
	end := offset + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func matchesReviewStatus(item map[string]any, reviewStatus string) bool {
	publishable, ok := item["is_publishable"].(bool)
	if !ok {
		publishable = false
	}

	switch reviewStatus {
	case "reviewed":
		return publishable
	case "unreviewed":
		return !boolFromAny(item["is_deleted"]) && !publishable
	case "error":
		return boolFromAny(item["is_deleted"])
	default:
		return true
	}
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
