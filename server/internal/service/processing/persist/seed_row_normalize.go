package persist

import (
	"encoding/json"
	"strings"

	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
	processingtitle "github.com/pt-nexus/server/internal/service/processing/title"
)

// NormalizeSeedRow 对 seed_parameters 行数据做统一归一化，便于后续发布映射与前端回显。
// 参数/返回：row 为数据库行；返回字段齐全且类型稳定的副本。
// 失败场景：输入为空时返回空 map；JSON 解析失败时返回空集合兜底。
// 副作用：无副作用，不访问外部资源。
func NormalizeSeedRow(row map[string]any) map[string]any {
	item := map[string]any{}
	for key, value := range row {
		item[key] = value
	}

	title := strings.TrimSpace(toStringWithFallback(item["title"], ""))
	name := strings.TrimSpace(toStringWithFallback(item["name"], ""))
	if title == "" {
		title = name
	}
	if name == "" {
		name = title
	}
	item["title"] = title
	item["name"] = name

	item["subtitle"] = toStringWithFallback(item["subtitle"], "")
	item["imdb_link"] = toStringWithFallback(item["imdb_link"], "")
	item["douban_link"] = toStringWithFallback(item["douban_link"], "")
	item["tmdb_link"] = toStringWithFallback(item["tmdb_link"], "")
	item["statement"] = toStringWithFallback(item["statement"], "")
	item["poster"] = toStringWithFallback(item["poster"], "")
	item["body"] = toStringWithFallback(item["body"], "")
	item["screenshots"] = toStringWithFallback(item["screenshots"], "")
	item["mediainfo"] = toStringWithFallback(item["mediainfo"], "")
	item["team"] = parser.NormalizeTeamKey(toStringWithFallback(item["team"], ""))

	item["source_params"] = ParseStringMap(item["source_params"])
	item["final_publish_parameters"] = ParseStringMap(item["final_publish_parameters"])
	item["complete_publish_params"] = ParseStringMap(item["complete_publish_params"])
	item["raw_params_for_preview"] = ParseStringMap(item["raw_params_for_preview"])
	item["tags"] = ParseStringArray(item["tags"])

	titleComponents := processingtitle.CompleteTitleComponents(ParseAnyArray(item["title_components"]), title)
	item["title_components"] = titleComponents
	item["removed_ardtudeclarations"] = ParseAnyArray(item["removed_ardtudeclarations"])
	item["unrecognized"] = extractUnrecognized(titleComponents)
	item["is_reviewed"] = BoolFromAny(item["is_reviewed"])
	item["is_deleted"] = false
	return item
}

// ParseStringArray 将任意类型值转换为字符串切片（支持 []string / []any / JSON 字符串 / 逗号分隔文本）。
func ParseStringArray(value any) []string {
	if value == nil {
		return []string{}
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			entry := strings.TrimSpace(toStringWithFallback(item, ""))
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
					entry := strings.TrimSpace(toStringWithFallback(item, ""))
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

// ParseAnyArray 将任意值转换为 []any（支持 []any 与 JSON 字符串）。
func ParseAnyArray(value any) []any {
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

// ParseStringMap 将任意值转换为 map[string]any（支持 map 与 JSON 对象字符串）。
func ParseStringMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return map[string]any{}
		}
		if strings.HasPrefix(trimmed, "{") {
			parsed := map[string]any{}
			if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
				return parsed
			}
		}
		return map[string]any{}
	default:
		return map[string]any{}
	}
}

// BoolFromAny 兼容解析 bool/int/float/string 为布尔值。
func BoolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case string:
		lower := strings.ToLower(strings.TrimSpace(typed))
		return lower == "1" || lower == "true" || lower == "yes"
	default:
		return false
	}
}

func extractUnrecognized(titleComponents []any) string {
	for _, raw := range titleComponents {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if toStringWithFallback(item["key"], "") == "无法识别" {
			return toStringWithFallback(item["value"], "")
		}
	}
	return ""
}

func toStringWithFallback(value any, fallback string) string {
	switch typed := value.(type) {
	case nil:
		return fallback
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback
		}
		return typed
	case []byte:
		text := strings.TrimSpace(string(typed))
		if text == "" {
			return fallback
		}
		return text
	default:
		return fallback
	}
}
