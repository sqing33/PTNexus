package sites

import (
	"errors"
	"regexp"
	"strings"
)

var (
	reHDVideoMediaInfoWrap = regexp.MustCompile(`(?is)<(?:div|td)[^>]*(?:id|class)=["'][^"']*(?:mediainfo|media_info|nexus-media-info-raw)[^"']*["'][^>]*>(.*?)</(?:div|td)>`)
	reHDVideoCodeMain      = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*codemain[^"']*["'][^>]*>(.*?)</div>`)
	reHDVideoPreBlock      = regexp.MustCompile(`(?is)<pre[^>]*>(.*?)</pre>`)
	reHDVideoCodeBlock     = regexp.MustCompile(`(?is)<code[^>]*>(.*?)</code>`)
	reHDVideoImageURL      = regexp.MustCompile(`https?://[^\s"'<>]+\.(?:jpg|jpeg|png|webp|gif)(?:\?[^\s"'<>]+)?`)
	reHDVideoScreenshotBox = regexp.MustCompile(`(?is)<(?:div|td)[^>]*(?:id|class)=["'][^"']*(?:kscreenshots|screenshots?|screenshot-area|torrent-screens)[^"']*["'][^>]*>(.*?)</(?:div|td)>`)
	reHDVideoDetailRow     = regexp.MustCompile(`(?is)<td[^>]*>\s*([^<]{1,40})\s*</td>\s*<td[^>]*>(.*?)</td>`)
)

// ExtractHDVideo 提取 HDvideo 详情页参数，并在公共提取结果缺失时补齐媒体信息、截图和风格标签。
// 参数/返回：输入详情页上下文与运行期依赖，返回统一 SeedData 供后续修复、入库和发布流程使用。
// 失败场景：公共提取器缺失或公共提取失败时返回错误；标签列表补抓失败仅忽略，不中断详情页提取。
// 副作用：可能通过列表页请求补充标签；不会写数据库或修改本地文件。
func ExtractHDVideo(input Input, runtime Runtime) (SeedData, error) {
	if runtime.ExtractWithPublic == nil {
		return SeedData{}, errors.New("缺少公共提取器依赖")
	}
	data, err := runtime.ExtractWithPublic(input)
	if err != nil {
		return data.Normalize(input.FallbackTitle), err
	}

	if media := extractHDVideoMediaInfo(input.PageHTML, runtime); media != "" {
		data.MediaInfo = media
	}
	if strings.TrimSpace(data.Intro.Screenshots) == "" {
		if screens := extractHDVideoScreenshots(input.PageHTML, data.Intro.Poster, runtime); screens != "" {
			data.Intro.Screenshots = screens
		}
	}
	if imdb := extractHDVideoExternalLink(input.PageHTML, runtime.ReIMDbLink, runtime.NormalizeExternalLink); imdb != "" {
		data.IMDbLink = imdb
	}
	if douban := extractHDVideoExternalLink(input.PageHTML, runtime.ReDoubanLink, runtime.NormalizeExternalLink); douban != "" {
		data.DoubanLink = douban
	}
	if tmdb := extractHDVideoExternalLink(input.PageHTML, runtime.ReTMDbLink, runtime.NormalizeExternalLink); tmdb != "" {
		data.TMDbLink = tmdb
	}

	data.Tags = mergeHDVideoTags(data.Tags, extractHDVideoStyleTags(input.PageHTML, runtime), runtime)
	if runtime.FetchTagsFromTorrentList != nil && len(data.Tags) == 0 {
		if tags, tagErr := runtime.FetchTagsFromTorrentList(input.BaseURL, input.Cookie, data.Title, input.TorrentID); tagErr == nil && len(tags) > 0 {
			data.Tags = mergeHDVideoTags(data.Tags, tags, runtime)
		}
	}
	applyHDVideoInferredFallbacks(&data, runtime)

	if runtime.BuildSourceParams != nil {
		data.SourceParams = runtime.BuildSourceParams(data)
	}
	return data.Normalize(input.FallbackTitle), nil
}

func extractHDVideoMediaInfo(pageHTML string, runtime Runtime) string {
	if runtime.ExtractRegexCandidates == nil || runtime.PickMediaInfoCandidate == nil || runtime.PickBDInfoCandidate == nil {
		return ""
	}

	candidates := make([]string, 0, 8)
	for _, pattern := range []*regexp.Regexp{reHDVideoMediaInfoWrap, reHDVideoCodeMain, reHDVideoPreBlock, reHDVideoCodeBlock} {
		for _, block := range runtime.ExtractRegexCandidates(pageHTML, pattern) {
			text := cleanHDVideoPreText(block, runtime)
			if text == "" {
				continue
			}
			candidates = append(candidates, text)
		}
	}
	if runtime.ReMediaInfoCodeMain != nil {
		for _, block := range runtime.ExtractRegexCandidates(pageHTML, runtime.ReMediaInfoCodeMain) {
			text := cleanHDVideoPreText(block, runtime)
			if text != "" {
				candidates = append(candidates, text)
			}
		}
	}

	if picked := runtime.PickMediaInfoCandidate(candidates); picked != "" {
		return stripHDVideoCodeWrapper(picked)
	}
	if picked := runtime.PickBDInfoCandidate(candidates); picked != "" {
		return stripHDVideoCodeWrapper(picked)
	}
	return ""
}

func cleanHDVideoPreText(raw string, runtime Runtime) string {
	text := strings.TrimSpace(raw)
	if runtime.SanitizeHTMLPreText != nil {
		text = strings.TrimSpace(runtime.SanitizeHTMLPreText(raw, true))
	} else if runtime.SanitizeHTMLText != nil {
		text = strings.TrimSpace(runtime.SanitizeHTMLText(raw, true))
	} else if runtime.NormalizeHTMLBlockText != nil {
		text = strings.TrimSpace(runtime.NormalizeHTMLBlockText(raw))
	}
	return stripHDVideoCodeWrapper(text)
}

func stripHDVideoCodeWrapper(text string) string {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "[code]") && strings.HasSuffix(lower, "[/code]") {
		return strings.TrimSpace(trimmed[len("[code]") : len(trimmed)-len("[/code]")])
	}
	return trimmed
}

func extractHDVideoScreenshots(pageHTML string, posterBBCode string, runtime Runtime) string {
	blocks := make([]string, 0, 4)
	if runtime.ExtractElementInnerHTMLByID != nil {
		for _, id := range []string{"kscreenshots", "screenshots", "torrent-screens"} {
			for _, tag := range []string{"div", "td"} {
				if block := strings.TrimSpace(runtime.ExtractElementInnerHTMLByID(pageHTML, tag, id)); block != "" {
					blocks = append(blocks, block)
				}
			}
		}
	}
	if runtime.ExtractRegexCandidates != nil {
		blocks = append(blocks, runtime.ExtractRegexCandidates(pageHTML, reHDVideoScreenshotBox)...)
	}
	if len(blocks) == 0 {
		return ""
	}

	posterURLs := extractHDVideoImageURLs(posterBBCode)
	urls := make([]string, 0, 12)
	for _, block := range blocks {
		for _, url := range extractHDVideoImageURLs(block) {
			if containsHDVideoString(posterURLs, url) {
				continue
			}
			urls = appendUniqueHDVideoString(urls, url)
		}
	}
	return toHDVideoBBCodeImages(urls)
}

func extractHDVideoImageURLs(text string) []string {
	urls := make([]string, 0)
	for _, raw := range reHDVideoImageURL.FindAllString(strings.TrimSpace(text), -1) {
		clean := strings.TrimSpace(raw)
		if clean != "" {
			urls = appendUniqueHDVideoString(urls, clean)
		}
	}
	return urls
}

func extractHDVideoExternalLink(pageHTML string, pattern *regexp.Regexp, normalize func(rawURL string, pattern *regexp.Regexp) string) string {
	if pattern == nil {
		return ""
	}
	raw := strings.TrimSpace(pattern.FindString(pageHTML))
	if raw == "" {
		return ""
	}
	if normalize != nil {
		return normalize(raw, pattern)
	}
	return strings.TrimRight(raw, "/")
}

func extractHDVideoStyleTags(pageHTML string, runtime Runtime) []string {
	tags := make([]string, 0, 8)
	if runtime.ExtractTagsFromPage != nil {
		tags = append(tags, runtime.ExtractTagsFromPage(pageHTML)...)
	}

	for _, match := range reHDVideoDetailRow.FindAllStringSubmatch(strings.TrimSpace(pageHTML), -1) {
		if len(match) < 3 {
			continue
		}
		label := cleanHDVideoCellText(match[1], runtime)
		if !isHDVideoStyleLabel(label) {
			continue
		}
		value := cleanHDVideoCellText(match[2], runtime)
		for _, item := range splitHDVideoTagValues(value) {
			tags = appendUniqueHDVideoString(tags, normalizeHDVideoTag(item))
		}
	}
	return tags
}

func cleanHDVideoCellText(raw string, runtime Runtime) string {
	if runtime.SanitizeHTMLText != nil {
		return strings.TrimSpace(runtime.SanitizeHTMLText(raw, true))
	}
	return strings.TrimSpace(raw)
}

func isHDVideoStyleLabel(label string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(label)), ""))
	switch normalized {
	case "风格", "题材", "genre", "genres", "style", "styles":
		return true
	default:
		return false
	}
}

func splitHDVideoTagValues(value string) []string {
	replacer := strings.NewReplacer(
		"\r\n", "\n",
		"\r", "\n",
		"／", "/",
		"、", "/",
		"，", "/",
		",", "/",
		"；", "/",
		";", "/",
		"|", "/",
	)
	normalized := replacer.Replace(strings.TrimSpace(value))
	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		return r == '/' || r == '\n' || r == '\t'
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func normalizeHDVideoTag(raw string) string {
	tag := strings.TrimSpace(raw)
	if tag == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(tag), "tag.") {
		return "tag." + strings.TrimSpace(tag[4:])
	}
	return "tag." + tag
}

func mergeHDVideoTags(primary []string, extra []string, runtime Runtime) []string {
	merged := make([]string, 0, len(primary)+len(extra))
	for _, tag := range primary {
		merged = appendUniqueHDVideoString(merged, tag)
	}
	for _, tag := range extra {
		merged = appendUniqueHDVideoString(merged, tag)
	}
	if runtime.MergeExplicitSourceTags != nil {
		return runtime.MergeExplicitSourceTags(merged)
	}
	return merged
}

func applyHDVideoInferredFallbacks(data *SeedData, runtime Runtime) {
	if data == nil || runtime.InferStandardizedValues == nil {
		return
	}
	inferred := runtime.InferStandardizedValues(data.Title, data.MediaInfo, data.Intro.Body)
	if value := strings.TrimSpace(inferred["medium"]); shouldUseHDVideoFallback(data.Medium, "medium.other") && value != "" {
		data.Medium = value
	}
	if value := strings.TrimSpace(inferred["video_codec"]); shouldUseHDVideoFallback(data.VideoCodec, "video.other") && value != "" {
		data.VideoCodec = value
	}
	if value := strings.TrimSpace(inferred["audio_codec"]); shouldUseHDVideoFallback(data.AudioCodec, "audio.other") && value != "" {
		data.AudioCodec = value
	}
	if value := strings.TrimSpace(inferred["resolution"]); shouldUseHDVideoFallback(data.Resolution, "resolution.other") && value != "" {
		data.Resolution = value
	}
	if value := strings.TrimSpace(inferred["team"]); shouldUseHDVideoFallback(data.Team, "team.other") && value != "" {
		data.Team = value
	}
	if value := strings.TrimSpace(inferred["source"]); shouldUseHDVideoFallback(data.Source, "source.other") && value != "" {
		data.Source = value
	}
}

func shouldUseHDVideoFallback(current string, fallbackValues ...string) bool {
	trimmed := strings.TrimSpace(current)
	if trimmed == "" {
		return true
	}
	for _, fallback := range fallbackValues {
		if strings.EqualFold(trimmed, strings.TrimSpace(fallback)) {
			return true
		}
	}
	return false
}

func containsHDVideoString(items []string, value string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == strings.TrimSpace(value) {
			return true
		}
	}
	return false
}

func appendUniqueHDVideoString(items []string, value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return items
	}
	for _, item := range items {
		if strings.TrimSpace(item) == trimmed {
			return items
		}
	}
	return append(items, trimmed)
}

func toHDVideoBBCodeImages(urls []string) string {
	if len(urls) == 0 {
		return ""
	}
	lines := make([]string, 0, len(urls))
	for _, raw := range urls {
		url := strings.TrimSpace(raw)
		if url != "" {
			lines = append(lines, "[img]"+url+"[/img]")
		}
	}
	return strings.Join(lines, "\n")
}
