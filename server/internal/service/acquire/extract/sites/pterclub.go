package sites

import (
	"errors"
	"regexp"
	"strings"
)

var (
	rePTerMediaInfoBlock        = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*codetop[^"']*["'][^>]*>.*?mediainfo.*?</div>\s*<div[^>]*class=["'][^"']*(?:hide|show)[^"']*["'][^>]*>(.*?)</div>`)
	rePTerCodeMain              = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*codemain[^"']*["'][^>]*>(.*?)</div>`)
	rePTerInlineMediaInfoMarker = regexp.MustCompile(`(?is)\[url=javascript:void\(0\)\]\s*mediainfo\s*-\s*.*?\[/url\]\s*`)
	reMediaInfoGeneralHeader    = regexp.MustCompile(`(?mi)^General\s*$`)
)

// ExtractPTerClub 提取猫站的详情页参数。
// 参数/返回：输入详情页上下文与运行期依赖，返回统一 SeedData。
// 失败场景：公共提取失败。
// 副作用：无。
func ExtractPTerClub(input Input, runtime Runtime) (SeedData, error) {
	if runtime.ExtractWithPublic == nil {
		return SeedData{}, errors.New("缺少公共提取器依赖")
	}
	data, err := runtime.ExtractWithPublic(input)
	if err != nil {
		return data.Normalize(input.FallbackTitle), err
	}

	if media := extractPTerClubMediaInfo(input.PageHTML, runtime); media != "" {
		data.MediaInfo = media
	}

	if strings.TrimSpace(data.Intro.Body) != "" {
		cleanedBody, embeddedMedia := stripPTerInlineMediaInfoFromBody(data.Intro.Body)
		if cleanedBody != data.Intro.Body {
			data.Intro.Body = cleanedBody
		}
		if strings.TrimSpace(data.MediaInfo) == "" && embeddedMedia != "" {
			data.MediaInfo = embeddedMedia
		}
	}

	if douban := extractPTerURLFromRowByLabels(input.PageHTML, []string{"豆瓣链接", "豆瓣鏈接"}, runtime.ReDoubanLink, runtime.NormalizeExternalLink, runtime.SanitizeHTMLText); douban != "" {
		data.DoubanLink = douban
	}
	if imdb := extractPTerURLFromRowByLabels(input.PageHTML, []string{"IMDb链接", "IMDb鏈接", "IMDB链接"}, runtime.ReIMDbLink, runtime.NormalizeExternalLink, runtime.SanitizeHTMLText); imdb != "" {
		data.IMDbLink = imdb
	}

	if runtime.BuildSourceParams != nil {
		data.SourceParams = runtime.BuildSourceParams(data)
	}
	return data.Normalize(input.FallbackTitle), nil
}

func stripPTerInlineMediaInfoFromBody(body string) (string, string) {
	text := strings.ReplaceAll(body, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if strings.TrimSpace(text) == "" {
		return "", ""
	}

	if loc := rePTerInlineMediaInfoMarker.FindStringIndex(text); len(loc) == 2 && loc[0] >= 0 {
		extracted := strings.TrimSpace(text[loc[0]:])
		extracted = rePTerInlineMediaInfoMarker.ReplaceAllString(extracted, "")
		extracted = normalizePTerMediaReportText(extracted)
		return strings.TrimRight(text[:loc[0]], "\n\t "), extracted
	}

	loc := reMediaInfoGeneralHeader.FindStringIndex(text)
	if len(loc) != 2 || loc[0] < 0 {
		return body, ""
	}
	tail := strings.TrimSpace(text[loc[0]:])
	if !looksLikeMediaInfoReport(tail) {
		return body, ""
	}
	return strings.TrimRight(text[:loc[0]], "\n\t "), normalizePTerMediaReportText(tail)
}

func looksLikeMediaInfoReport(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "complete name") && strings.Contains(lower, "unique id") && strings.Contains(lower, "overall bit rate")
}

func normalizePTerMediaReportText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"\u00a0", " ",
		"\u2007", " ",
		"\u202f", " ",
	)
	return strings.TrimSpace(replacer.Replace(trimmed))
}

func extractPTerClubMediaInfo(pageHTML string, runtime Runtime) string {
	if runtime.ExtractRegexCandidates == nil || runtime.PickMediaInfoCandidate == nil || runtime.PickBDInfoCandidate == nil {
		return ""
	}

	blocks := runtime.ExtractRegexCandidates(pageHTML, rePTerMediaInfoBlock)
	candidates := make([]string, 0, len(blocks))
	for _, block := range blocks {
		codemainCandidates := runtime.ExtractRegexCandidates(block, rePTerCodeMain)
		if len(codemainCandidates) > 0 {
			for _, c := range codemainCandidates {
				text := strings.TrimSpace(c)
				if runtime.SanitizeHTMLPreText != nil {
					text = strings.TrimSpace(runtime.SanitizeHTMLPreText(c, true))
				} else if runtime.NormalizeHTMLBlockText != nil {
					text = strings.TrimSpace(runtime.NormalizeHTMLBlockText(c))
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
		} else if runtime.NormalizeHTMLBlockText != nil {
			text = strings.TrimSpace(runtime.NormalizeHTMLBlockText(block))
		}
		if text != "" {
			candidates = append(candidates, text)
		}
	}
	if len(candidates) == 0 {
		blocks := runtime.ExtractRegexCandidates(pageHTML, rePTerCodeMain)
		for _, block := range blocks {
			text := strings.TrimSpace(block)
			if runtime.SanitizeHTMLPreText != nil {
				text = strings.TrimSpace(runtime.SanitizeHTMLPreText(block, true))
			} else if runtime.NormalizeHTMLBlockText != nil {
				text = strings.TrimSpace(runtime.NormalizeHTMLBlockText(block))
			}
			if text != "" {
				candidates = append(candidates, text)
			}
		}
		if len(candidates) == 0 && runtime.ExtractRegexCandidatesAsText != nil {
			candidates = runtime.ExtractRegexCandidatesAsText(pageHTML, rePTerCodeMain)
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

func extractPTerURLFromRowByLabels(pageHTML string, labels []string, urlPattern *regexp.Regexp, normalize func(rawURL string, pattern *regexp.Regexp) string, sanitize func(input string, keepLineBreak bool) string) string {
	if urlPattern == nil {
		return ""
	}
	for _, label := range labels {
		escaped := regexp.QuoteMeta(strings.TrimSpace(label))
		pattern := regexp.MustCompile(`(?is)<td[^>]*>\s*` + escaped + `\s*</td>\s*<td[^>]*>(.*?)</td>`)
		match := pattern.FindStringSubmatch(pageHTML)
		if len(match) < 2 {
			continue
		}
		rawValue := match[1]
		if link := normalizePTerURL(urlPattern.FindString(rawValue), urlPattern, normalize); link != "" {
			return link
		}
		plainValue := strings.TrimSpace(rawValue)
		if sanitize != nil {
			plainValue = strings.TrimSpace(sanitize(rawValue, true))
		}
		if link := normalizePTerURL(urlPattern.FindString(plainValue), urlPattern, normalize); link != "" {
			return link
		}
	}
	return ""
}

func normalizePTerURL(rawURL string, pattern *regexp.Regexp, normalize func(rawURL string, pattern *regexp.Regexp) string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	if normalize != nil {
		return normalize(trimmed, pattern)
	}
	return strings.TrimRight(trimmed, "/")
}
