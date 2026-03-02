package sites

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

const hdfansPublishMappingLogModule = "发布-HDFans"

// PublishHdfans 执行 HDFans 站点特殊发布流程（标签/媒介覆盖规则）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回发布详情页 URL、是否疑似“种子已存在”、用于自动编辑的表单字段，以及发布过程日志。
// 失败场景：同 Public 发布器。
// 副作用：读取本地种子文件并向目标站点发起上传请求；可选写入 data/tmp/torrents 参数落盘。
func PublishHdfans(input publisher.PublishInput) (publisher.PublishResult, error) {
	next := input
	prevAdjust := input.AdjustFormFields
	next.AdjustFormFields = func(formFields map[string]string) {
		if prevAdjust != nil {
			prevAdjust(formFields)
		}
		applyHdfansOverrides(formFields, input.UploadData, strings.TrimSpace(input.SourceSiteNickname), input.FindSiteNicknameByGroup)
	}

	return publisher.PublishPublic(next)
}

func applyHdfansOverrides(formFields map[string]string, uploadData map[string]any, sourceSiteNickname string, findSiteNicknameByGroup func(releaseGroup string) (string, error)) {
	if formFields == nil {
		return
	}

	standardized := map[string]any{}
	if uploadData != nil {
		if item, ok := uploadData["standardized_params"].(map[string]any); ok && item != nil {
			standardized = item
		}
	}

	tags := collectHdfansAllTags(uploadData, standardized)
	enhanced := buildHdfansEnhancedTags(tags, uploadData, standardized, strings.TrimSpace(sourceSiteNickname), findSiteNicknameByGroup)

	rebuildHdfansTagFields(formFields, enhanced)
	refineHdfansMedium(formFields, standardized, enhanced)
}

func collectHdfansAllTags(uploadData map[string]any, standardized map[string]any) map[string]struct{} {
	seen := map[string]struct{}{}
	for _, tag := range parseHdfansStringSlice(standardized["tags"]) {
		addHdfansTag(seen, tag)
	}
	if uploadData != nil {
		for _, tag := range parseHdfansStringSlice(uploadData["tags"]) {
			addHdfansTag(seen, tag)
		}
	}
	return seen
}

func buildHdfansEnhancedTags(tags map[string]struct{}, uploadData map[string]any, standardized map[string]any, sourceSiteNickname string, findSiteNicknameByGroup func(releaseGroup string) (string, error)) map[string]struct{} {
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
	if sourceSiteNickname != "" && findSiteNicknameByGroup != nil {
		releaseGroup := normalizeHdfansReleaseGroup(extractHdfansReleaseGroupRaw(uploadData, standardized))
		if releaseGroup != "" {
			matchedNickname, err := findSiteNicknameByGroup(releaseGroup)
			if err != nil {
				logx.Warnf(hdfansPublishMappingLogModule, "源站转发匹配失败 release_group=%s err=%v", releaseGroup, err)
			} else if strings.TrimSpace(matchedNickname) == sourceSiteNickname {
				result["tag.源站转发"] = struct{}{}
			}
		}
	}

	return result
}

func rebuildHdfansTagFields(formFields map[string]string, tags map[string]struct{}) {
	if formFields == nil {
		return
	}

	// 移除可能残留的 tags[...] 字段，避免重复/污染。
	for key := range formFields {
		if strings.HasPrefix(key, "tags[") {
			delete(formFields, key)
		}
	}

	siteCfg, err := publishmapping.LoadSitePublishConfig("hdfans")
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
			if mappedValue := publishmapping.PickMappedValueWithFallbackNoDefault("tag", tagMapping, candidate); strings.TrimSpace(mappedValue) != "" {
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
		formFields[fmt.Sprintf("tags[4][%d]", idx)] = id
	}
}

func refineHdfansMedium(formFields map[string]string, standardized map[string]any, tags map[string]struct{}) {
	if formFields == nil {
		return
	}

	mediumField := "medium_sel[4]"
	siteCfg, _ := publishmapping.LoadSitePublishConfig("hdfans")
	if siteCfg != nil {
		if resolved := strings.TrimSpace(siteCfg.FormFields["medium"]); resolved != "" {
			mediumField = resolved
		}
	}

	medium := strings.TrimSpace(toStringAny(standardized["medium"], ""))
	resolution := strings.TrimSpace(toStringAny(standardized["resolution"], ""))

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
			formFields[mediumField] = "18"
		} else {
			formFields[mediumField] = "17"
		}
		return
	}
	if medium == "medium.bluray" || medium == "medium.bluray_diy" {
		if hasDIY {
			formFields[mediumField] = "22"
		} else {
			formFields[mediumField] = "21"
		}
		return
	}

	// Encode：按分辨率细分（UHD=20 / 1080P/i=24 / 720P=25）
	switch medium {
	case "medium.encode_2160p":
		formFields[mediumField] = "20"
		return
	case "medium.encode_720p":
		formFields[mediumField] = "25"
		return
	case "medium.encode_1080p":
		formFields[mediumField] = "24"
		return
	case "medium.encode":
		switch resolution {
		case "resolution.r2160p":
			formFields[mediumField] = "20"
		case "resolution.r720p":
			formFields[mediumField] = "25"
		default:
			formFields[mediumField] = "24"
		}
		return
	default:
		return
	}
}

func extractHdfansReleaseGroupRaw(uploadData map[string]any, standardized map[string]any) string {
	// 1) 优先使用 title_components 的原始制作组
	if uploadData != nil {
		if raw := strings.TrimSpace(extractHdfansTeamFromTitleComponents(uploadData["title_components"])); raw != "" {
			return raw
		}

		if sourceParams, ok := uploadData["source_params"].(map[string]any); ok {
			if raw := strings.TrimSpace(toStringAny(sourceParams["制作组"], "")); raw != "" {
				return raw
			}
		}
	}

	// 2) 兜底使用 standardized team（可能是 team.xxx；但仍做一下清洗）
	return strings.TrimSpace(toStringAny(standardized["team"], ""))
}

func extractHdfansTeamFromTitleComponents(raw any) string {
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
			if strings.TrimSpace(toStringAny(component["key"], "")) != "制作组" {
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
					text := strings.TrimSpace(toStringAny(entry, ""))
					if text != "" {
						parts = append(parts, text)
					}
				}
				return strings.TrimSpace(strings.Join(parts, " "))
			default:
				return strings.TrimSpace(toStringAny(typed, ""))
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

func addHdfansTag(target map[string]struct{}, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	target[trimmed] = struct{}{}
}

func parseHdfansStringSlice(value any) []string {
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
		text := strings.TrimSpace(typed)
		if text == "" {
			return []string{}
		}
		if strings.HasPrefix(text, "[") {
			parsed := []string{}
			if err := json.Unmarshal([]byte(text), &parsed); err == nil {
				return parseHdfansStringSlice(parsed)
			}
			parsedAny := []any{}
			if err := json.Unmarshal([]byte(text), &parsedAny); err == nil {
				return parseHdfansStringSlice(parsedAny)
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
		return []string{strings.TrimSpace(toStringAny(typed, ""))}
	}
}
