package extract

import (
	"html"
	"regexp"
	"strings"
)

var (
	reIMDbLink   = regexp.MustCompile(`https?://(?:www\.)?imdb\.com/title/tt\d+/?`)
	reDoubanLink = regexp.MustCompile(`https?://movie\.douban\.com/subject/\d+/?`)
	reTMDbLink   = regexp.MustCompile(`https?://(?:www\.)?themoviedb\.org/[a-zA-Z]+/\d+/?`)

	reSummarySpan  = regexp.MustCompile(`(?is)<span[^>]+property=["']v:summary["'][^>]*>(.*?)</span>`)
	reMetaDesc     = regexp.MustCompile(`(?is)<meta[^>]+name=["']description["'][^>]*content=["']([^"']+)["']`)
	reBreakLineTag = regexp.MustCompile(`(?is)<br\s*/?>`)
	reAnyHTMLTag   = regexp.MustCompile(`(?is)<[^>]+>`)
	reMultiSpace   = regexp.MustCompile(`[ \t]+`)
	reMultiNewLine = regexp.MustCompile(`\n{3,}`)
	reImgBBCode    = regexp.MustCompile(`(?is)\[img\](.*?)\[/img\]`)
	reImageURL     = regexp.MustCompile(`https?://[^\s\[\]"'<>]+\.(?:jpg|jpeg|png|webp|gif)`)
	reMediaReportSectionHeader = regexp.MustCompile(`(?i)^(General|Video|Audio|Text|Menu|Chapters)(\s*#\d+)?$`)
)

func appendUniqueString(items []string, value string) []string {
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

func normalizeExternalLink(link string, pattern *regexp.Regexp) string {
	trimmed := strings.TrimSpace(link)
	if trimmed == "" {
		return ""
	}
	if pattern == nil {
		return trimmed
	}
	matched := pattern.FindString(trimmed)
	if matched == "" {
		return trimmed
	}
	return strings.TrimRight(matched, "/")
}

func sanitizeHTMLText(input string, keepLineBreak bool) string {
	text := input
	if keepLineBreak {
		text = reBreakLineTag.ReplaceAllString(text, "\n")
	}
	text = reAnyHTMLTag.ReplaceAllString(text, "")
	text = html.UnescapeString(text)
	lines := strings.Split(text, "\n")
	cleanLines := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(reMultiSpace.ReplaceAllString(line, " "))
		if trimmed == "" {
			continue
		}
		cleanLines = append(cleanLines, trimmed)
	}
	joined := strings.Join(cleanLines, "\n")
	joined = reMultiNewLine.ReplaceAllString(joined, "\n\n")
	return strings.TrimSpace(joined)
}

func sanitizeHTMLPreText(input string, keepLineBreak bool) string {
	text := input
	if keepLineBreak {
		text = reBreakLineTag.ReplaceAllString(text, "\n")
	}
	text = reAnyHTMLTag.ReplaceAllString(text, "")
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\u00a0", " ")

	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t\f\v")
	}

	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	joined := strings.Join(lines[start:end], "\n")
	joined = reMultiNewLine.ReplaceAllString(joined, "\n\n")
	return compactBlankLinesForMediaReports(joined)
}

func compactBlankLinesForMediaReports(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	// 只对 MediaInfo/BDInfo 做定向清理，避免影响普通简介/BBCode 等文本的排版。
	if !isLikelyMediaInfoText(trimmed) && !isLikelyBDInfoText(trimmed) {
		return text
	}

	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))

	appendBlank := func() {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			return
		}
		out = append(out, "")
	}

	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t\f\v")
		if strings.TrimSpace(line) == "" {
			// 仅保留“分节边界”的空行：空行前一个/后一个非空行如果是节标题，则保留一个空行。
			prev := ""
			for j := len(out) - 1; j >= 0; j-- {
				if strings.TrimSpace(out[j]) == "" {
					continue
				}
				prev = strings.TrimSpace(out[j])
				break
			}
			next := i + 1
			for next < len(lines) && strings.TrimSpace(lines[next]) == "" {
				next++
			}

			prevIsHeader := prev != "" && reMediaReportSectionHeader.MatchString(prev)
			nextIsHeader := next < len(lines) && reMediaReportSectionHeader.MatchString(strings.TrimSpace(lines[next]))
			if prevIsHeader || nextIsHeader {
				appendBlank()
			}
			continue
		}
		out = append(out, line)
	}

	joined := strings.TrimSpace(strings.Join(out, "\n"))
	joined = reMultiNewLine.ReplaceAllString(joined, "\n\n")
	return joined
}

func extractDoubanSummary(pageHTML string) string {
	if strings.TrimSpace(pageHTML) == "" {
		return ""
	}
	if match := reSummarySpan.FindStringSubmatch(pageHTML); len(match) >= 2 {
		summary := sanitizeHTMLText(match[1], true)
		if summary != "" {
			return summary
		}
	}
	if match := reMetaDesc.FindStringSubmatch(pageHTML); len(match) >= 2 {
		return sanitizeHTMLText(match[1], false)
	}
	return ""
}

func toBBCodeImages(urls []string) string {
	clean := make([]string, 0, len(urls))
	for _, raw := range urls {
		url := strings.TrimSpace(raw)
		if url == "" {
			continue
		}
		clean = appendUniqueString(clean, url)
	}
	if len(clean) == 0 {
		return ""
	}
	lines := make([]string, 0, len(clean))
	for _, url := range clean {
		lines = append(lines, "[img]"+url+"[/img]")
	}
	return strings.Join(lines, "\n")
}

func extractImageURLsFromText(text string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return []string{}
	}
	result := make([]string, 0)
	for _, match := range reImgBBCode.FindAllStringSubmatch(trimmed, -1) {
		if len(match) < 2 {
			continue
		}
		url := strings.TrimSpace(match[1])
		if url != "" {
			result = appendUniqueString(result, url)
		}
	}
	for _, match := range reImageURL.FindAllString(trimmed, -1) {
		url := strings.TrimSpace(match)
		if url != "" {
			result = appendUniqueString(result, url)
		}
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
