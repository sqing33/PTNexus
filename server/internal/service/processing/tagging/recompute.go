package tagging

import (
	"strings"

	processingmedia "github.com/pt-nexus/server-go/internal/service/processing/media"
)

// RecomputeStandardTags 根据当前种子文本信息重新计算标准化 tags，并给出可选的类型修正（仅动画判定）。
// 参数/返回：siteCode 用于读取站点 tag 映射；title/subtitle/statement/body/mediainfo/titleComponents 为种子信息；savePath/torrentNameForPath/downloaderID/rootConfig 用于完结检测；
// existingTags 会被当作候选来源之一（避免丢失已存在的有效标准标签）。
// 失败场景：映射配置缺失或媒体文本为空时，会输出更少的标签，但不会返回错误。
func RecomputeStandardTags(
	siteCode string,
	title string,
	subtitle string,
	statement string,
	body string,
	mediainfo string,
	titleComponents []any,
	savePath string,
	torrentNameForPath string,
	downloaderID string,
	rootConfig map[string]any,
	existingTags []string,
) ([]string, string, []string) {
	description := strings.TrimSpace(strings.Join([]string{strings.TrimSpace(statement), strings.TrimSpace(body)}, "\n"))

	rawTagCandidates := make([]string, 0, len(existingTags)+24)
	rawTagCandidates = append(rawTagCandidates, existingTags...)
	rawTagCandidates = append(rawTagCandidates, ExtractRawTagsFromTitleComponents(AnyTitleComponentsToMaps(titleComponents))...)
	rawTagCandidates = append(rawTagCandidates, ExtractRawTagsFromSubtitle(subtitle)...)
	rawTagCandidates = append(rawTagCandidates, ExtractTagsFromDescriptionCategory(description)...)

	_, isBDInfo, _ := processingmedia.ValidateMediaInfoFormat(strings.TrimSpace(mediainfo))
	rawTagCandidates = append(rawTagCandidates, ExtractRawTagsFromMediaText(mediainfo, isBDInfo)...)

	typeOverride := ""
	if CheckAnimationTypeFromDescription(description) {
		typeOverride = "category.animation"
	}

	contentName := strings.TrimSpace(title)
	if contentName == "" {
		contentName = strings.TrimSpace(torrentNameForPath)
	}
	completion := CheckCompletionStatusWithDownloaderContext(
		title,
		subtitle,
		description,
		CompletionCheckContext{
			SavePath:     savePath,
			TorrentName:  torrentNameForPath,
			ContentName:  contentName,
			DownloaderID: downloaderID,
			RootConfig:   rootConfig,
		},
	)
	if ShouldAddCompletionTag(rawTagCandidates, completion) {
		rawTagCandidates = append(rawTagCandidates, "完结")
	}

	mappedTags, unmappedTags := MapTagsToStandard(rawTagCandidates, siteCode)
	return mappedTags, typeOverride, unmappedTags
}

// AnyTitleComponentsToMaps 将任意数组过滤为有效 title_components 结构切片。
func AnyTitleComponentsToMaps(items []any) []map[string]any {
	if len(items) == 0 {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		component, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key := strings.TrimSpace(toStringAny(component["key"], ""))
		if key == "" {
			continue
		}
		result = append(result, component)
	}
	return result
}

// TagSample 返回标签采样切片，避免日志过长。
func TagSample(tags []string, limit int) []string {
	if limit <= 0 {
		limit = 8
	}
	if len(tags) <= limit {
		return tags
	}
	return tags[:limit]
}
