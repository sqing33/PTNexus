package mapping

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pt-nexus/server-go/internal/platform/logx"
)

const hdfansPublishMappingLogModule = "发布-HDFans"

func applyHdfansOverrides(mapped map[string]string, uploadData map[string]any, ctx MappingContext) {
	if mapped == nil {
		return
	}

	standardized := map[string]any{}
	if uploadData != nil {
		if item, ok := uploadData["standardized_params"].(map[string]any); ok && item != nil {
			standardized = item
		}
	}

	tags := collectHdfansAllTags(uploadData, standardized)
	enhanced := buildHdfansEnhancedTags(tags, uploadData, standardized, ctx)

	rebuildHdfansTagFields(mapped, enhanced)
	refineHdfansMedium(mapped, standardized, enhanced)
}

func collectHdfansAllTags(uploadData map[string]any, standardized map[string]any) map[string]struct{} {
	seen := map[string]struct{}{}
	for _, tag := range parseStringSlice(standardized["tags"]) {
		addTag(seen, tag)
	}
	if uploadData != nil {
		for _, tag := range parseStringSlice(uploadData["tags"]) {
			addTag(seen, tag)
		}
	}
	return seen
}

func buildHdfansEnhancedTags(tags map[string]struct{}, uploadData map[string]any, standardized map[string]any, ctx MappingContext) map[string]struct{} {
	result := map[string]struct{}{}
	for tag := range tags {
		result[tag] = struct{}{}
	}

	// --- 中英双语：仅音轨双语 ---
	if hasAnyTagLower(result, "tag.国语", "tag.粤语", "tag.台配", "国语", "粤语", "台配") &&
		hasAnyTagLower(result, "tag.英语", "英语") {
		result["tag.中英双语"] = struct{}{}
	}

	// --- 源站转发：仅数据库 ---
	sourceNickname := strings.TrimSpace(ctx.SourceSiteNickname)
	if sourceNickname != "" && ctx.FindSiteNicknameByGroup != nil {
		releaseGroup := normalizeHdfansReleaseGroup(extractHdfansReleaseGroupRaw(uploadData, standardized))
		if releaseGroup != "" {
			matchedNickname, err := ctx.FindSiteNicknameByGroup(releaseGroup)
			if err != nil {
				logx.Warnf(hdfansPublishMappingLogModule, "源站转发匹配失败 release_group=%s err=%v", releaseGroup, err)
			} else if strings.TrimSpace(matchedNickname) == sourceNickname {
				result["tag.源站转发"] = struct{}{}
			}
		}
	}

	return result
}

func rebuildHdfansTagFields(mapped map[string]string, tags map[string]struct{}) {
	if mapped == nil {
		return
	}

	// 移除可能残留的 tags[...] 字段，避免重复/污染。
	for key := range mapped {
		if strings.HasPrefix(key, "tags[") {
			delete(mapped, key)
		}
	}

	siteCfg, err := LoadSitePublishConfig("hdfans")
	if err != nil || siteCfg == nil {
		return
	}
	tagMapping := siteCfg.Mappings["tag"]
	if len(tagMapping) == 0 || len(tags) == 0 {
		return
	}

	tagIDs := make([]string, 0, len(tags))
	for tag := range tags {
		candidates := []string{tag}
		if strings.HasPrefix(tag, "tag.") {
			candidates = append(candidates, strings.TrimPrefix(tag, "tag."))
		} else {
			candidates = append(candidates, "tag."+tag)
		}
		for _, candidate := range candidates {
			if mappedValue := pickMappedValueWithFallback("tag", tagMapping, candidate, false, false); strings.TrimSpace(mappedValue) != "" {
				tagIDs = append(tagIDs, strings.TrimSpace(mappedValue))
				break
			}
		}
	}
	if len(tagIDs) == 0 {
		return
	}

	unique := map[string]struct{}{}
	for _, id := range tagIDs {
		unique[id] = struct{}{}
	}
	finalIDs := make([]string, 0, len(unique))
	for id := range unique {
		finalIDs = append(finalIDs, id)
	}
	sort.Strings(finalIDs)

	for idx, id := range finalIDs {
		mapped[fmt.Sprintf("tags[4][%d]", idx)] = id
	}
}

func refineHdfansMedium(mapped map[string]string, standardized map[string]any, tags map[string]struct{}) {
	if mapped == nil {
		return
	}

	mediumField := "medium_sel[4]"
	siteCfg, _ := LoadSitePublishConfig("hdfans")
	if siteCfg != nil {
		if resolved := strings.TrimSpace(siteCfg.FormFields["medium"]); resolved != "" {
			mediumField = resolved
		}
	}

	medium := strings.TrimSpace(toStringAnyBasic(standardized["medium"], ""))
	resolution := strings.TrimSpace(toStringAnyBasic(standardized["resolution"], ""))

	hasDIY := false
	if _, ok := tags["tag.DIY"]; ok {
		hasDIY = true
	} else {
		for tag := range tags {
			if strings.Contains(strings.ToUpper(tag), "DIY") {
				hasDIY = true
				break
			}
		}
	}

	// UHD / BD 原盘（仅 DIY 细分）
	if medium == "medium.uhd_bluray" || medium == "medium.uhd_diy" {
		if hasDIY {
			mapped[mediumField] = "18"
		} else {
			mapped[mediumField] = "17"
		}
		return
	}
	if medium == "medium.bluray" || medium == "medium.bluray_diy" {
		if hasDIY {
			mapped[mediumField] = "22"
		} else {
			mapped[mediumField] = "21"
		}
		return
	}

	// Encode：按分辨率细分（UHD=20 / 1080P/i=24 / 720P=25）
	switch medium {
	case "medium.encode_2160p":
		mapped[mediumField] = "20"
		return
	case "medium.encode_720p":
		mapped[mediumField] = "25"
		return
	case "medium.encode_1080p":
		mapped[mediumField] = "24"
		return
	case "medium.encode":
		switch resolution {
		case "resolution.r2160p":
			mapped[mediumField] = "20"
		case "resolution.r720p":
			mapped[mediumField] = "25"
		default:
			mapped[mediumField] = "24"
		}
		return
	default:
		return
	}
}

func extractHdfansReleaseGroupRaw(uploadData map[string]any, standardized map[string]any) string {
	// 1) 优先使用 title_components 的原始制作组
	if uploadData != nil {
		if raw := strings.TrimSpace(extractTeamFromTitleComponents(uploadData["title_components"])); raw != "" {
			return raw
		}

		if sourceParams, ok := uploadData["source_params"].(map[string]any); ok {
			if raw := strings.TrimSpace(toStringAnyBasic(sourceParams["制作组"], "")); raw != "" {
				return raw
			}
		}
	}

	// 2) 兜底使用 standardized team（可能是 team.xxx；但仍做一下清洗）
	return strings.TrimSpace(toStringAnyBasic(standardized["team"], ""))
}

func extractTeamFromTitleComponents(raw any) string {
	if raw == nil {
		return ""
	}

	checkComponents := func(components []any) string {
		for _, item := range components {
			component, ok := item.(map[string]any)
			if !ok {
				if typed, ok := item.(map[string]interface{}); ok {
					component = map[string]any{}
					for k, v := range typed {
						component[k] = v
					}
				} else {
					continue
				}
			}
			if strings.TrimSpace(toStringAnyBasic(component["key"], "")) != "制作组" {
				continue
			}
			value := component["value"]
			if value == nil {
				continue
			}
			switch typed := value.(type) {
			case []string:
				return strings.TrimSpace(strings.Join(typed, " "))
			case []any:
				parts := make([]string, 0, len(typed))
				for _, entry := range typed {
					text := strings.TrimSpace(toStringAnyBasic(entry, ""))
					if text != "" {
						parts = append(parts, text)
					}
				}
				return strings.TrimSpace(strings.Join(parts, " "))
			default:
				return strings.TrimSpace(toStringAnyBasic(typed, ""))
			}
		}
		return ""
	}

	switch typed := raw.(type) {
	case []map[string]any:
		components := make([]any, 0, len(typed))
		for _, item := range typed {
			components = append(components, item)
		}
		return checkComponents(components)
	case []any:
		return checkComponents(typed)
	case string:
		var parsed []any
		if err := json.Unmarshal([]byte(typed), &parsed); err != nil {
			return ""
		}
		return checkComponents(parsed)
	case []byte:
		var parsed []any
		if err := json.Unmarshal(typed, &parsed); err != nil {
			return ""
		}
		return checkComponents(parsed)
	default:
		return ""
	}
}

func normalizeHdfansReleaseGroup(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}

	// 合作制作组：使用 @ 后面
	if strings.Contains(v, "@") {
		parts := strings.Split(v, "@")
		if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
			v = strings.TrimSpace(parts[1])
		}
	}

	return strings.TrimSpace(strings.TrimLeft(v, "-"))
}

func hasAnyTagLower(tags map[string]struct{}, candidates ...string) bool {
	if len(tags) == 0 || len(candidates) == 0 {
		return false
	}
	for _, candidate := range candidates {
		lower := strings.ToLower(strings.TrimSpace(candidate))
		if lower == "" {
			continue
		}
		for tag := range tags {
			if strings.ToLower(strings.TrimSpace(tag)) == lower {
				return true
			}
		}
	}
	return false
}

func addTag(target map[string]struct{}, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	target[trimmed] = struct{}{}
}
