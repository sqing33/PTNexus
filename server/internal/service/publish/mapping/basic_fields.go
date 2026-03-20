package mapping

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ResolveBasicPublishMappings 根据标准化参数生成目标站的基础表单字段映射。
func ResolveBasicPublishMappings(siteCode string, uploadData map[string]any) map[string]string {
	mapped := map[string]string{}
	standardized, _ := uploadData["standardized_params"].(map[string]any)
	if standardized == nil {
		standardized = map[string]any{}
	}

	siteCfg, _ := LoadSitePublishConfig(siteCode)
	apply := func(mappingKey string, formKey string, value string, fallbackField string, requireConfiguredField bool) {
		if strings.TrimSpace(value) == "" {
			return
		}
		fieldName := fallbackField
		mappedValue := value
		if siteCfg != nil {
			if resolvedField := strings.TrimSpace(siteCfg.FormFields[formKey]); resolvedField != "" {
				fieldName = resolvedField
			} else if requireConfiguredField {
				return
			}
			mappedValue = strings.TrimSpace(PickMappedValueWithFallback(mappingKey, siteCfg.Mappings[mappingKey], value))
		} else if requireConfiguredField {
			return
		}
		if fieldName == "" || mappedValue == "" {
			return
		}
		mapped[fieldName] = mappedValue
	}

	apply("type", "category", strings.TrimSpace(toStringAnyBasic(standardized["type"], "")), "type", false)
	apply("medium", "medium", strings.TrimSpace(toStringAnyBasic(standardized["medium"], "")), "medium", false)
	apply("video_codec", "video_codec", strings.TrimSpace(toStringAnyBasic(standardized["video_codec"], "")), "codec", false)
	apply("audio_codec", "audio_codec", strings.TrimSpace(toStringAnyBasic(standardized["audio_codec"], "")), "audiocodec", false)
	apply("resolution", "resolution", strings.TrimSpace(toStringAnyBasic(standardized["resolution"], "")), "standard", false)
	applySourceOrProcessing(siteCfg, mapped, strings.TrimSpace(toStringAnyBasic(standardized["source"], "")))
	apply("team", "team", strings.TrimSpace(toStringAnyBasic(standardized["team"], "")), "team", false)

	applyTags("tag", siteCfg, mapped, uploadData, standardized)
	return mapped
}

func applySourceOrProcessing(siteCfg *SitePublishConfig, mapped map[string]string, sourceValue string) {
	trimmed := strings.TrimSpace(sourceValue)
	if trimmed == "" || mapped == nil {
		return
	}

	if siteCfg == nil {
		mapped["source"] = trimmed
		return
	}

	if fieldName, mappedValue := resolveMappedField(siteCfg, "source", "source", trimmed, "source", false); fieldName != "" && mappedValue != "" {
		mapped[fieldName] = mappedValue
		return
	}

	if fieldName, mappedValue := resolveMappedField(siteCfg, "processing", "processing", trimmed, "processing", true); fieldName != "" && mappedValue != "" {
		mapped[fieldName] = mappedValue
	}
}

func resolveMappedField(siteCfg *SitePublishConfig, mappingKey string, formKey string, value string, fallbackField string, requireConfiguredField bool) (string, string) {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return "", ""
	}

	fieldName := fallbackField
	mappedValue := trimmedValue
	if siteCfg != nil {
		if resolvedField := strings.TrimSpace(siteCfg.FormFields[formKey]); resolvedField != "" {
			fieldName = resolvedField
		} else if requireConfiguredField {
			return "", ""
		}
		mappedValue = strings.TrimSpace(PickMappedValueWithFallback(mappingKey, siteCfg.Mappings[mappingKey], trimmedValue))
	} else if requireConfiguredField {
		return "", ""
	}

	if strings.TrimSpace(fieldName) == "" || strings.TrimSpace(mappedValue) == "" {
		return "", ""
	}
	return strings.TrimSpace(fieldName), strings.TrimSpace(mappedValue)
}

func applyTags(mappingKey string, siteCfg *SitePublishConfig, mapped map[string]string, uploadData map[string]any, standardized map[string]any) {
	if mapped == nil || siteCfg == nil {
		return
	}
	tagMapping := siteCfg.Mappings[mappingKey]
	if len(tagMapping) == 0 {
		return
	}

	allTags := collectAllTags(uploadData, standardized)
	if len(allTags) == 0 {
		return
	}

	tagIDs := make([]string, 0, len(allTags))
	seen := map[string]struct{}{}
	for _, tag := range allTags {
		candidates := []string{tag}
		if strings.HasPrefix(tag, "tag.") {
			candidates = append(candidates, strings.TrimPrefix(tag, "tag."))
		} else {
			candidates = append(candidates, "tag."+tag)
		}
		mappedID := ""
		for _, candidate := range candidates {
			if mappedValue := pickMappedValueWithFallback(mappingKey, tagMapping, candidate, false, false); strings.TrimSpace(mappedValue) != "" {
				mappedID = strings.TrimSpace(mappedValue)
				break
			}
		}
		if mappedID == "" {
			if fallback, ok := tagMapping["default"]; ok {
				mappedID = strings.TrimSpace(fallback)
			}
		}
		if mappedID == "" {
			continue
		}
		if _, exists := seen[mappedID]; exists {
			continue
		}
		seen[mappedID] = struct{}{}
		tagIDs = append(tagIDs, mappedID)
	}
	if len(tagIDs) == 0 {
		return
	}
	sort.Strings(tagIDs)

	base := resolveTagFieldBase(siteCfg)
	if base == "" {
		base = "tags[4]"
	}
	for idx, id := range tagIDs {
		mapped[fmt.Sprintf("%s[%d]", base, idx)] = id
	}
}

func resolveTagFieldBase(siteCfg *SitePublishConfig) string {
	if siteCfg == nil {
		return ""
	}
	if raw := strings.TrimSpace(siteCfg.FormFields["tags[]"]); raw != "" {
		return strings.TrimSuffix(raw, "[]")
	}
	if raw := strings.TrimSpace(siteCfg.FormFields["tag_list[]"]); raw != "" {
		return strings.TrimSuffix(raw, "[]")
	}
	// 一些站点的配置使用 tags[4][] 作为 key，本质上就是默认字段名。
	if _, exists := siteCfg.FormFields["tags[4][]"]; exists {
		return strings.TrimSuffix("tags[4][]", "[]")
	}
	return ""
}

func collectAllTags(uploadData map[string]any, standardized map[string]any) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, 16)

	appendTags := func(value any) {
		for _, tag := range parseStringSlice(value) {
			if tag == "" {
				continue
			}
			if _, exists := seen[tag]; exists {
				continue
			}
			seen[tag] = struct{}{}
			result = append(result, tag)
		}
	}

	appendTags(standardized["tags"])
	if uploadData != nil {
		appendTags(uploadData["tags"])
		if sourceParams, ok := uploadData["source_params"].(map[string]any); ok && sourceParams != nil {
			appendTags(sourceParams["标签"])
		}
	}
	return result
}

func parseStringSlice(value any) []string {
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
			trimmed := strings.TrimSpace(toStringAnyBasic(item, ""))
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return []string{}
		}
		if strings.HasPrefix(text, "[") {
			parsed := []string{}
			if err := json.Unmarshal([]byte(text), &parsed); err == nil {
				return parseStringSlice(parsed)
			}
			parsedAny := []any{}
			if err := json.Unmarshal([]byte(text), &parsedAny); err == nil {
				return parseStringSlice(parsedAny)
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
		return []string{strings.TrimSpace(toStringAnyBasic(typed, ""))}
	}
}

func toStringAnyBasic(value any, fallback string) string {
	switch typed := value.(type) {
	case nil:
		return fallback
	case string:
		return typed
	case []byte:
		return string(typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int8:
		return fmt.Sprintf("%d", typed)
	case int16:
		return fmt.Sprintf("%d", typed)
	case int32:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case uint:
		return fmt.Sprintf("%d", typed)
	case uint8:
		return fmt.Sprintf("%d", typed)
	case uint16:
		return fmt.Sprintf("%d", typed)
	case uint32:
		return fmt.Sprintf("%d", typed)
	case uint64:
		return fmt.Sprintf("%d", typed)
	case float32:
		return fmt.Sprintf("%v", typed)
	case float64:
		return fmt.Sprintf("%v", typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fallback
	}
}
