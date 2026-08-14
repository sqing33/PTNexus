package sites

import (
	"strings"

	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

// 定义 HDvideo 站点在公共表单发布流程上的差异步骤。
type hdvideoPublisher struct {
	publicSiteDefaults
}

// PublishHDVideo 执行 HDvideo 站点发布流程，补齐站点必填地区、风格与豆瓣字段。
// 参数/返回：input 提供目标站点、种子路径与标准化发布参数；返回公共发布器的发布结果。
// 失败场景：同公共发布器，表单构造失败或站点上传接口返回错误时返回 error。
// 副作用：读取本地 torrent 文件并向 HDvideo 发起上传请求，测试模式下仅落盘参数。
func PublishHDVideo(input publisher.PublishInput) (publisher.PublishResult, error) {
	return publishWithPublicSite(input, hdvideoPublisher{})
}

func (hdvideoPublisher) LogModule() string {
	return "发布-HDvideo"
}

func (hdvideoPublisher) AttemptPrefix(input publisher.PublishInput) string {
	return "检测到 HDvideo 站点：补齐豆瓣字段、地区、默认风格与 MediaInfo code 包裹"
}

func (hdvideoPublisher) BuildDescription(input publisher.PublishInput) string {
	return buildHDVideoDescription(input)
}

func (hdvideoPublisher) AdjustFormFields(input publisher.PublishInput, formFields map[string]string) {
	if formFields == nil {
		return
	}

	// HDvideo 的豆瓣字段为 douban_url，避免公共兜底 dburl 被站点忽略。
	if value := strings.TrimSpace(formFields["dburl"]); value != "" && strings.TrimSpace(formFields["douban_url"]) == "" {
		formFields["douban_url"] = value
	}
	delete(formFields, "dburl")
	delete(formFields, "title")
	delete(formFields, "technical_info")

	if !hasHDVideoRegionField(formFields) {
		formFields["region_sel[4]"] = "28"
	}
	if hasHDVideoStyleField(formFields) {
		return
	}
	formFields["style_sel[4][0]"] = resolveHDVideoDefaultStyle(input.UploadData)
}

func buildHDVideoDescription(input publisher.PublishInput) string {
	uploadData := input.UploadData
	if uploadData == nil {
		uploadData = map[string]any{}
	}

	parts := make([]string, 0, 5)
	for _, key := range []string{"statement", "poster", "body"} {
		if section := strings.TrimSpace(resolveUploadSection(uploadData, key)); section != "" {
			parts = append(parts, section)
		}
	}
	if mediainfo := resolveHDVideoMediaInfo(input); mediainfo != "" {
		parts = append(parts, wrapHDVideoCodeBlock(mediainfo))
	}
	if screenshots := strings.TrimSpace(resolveUploadSection(uploadData, "screenshots")); screenshots != "" {
		parts = append(parts, screenshots)
	}

	if len(parts) == 0 {
		return strings.TrimSpace(input.Description)
	}
	return strings.Join(parts, "\n")
}

func resolveHDVideoMediaInfo(input publisher.PublishInput) string {
	uploadData := input.UploadData
	if uploadData == nil {
		uploadData = map[string]any{}
	}
	return strings.TrimSpace(firstNonEmpty(
		toStringAny(uploadData["mediainfo"], ""),
		toStringAny(uploadData["media_info"], ""),
		toStringAny(uploadData["mediainfo_text"], ""),
		strings.TrimSpace(input.MediaInfo),
	))
}

func wrapHDVideoCodeBlock(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "[code]") && strings.HasSuffix(lower, "[/code]") {
		return trimmed
	}
	return "[code]" + trimmed + "[/code]"
}

func hasHDVideoRegionField(formFields map[string]string) bool {
	if formFields == nil {
		return false
	}
	return strings.TrimSpace(formFields["region_sel[4]"]) != ""
}

func hasHDVideoStyleField(formFields map[string]string) bool {
	for key, value := range formFields {
		trimmedKey := strings.TrimSpace(key)
		if strings.HasPrefix(trimmedKey, "style_sel[4]") && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func resolveHDVideoDefaultStyle(uploadData map[string]any) string {
	standardized := map[string]any{}
	if uploadData != nil {
		if item, ok := uploadData["standardized_params"].(map[string]any); ok && item != nil {
			standardized = item
		}
	}

	tags := resolveSiteCombinedTags(uploadData)
	if hasAnySiteTagLower(tags, "tag.喜剧", "喜剧", "comedy") {
		return "2"
	}
	if hasAnySiteTagLower(tags, "tag.动作", "动作", "action") {
		return "3"
	}
	if hasAnySiteTagLower(tags, "tag.科幻", "科幻", "sci-fi", "science fiction") {
		return "4"
	}
	if hasAnySiteTagLower(tags, "tag.惊悚", "惊悚", "thriller") {
		return "5"
	}
	if hasAnySiteTagLower(tags, "tag.爱情", "爱情", "romance") {
		return "7"
	}
	if hasAnySiteTagLower(tags, "tag.恐怖", "恐怖", "horror") {
		return "8"
	}
	if hasAnySiteTagLower(tags, "tag.犯罪", "犯罪", "crime") {
		return "9"
	}
	if hasAnySiteTagLower(tags, "tag.悬疑", "悬疑", "mystery") {
		return "10"
	}
	if hasAnySiteTagLower(tags, "tag.冒险", "冒险", "adventure") {
		return "11"
	}
	if hasAnySiteTagLower(tags, "tag.MV", "tag.LIVE", "tag.演唱会", "tag.音乐专辑", "MV", "LIVE", "演唱会", "音乐专辑", "音乐") {
		return "23"
	}

	switch strings.TrimSpace(toStringAny(standardized["type"], "")) {
	case "category.mv", "category.music":
		return "23"
	case "category.tv_shows":
		return "24"
	case "category.sports":
		return "26"
	default:
		return "6"
	}
}
