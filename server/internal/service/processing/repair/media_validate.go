package repair

import (
	"strings"

	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
	processingtagging "github.com/pt-nexus/server/internal/service/processing/tagging"
)

const screenshotValidateLogModule = "媒体校验-截图"

// MediaValidateDeps 定义媒体校验流程依赖。
type MediaValidateDeps struct {
	GetSeedMediaInfo func(seedID string) string
}

// ValidateMediaPayload 按类型执行截图/海报/简介/媒体信息校验与修复。
// 参数/返回：payload 为前端提交参数；rootConfig 为运行配置；csptToken 为 PTGen 令牌；deps 为外部依赖回调。
// 失败场景：参数缺失、外部信息获取失败、自动修复失败会返回对应错误状态码。
// 副作用：可能触发截图生成上传与外部站点请求。
func ValidateMediaPayload(payload map[string]any, rootConfig map[string]any, csptToken string, deps MediaValidateDeps) (map[string]any, int) {
	mediaType := strings.TrimSpace(toStringAny(payload["type"], ""))
	contentName := strings.TrimSpace(toStringAny(payload["content_name"], ""))
	sourceInfo, _ := payload["source_info"].(map[string]any)
	subtitle := strings.TrimSpace(toStringAny(sourceInfo["subtitle"], ""))

	switch mediaType {
	case "screenshot":
		// “重新获取截图”语义：固定从视频重建，不复用来源文本里的截图。
		generated, genErr := GenerateAndUploadScreenshots(ScreenshotGenerateInput{
			Payload:     payload,
			SourceInfo:  sourceInfo,
			ContentName: contentName,
			RootConfig:  rootConfig,
		})
		if genErr != nil {
			return map[string]any{"success": false, "error": genErr.Error()}, 400
		}
		urls := generated
		if len(urls) == 0 {
			return map[string]any{"success": false, "error": "未找到可用截图，且自动生成截图失败。"}, 400
		}
		return map[string]any{"success": true, "screenshots": ToBBCodeImages(urls)}, 200

	case "poster":
		result, errMsg := FetchMovieInfo(mediaType, contentName, subtitle, sourceInfo, csptToken)
		if errMsg != "" {
			return map[string]any{"success": false, "error": errMsg}, 400
		}
		result.Poster = NormalizePosterBBCode(result.Poster)
		return map[string]any{
			"success":               true,
			"posters":               result.Poster,
			"source_links":          BuildSourceLinks(result.IMDb, result.Douban, result.TMDb),
			"extracted_imdb_link":   result.IMDb,
			"extracted_douban_link": result.Douban,
			"extracted_tmdb_link":   result.TMDb,
		}, 200

	case "intro":
		result, errMsg := FetchMovieInfo(mediaType, contentName, subtitle, sourceInfo, csptToken)
		if errMsg != "" {
			return map[string]any{"success": false, "error": errMsg}, 400
		}
		// 从新获取的简介文本中提取“类别”字段对应的标准化标签，仅随响应返回，不落库。
		categoryTags := processingtagging.ExtractTagsFromDescriptionCategory(result.Intro)
		typeOverride := ""
		if processingtagging.CheckAnimationTypeFromDescription(result.Intro) {
			typeOverride = "category.animation"
		}
		// 从新获取的简介文本提取产地，用于前端同步修正标准化产地键（不落库）。
		sourceOverride := strings.TrimSpace(parser.InferSourceFromDescription(result.Intro))
		return map[string]any{
			"success":               true,
			"intro":                 result.Intro,
			"category_tags":         categoryTags,
			"type_override":         typeOverride,
			"source_override":       sourceOverride,
			"source_links":          BuildSourceLinks(result.IMDb, result.Douban, result.TMDb),
			"extracted_imdb_link":   result.IMDb,
			"extracted_douban_link": result.Douban,
			"extracted_tmdb_link":   result.TMDb,
		}, 200

	case "mediainfo":
		current := strings.TrimSpace(toStringAny(payload["current_mediainfo"], ""))
		if current == "" {
			current = strings.TrimSpace(toStringAny(payload["mediainfo"], ""))
		}
		if current == "" {
			current = strings.TrimSpace(toStringAny(sourceInfo["mediainfo"], ""))
		}
		if current == "" {
			seedID := strings.TrimSpace(toStringAny(payload["seed_id"], ""))
			if seedID != "" && deps.GetSeedMediaInfo != nil {
				current = strings.TrimSpace(deps.GetSeedMediaInfo(seedID))
			}
		}
		if current == "" {
			return map[string]any{"success": false, "error": "暂无可用媒体信息，请先执行“重新获取媒体信息”任务。"}, 400
		}
		return map[string]any{"success": true, "mediainfo": current}, 200

	default:
		return map[string]any{"success": false, "error": "不支持的媒体类型: " + mediaType}, 400
	}
}
