package sites

import (
	"errors"
	"regexp"
	"strings"
)

var (
	reHDDolbyMediaInfoFull   = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*mediainfo-full[^"']*["'][^>]*>(.*?)</div>`)
	reHDDolbyImageURL        = regexp.MustCompile(`https?://[^\s"'<>]+\.(?:jpg|jpeg|png|webp|gif)(?:\?[^\s"'<>]+)?`)
	reHDDolbyCodeMain        = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*codemain[^"']*["'][^>]*>(.*?)</div>`)
	reHDDolbyPreBlock        = regexp.MustCompile(`(?is)<pre[^>]*>(.*?)</pre>`)
	reHDDolbyScreenshotsWrap = regexp.MustCompile(`(?is)<div[^>]*id=["']kscreenshots["'][^>]*>(.*?)</div>`)
)

// ExtractHDDolby 提取杜比站点的详情页参数。
// 参数/返回：输入详情页上下文与运行期依赖，返回统一 SeedData。
// 失败场景：公共提取失败。
// 副作用：无。
func ExtractHDDolby(input Input, runtime Runtime) (SeedData, error) {
	if runtime.ExtractWithPublic == nil {
		return SeedData{}, errors.New("缺少公共提取器依赖")
	}
	data, err := runtime.ExtractWithPublic(input)
	if err != nil {
		return data.Normalize(input.FallbackTitle), err
	}

	// HDDolby 详情页的截图可能不在 #kdescr 描述里，而是放在独立的截图区块中（例如 div#kscreenshots）。
	// 公共提取器未提取到截图时，从整页 HTML 再兜底提取一次。
	if strings.TrimSpace(data.Intro.Screenshots) == "" {
		if screens := extractHDDolbyScreenshotsFromPage(input.PageHTML, runtime); strings.TrimSpace(screens) != "" {
			data.Intro.Screenshots = screens
		}
	}

	if media := extractHDDolbyMediaInfo(input.PageHTML, runtime); media != "" {
		data.MediaInfo = media
	}
	if imdb := extractHDDolbyIMDbLink(input.PageHTML, runtime); imdb != "" {
		data.IMDbLink = imdb
	}

	if runtime.BuildSourceParams != nil {
		data.SourceParams = runtime.BuildSourceParams(data)
	}
	return data.Normalize(input.FallbackTitle), nil
}

func extractHDDolbyMediaInfo(pageHTML string, runtime Runtime) string {
	if runtime.ExtractRegexCandidates == nil || runtime.PickMediaInfoCandidate == nil || runtime.PickBDInfoCandidate == nil {
		return ""
	}

	blocks := runtime.ExtractRegexCandidates(pageHTML, reHDDolbyMediaInfoFull)
	candidates := make([]string, 0, len(blocks))
	for _, block := range blocks {
		preBlocks := runtime.ExtractRegexCandidates(block, reHDDolbyPreBlock)
		if len(preBlocks) > 0 {
			for _, pre := range preBlocks {
				text := strings.TrimSpace(pre)
				if runtime.SanitizeHTMLPreText != nil {
					text = strings.TrimSpace(runtime.SanitizeHTMLPreText(pre, true))
				} else if runtime.SanitizeHTMLText != nil {
					text = strings.TrimSpace(runtime.SanitizeHTMLText(pre, true))
				}
				if text != "" {
					candidates = append(candidates, text)
				}
			}
			continue
		}
		text := strings.TrimSpace(block)
		if runtime.SanitizeHTMLPreText != nil {
			text = strings.TrimSpace(runtime.SanitizeHTMLPreText(block, true))
		} else if runtime.SanitizeHTMLText != nil {
			text = strings.TrimSpace(runtime.SanitizeHTMLText(block, true))
		}
		if text != "" {
			candidates = append(candidates, text)
		}
	}

	if len(candidates) == 0 {
		codeMains := runtime.ExtractRegexCandidates(pageHTML, reHDDolbyCodeMain)
		for _, block := range codeMains {
			text := strings.TrimSpace(block)
			if runtime.SanitizeHTMLPreText != nil {
				text = strings.TrimSpace(runtime.SanitizeHTMLPreText(block, true))
			} else if runtime.SanitizeHTMLText != nil {
				text = strings.TrimSpace(runtime.SanitizeHTMLText(block, true))
			}
			if text != "" {
				candidates = append(candidates, text)
			}
		}
		if runtime.ReMediaInfoCodeMain != nil {
			codeMains := runtime.ExtractRegexCandidates(pageHTML, runtime.ReMediaInfoCodeMain)
			for _, block := range codeMains {
				text := strings.TrimSpace(block)
				if runtime.SanitizeHTMLPreText != nil {
					text = strings.TrimSpace(runtime.SanitizeHTMLPreText(block, true))
				} else if runtime.SanitizeHTMLText != nil {
					text = strings.TrimSpace(runtime.SanitizeHTMLText(block, true))
				}
				if text != "" {
					candidates = append(candidates, text)
				}
			}
		}
	}

	if picked := runtime.PickMediaInfoCandidate(candidates); picked != "" {
		return picked
	}
	if picked := runtime.PickBDInfoCandidate(candidates); picked != "" {
		return picked
	}
	return ""
}

func extractHDDolbyScreenshotsFromPage(pageHTML string, runtime Runtime) string {
	page := strings.TrimSpace(pageHTML)
	if page == "" {
		return ""
	}

	block := ""
	if runtime.ExtractElementInnerHTMLByID != nil {
		block = runtime.ExtractElementInnerHTMLByID(page, "div", "kscreenshots")
	}
	if strings.TrimSpace(block) == "" {
		matches := reHDDolbyScreenshotsWrap.FindStringSubmatch(page)
		if len(matches) >= 2 {
			block = matches[1]
		}
	}
	if strings.TrimSpace(block) == "" {
		return ""
	}

	urls := make([]string, 0, 12)
	for _, url := range reHDDolbyImageURL.FindAllString(block, -1) {
		clean := strings.TrimSpace(url)
		if clean == "" {
			continue
		}
		if isLikelyHDDolbyPosterURL(clean) {
			continue
		}
		urls = appendUniqueHDDolbyString(urls, clean)
	}
	return toHDDolbyBBCodeImages(urls)
}

func extractHDDolbyIMDbLink(pageHTML string, runtime Runtime) string {
	if runtime.ReIMDbLink == nil {
		return ""
	}

	kimdb := ""
	if runtime.ExtractElementInnerHTMLByID != nil {
		kimdb = runtime.ExtractElementInnerHTMLByID(pageHTML, "div", "kimdb")
	}
	if strings.TrimSpace(kimdb) == "" {
		return ""
	}

	raw := runtime.ReIMDbLink.FindString(kimdb)
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if runtime.NormalizeExternalLink != nil {
		return runtime.NormalizeExternalLink(raw, runtime.ReIMDbLink)
	}
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func isLikelyHDDolbyPosterURL(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "doubanio") || strings.Contains(lower, "tmdb") {
		return true
	}
	if strings.Contains(lower, "poster") || strings.Contains(lower, "l_ratio_poster") {
		return true
	}
	return false
}

func appendUniqueHDDolbyString(items []string, value string) []string {
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

func toHDDolbyBBCodeImages(urls []string) string {
	if len(urls) == 0 {
		return ""
	}
	lines := make([]string, 0, len(urls))
	for _, raw := range urls {
		url := strings.TrimSpace(raw)
		if url == "" {
			continue
		}
		lines = append(lines, "[img]"+url+"[/img]")
	}
	return strings.Join(lines, "\n")
}
