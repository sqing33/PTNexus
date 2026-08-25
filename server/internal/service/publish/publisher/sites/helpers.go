package sites

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

func toStringAny(value any, fallback string) string {
	switch typed := value.(type) {
	case nil:
		return fallback
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed != "" {
			return trimmed
		}
	case []byte:
		trimmed := strings.TrimSpace(string(typed))
		if trimmed != "" {
			return trimmed
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		return fmt.Sprintf("%v", typed)
	default:
		return fallback
	}
	return fallback
}

func boolFromAnyWithDefault(value any, fallback bool) bool {
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
		text := strings.ToLower(strings.TrimSpace(typed))
		if text == "" {
			return fallback
		}
		if text == "true" || text == "1" || text == "yes" || text == "y" {
			return true
		}
		if text == "false" || text == "0" || text == "no" || text == "n" {
			return false
		}
		return fallback
	default:
		return fallback
	}
}

func boolFromAny(value any) bool {
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

func normalizeBaseURL(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(trimmed), "http://") && !strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		trimmed = "https://" + trimmed
	}
	return strings.TrimRight(trimmed, "/")
}

func summarizeResponseBody(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Fields(trimmed), " ")
}

func parseStringArray(value any) []string {
	switch typed := value.(type) {
	case nil:
		return []string{}
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			trimmed := strings.TrimSpace(toStringAny(item, ""))
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case string:
		raw := strings.TrimSpace(typed)
		if raw == "" {
			return []string{}
		}
		if strings.Contains(raw, ",") {
			items := strings.Split(raw, ",")
			out := make([]string, 0, len(items))
			for _, item := range items {
				trimmed := strings.TrimSpace(item)
				if trimmed != "" {
					out = append(out, trimmed)
				}
			}
			return out
		}
		return []string{raw}
	default:
		return []string{}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func removeFormFieldsByPrefix(formFields map[string]string, prefix string) {
	if formFields == nil {
		return
	}
	trimmedPrefix := strings.TrimSpace(prefix)
	if trimmedPrefix == "" {
		return
	}
	for key := range formFields {
		if strings.HasPrefix(strings.TrimSpace(key), trimmedPrefix) {
			delete(formFields, key)
		}
	}
}

func rebuildIndexedFormFields(formFields map[string]string, base string, values []string) {
	if formFields == nil {
		return
	}
	trimmedBase := strings.TrimSpace(base)
	if trimmedBase == "" {
		return
	}
	removeFormFieldsByPrefix(formFields, trimmedBase+"[")
	for idx, value := range values {
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue == "" {
			continue
		}
		formFields[fmt.Sprintf("%s[%d]", trimmedBase, idx)] = trimmedValue
	}
}

func resolveSiteCombinedTags(uploadData map[string]any) map[string]struct{} {
	result := map[string]struct{}{}
	if uploadData == nil {
		return result
	}

	appendValue := func(value any) {
		for _, tag := range parseFlexibleStringArray(value) {
			trimmed := strings.TrimSpace(tag)
			if trimmed == "" {
				continue
			}
			result[trimmed] = struct{}{}
		}
	}

	if standardized, ok := uploadData["standardized_params"].(map[string]any); ok && standardized != nil {
		appendValue(standardized["tags"])
	}
	appendValue(uploadData["tags"])
	if sourceParams, ok := uploadData["source_params"].(map[string]any); ok && sourceParams != nil {
		appendValue(sourceParams["标签"])
	}

	return result
}

// hasAnimationTag 判断发布数据是否包含动漫标签，动漫属性由标签表达而不是类型字段表达。
// 参数/返回：uploadData 为标准化发布数据；返回是否命中动漫/动画标签。
// 失败场景：发布数据为空或标签格式无法解析时返回 false。
// 副作用：无。
func hasAnimationTag(uploadData map[string]any) bool {
	for tag := range resolveSiteCombinedTags(uploadData) {
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "tag.动漫", "tag.动画", "动漫", "动画", "anime", "animation":
			return true
		}
	}
	return false
}

func parseFlexibleStringArray(value any) []string {
	switch typed := value.(type) {
	case nil:
		return []string{}
	case []string:
		return parseStringArray(typed)
	case []any:
		return parseStringArray(typed)
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return []string{}
		}
		if strings.HasPrefix(text, "[") {
			parsedStrings := []string{}
			if err := json.Unmarshal([]byte(text), &parsedStrings); err == nil {
				return parseStringArray(parsedStrings)
			}
			parsedAny := []any{}
			if err := json.Unmarshal([]byte(text), &parsedAny); err == nil {
				return parseStringArray(parsedAny)
			}
		}
		if strings.Contains(text, ",") {
			parts := strings.Split(text, ",")
			out := make([]string, 0, len(parts))
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					out = append(out, trimmed)
				}
			}
			return out
		}
		return []string{text}
	default:
		return parseStringArray(value)
	}
}

func hasAnySiteTagLower(tags map[string]struct{}, candidates ...string) bool {
	if len(tags) == 0 {
		return false
	}
	for tag := range tags {
		lower := strings.ToLower(strings.TrimSpace(tag))
		for _, candidate := range candidates {
			if lower == strings.ToLower(strings.TrimSpace(candidate)) {
				return true
			}
		}
	}
	return false
}

func sortedUniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

var reImgTagSource = regexp.MustCompile(`(?is)\[img[^\]]*\](.*?)\[/img\]`)
var reHTMLImgSource = regexp.MustCompile(`(?is)<img[^>]+src=["']([^"']+)["']`)
var reMarkdownImgSource = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)
var reRawImageURL = regexp.MustCompile(`https?://[^\s\[\]<>"]+\.(?:png|jpe?g|gif|webp)(?:\?[^\s\[\]<>"]*)?`)

func extractImageURLsFromText(text string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return []string{}
	}
	out := make([]string, 0, 8)
	appendMatches := func(re *regexp.Regexp) {
		for _, sub := range re.FindAllStringSubmatch(trimmed, -1) {
			if len(sub) < 2 {
				continue
			}
			if value := strings.TrimSpace(sub[1]); value != "" {
				out = append(out, value)
			}
		}
	}
	appendMatches(reImgTagSource)
	appendMatches(reHTMLImgSource)
	appendMatches(reMarkdownImgSource)
	for _, item := range reRawImageURL.FindAllString(trimmed, -1) {
		if value := strings.TrimSpace(item); value != "" {
			out = append(out, value)
		}
	}
	return sortedUniqueStrings(out)
}

func resolveUploadSection(uploadData map[string]any, key string) string {
	if uploadData == nil {
		return ""
	}
	if fromTop := strings.TrimSpace(toStringAny(uploadData[key], "")); fromTop != "" {
		return fromTop
	}
	intro, _ := uploadData["intro"].(map[string]any)
	if intro == nil {
		return ""
	}
	return strings.TrimSpace(toStringAny(intro[key], ""))
}

func parseTitleComponentsLocal(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			component, ok := item.(map[string]any)
			if !ok || component == nil {
				continue
			}
			result = append(result, component)
		}
		return result
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		parsed := []map[string]any{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return parsed
		}
		return nil
	default:
		return nil
	}
}

func findTitleComponentValue(components []map[string]any, key string) string {
	for _, component := range components {
		if strings.TrimSpace(toStringAny(component["key"], "")) != strings.TrimSpace(key) {
			continue
		}
		return strings.TrimSpace(toStringAny(component["value"], ""))
	}
	return ""
}

var reFourDigitYear = regexp.MustCompile(`\b(19|20)\d{2}\b`)

func extractFourDigitYear(text string) string {
	match := reFourDigitYear.FindString(strings.TrimSpace(text))
	return strings.TrimSpace(match)
}
