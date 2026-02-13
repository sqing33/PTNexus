package sites

import (
	"errors"
	stdhtml "html"
	"regexp"
	"strings"
	"unicode"
)

var (
	reSSDTorrentNameSpan = regexp.MustCompile(`(?is)<span[^>]*id=["']torrent-name["'][^>]*>(.*?)</span>`)
	reSSDTopTitle        = regexp.MustCompile(`(?is)<h1[^>]*id=["']top["'][^>]*>(.*?)</h1>`)
	reSSDTitleBadgeText  = regexp.MustCompile(`(?i)\s*\[[^\]]*(?:免费|free|hot|置顶|促销|活动|推荐|通过)[^\]]*\]\s*$`)
	reSSDTitleBadgeWord  = regexp.MustCompile(`(?i)\s*(?:免费|free|hot|置顶|促销|活动|推荐|通过)\s*$`)
	reSSDAnyHTMLTag      = regexp.MustCompile(`(?is)<[^>]+>`)
	reSSDHTMLBreakTag    = regexp.MustCompile(`(?i)<br\s*/?>`)
	reSSDManyNewline     = regexp.MustCompile(`\n{3,}`)

	reSSDMediaInfoCodeMainInGroup = regexp.MustCompile(`(?is)<[^>]*data-group=["'](?:mediainfo|mediainfo_toggle)["'][^>]*>.*?<div[^>]*class=["'][^"']*codemain[^"']*["'][^>]*>(.*?)</div>`)
	reSSDMediaInfoPreInGroup      = regexp.MustCompile(`(?is)<[^>]*data-group=["'](?:mediainfo|mediainfo_toggle)["'][^>]*>.*?<pre[^>]*>(.*?)</pre>`)
	reSSDCodeMainBlock            = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*codemain[^"']*["'][^>]*>(.*?)</div>`)
	reSSDPreBlock                 = regexp.MustCompile(`(?is)<pre[^>]*>(.*?)</pre>`)

	reSSDVideoImagesBlock = regexp.MustCompile(`(?is)<(?:artical|article)[^>]*class=["'][^"']*video-images[^"']*["'][^>]*>(.*?)</(?:artical|article)>`)
	reSSDScreenshotsBlock = regexp.MustCompile(`(?is)<section[^>]*data-group=["']screenshots["'][^>]*>(.*?)</section>`)
	reSSDScreenshotsDiv   = regexp.MustCompile(`(?is)<div[^>]*data-group=["']screenshots["'][^>]*>(.*?)</div>`)
	reSSDImageURL         = regexp.MustCompile(`https?://[^\s"'<>]+\.(?:jpg|jpeg|png|webp|gif)(?:\?[^\s"'<>]+)?`)
)

// ExtractSSD 提取不可说站点的详情页参数。
// 参数/返回：输入详情页上下文与运行期依赖，返回统一 SeedData。
// 失败场景：详情页为空、关键依赖缺失、关键字段不足。
// 副作用：无。
func ExtractSSD(input Input, runtime Runtime) (SeedData, error) {
	page := strings.TrimSpace(input.PageHTML)
	if page == "" {
		return SeedData{}, errors.New("详情页内容为空")
	}

	title := strings.TrimSpace(firstNonEmpty(extractSSDTitle(page), input.FallbackTitle))
	title = normalizeSSDDotSeparatedTitle(title)
	subtitle := ""
	if runtime.ExtractSubtitle != nil {
		subtitle = strings.TrimSpace(runtime.ExtractSubtitle(page))
	}

	descrHTML := ""
	if runtime.ExtractElementInnerHTMLByID != nil {
		descrHTML = runtime.ExtractElementInnerHTMLByID(page, "div", "kdescr")
		if strings.TrimSpace(descrHTML) == "" {
			descrHTML = runtime.ExtractElementInnerHTMLByID(page, "td", "kdescr")
		}
	}

	descrBBCode := ""
	if runtime.HTMLToBBCode != nil {
		descrBBCode = runtime.HTMLToBBCode(descrHTML)
	}
	extraStatementBBCode := ""
	if runtime.ExtractExtraTextBBCode != nil {
		extraStatementBBCode = runtime.ExtractExtraTextBBCode(page)
	}

	statement, poster, body, screens, mediainfo, statementTags, removedDeclarations := "", "", "", "", "", []string{}, []string{}
	if runtime.ExtractDescriptionSections != nil {
		statement, poster, body, screens, mediainfo, statementTags, removedDeclarations = runtime.ExtractDescriptionSections(descrHTML, descrBBCode, extraStatementBBCode)
	}
	// SSD 的截图往往不在 #kdescr 描述里，而是放在独立的“截图信息”区块。
	// 当通用分段未提取到截图时，从整页 HTML 再兜底提取一次。
	if strings.TrimSpace(screens) == "" {
		screens = extractSSDScreenshotsFromPage(page)
	}
	if strings.TrimSpace(statement) == "" && strings.TrimSpace(extraStatementBBCode) != "" && runtime.BuildStatementFromExtraBBCode != nil {
		statement = runtime.BuildStatementFromExtraBBCode(extraStatementBBCode)
	}
	if strings.TrimSpace(mediainfo) == "" {
		mediainfo = extractSSDMediaInfoFromPage(page, runtime)
	}
	if strings.TrimSpace(mediainfo) == "" && runtime.ExtractMediaInfoFromDetail != nil {
		mediainfo = runtime.ExtractMediaInfoFromDetail(descrHTML, descrBBCode)
	}

	linkText := strings.Join([]string{page, descrBBCode, extraStatementBBCode}, "\n")
	imdbLink := ""
	doubanLink := ""
	tmdbLink := ""
	if runtime.NormalizeExternalLink != nil {
		if runtime.ReIMDbLink != nil {
			imdbLink = runtime.NormalizeExternalLink(runtime.ReIMDbLink.FindString(linkText), runtime.ReIMDbLink)
		}
		if runtime.ReDoubanLink != nil {
			doubanLink = runtime.NormalizeExternalLink(runtime.ReDoubanLink.FindString(linkText), runtime.ReDoubanLink)
		}
		if runtime.ReTMDbLink != nil {
			tmdbLink = runtime.NormalizeExternalLink(runtime.ReTMDbLink.FindString(linkText), runtime.ReTMDbLink)
		}
	}

	inferred := map[string]string{}
	if runtime.InferStandardizedValues != nil {
		inferred = runtime.InferStandardizedValues(title, mediainfo, body)
	}
	team := strings.TrimSpace(inferred["team"])
	if runtime.ExtractTeamFromPage != nil && runtime.NormalizeTeamKey != nil {
		if teamFromPage := runtime.ExtractTeamFromPage(page); teamFromPage != "" {
			teamKey := runtime.NormalizeTeamKey(teamFromPage)
			if teamKey != "" && teamKey != "team.other" {
				team = teamKey
			}
		}
	}

	tags := append([]string{}, statementTags...)
	if runtime.ExtractTagsFromPage != nil {
		tags = append(tags, runtime.ExtractTagsFromPage(page)...)
	}
	if runtime.MergeExplicitSourceTags != nil {
		tags = runtime.MergeExplicitSourceTags(tags)
	}

	data := SeedData{
		Title:     title,
		Subtitle:  subtitle,
		MediaInfo: strings.TrimSpace(mediainfo),
		Intro: IntroData{
			Statement:                strings.TrimSpace(statement),
			Poster:                   strings.TrimSpace(poster),
			Body:                     strings.TrimSpace(body),
			Screenshots:              strings.TrimSpace(screens),
			RemovedARDTUDeclarations: append([]string{}, removedDeclarations...),
		},
		Type:       strings.TrimSpace(inferred["type"]),
		Medium:     strings.TrimSpace(inferred["medium"]),
		VideoCodec: strings.TrimSpace(inferred["video_codec"]),
		AudioCodec: strings.TrimSpace(inferred["audio_codec"]),
		Resolution: strings.TrimSpace(inferred["resolution"]),
		Team:       strings.TrimSpace(team),
		Source:     strings.TrimSpace(inferred["source"]),
		Tags:       append([]string{}, tags...),
		IMDbLink:   strings.TrimSpace(imdbLink),
		DoubanLink: strings.TrimSpace(doubanLink),
		TMDbLink:   strings.TrimSpace(tmdbLink),
	}

	if runtime.BuildSourceParams != nil {
		data.SourceParams = runtime.BuildSourceParams(data)
	}
	data = data.Normalize(input.FallbackTitle)

	if runtime.IsSSDSufficient != nil && !runtime.IsSSDSufficient(data) {
		return data, errors.New("特殊提取结果关键字段不足")
	}
	return data, nil
}

func extractSSDTitle(pageHTML string) string {
	for _, match := range reSSDTorrentNameSpan.FindAllStringSubmatch(strings.TrimSpace(pageHTML), -1) {
		if len(match) < 2 {
			continue
		}
		title := strings.TrimSpace(sanitizeSSDText(match[1], false))
		title = strings.TrimSpace(reSSDTitleBadgeText.ReplaceAllString(title, ""))
		title = strings.TrimSpace(reSSDTitleBadgeWord.ReplaceAllString(title, ""))
		if title != "" {
			return title
		}
	}
	return extractSSDTopTitle(pageHTML)
}

func normalizeSSDDotSeparatedTitle(title string) string {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return ""
	}
	// 仅在“明显是点分隔发布名”的情况下做替换，避免误伤正常标题中的少量点号（如 Mr. Robot）。
	dotCount := strings.Count(trimmed, ".")
	if dotCount < 4 {
		return trimmed
	}
	spaceCount := strings.Count(trimmed, " ")
	if spaceCount > dotCount/2 {
		return trimmed
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
		return trimmed
	}

	var builder strings.Builder
	builder.Grow(len(trimmed))
	runes := []rune(trimmed)
	for idx, r := range runes {
		if r != '.' {
			builder.WriteRune(r)
			continue
		}

		// 对齐 Python 修正规则：
		// - 将发布名中的分隔点替换为空格；
		// - 但保留“数字.数字”且小数部分是 token 末尾的小数点（如 2.0 / 7.1），避免破坏声道数等信息。
		prevIsDigit := idx-1 >= 0 && runes[idx-1] >= '0' && runes[idx-1] <= '9'
		nextIsDigit := idx+1 < len(runes) && runes[idx+1] >= '0' && runes[idx+1] <= '9'
		nextIsTokenEnd := func() bool {
			if idx+2 >= len(runes) {
				return true
			}
			return !isSSDWordRune(runes[idx+2])
		}()
		if prevIsDigit && nextIsDigit && nextIsTokenEnd {
			builder.WriteRune(r)
			continue
		}
		builder.WriteRune(' ')
	}
	return strings.TrimSpace(strings.Join(strings.Fields(builder.String()), " "))
}

func isSSDWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func extractSSDTopTitle(pageHTML string) string {
	match := reSSDTopTitle.FindStringSubmatch(strings.TrimSpace(pageHTML))
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(sanitizeSSDText(match[1], false))
}

func extractSSDMediaInfoFromPage(pageHTML string, runtime Runtime) string {
	patterns := []*regexp.Regexp{
		reSSDMediaInfoCodeMainInGroup,
		reSSDMediaInfoPreInGroup,
		reSSDCodeMainBlock,
		reSSDPreBlock,
	}
	if runtime.ExtractMediaInfoByRegexes == nil {
		return ""
	}
	return strings.TrimSpace(runtime.ExtractMediaInfoByRegexes(pageHTML, patterns))
}

func isLikelyPosterURL(raw string) bool {
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

func extractSSDScreenshotsFromPage(pageHTML string) string {
	page := strings.TrimSpace(pageHTML)
	if page == "" {
		return ""
	}

	urls := make([]string, 0, 12)
	appendURL := func(raw string) {
		url := strings.TrimSpace(raw)
		if url == "" {
			return
		}
		if isLikelyPosterURL(url) {
			return
		}
		urls = appendUniqueString(urls, url)
	}

	for _, m := range reSSDVideoImagesBlock.FindAllStringSubmatch(page, -1) {
		if len(m) < 2 {
			continue
		}
		text := sanitizeSSDText(m[1], true)
		for _, url := range reSSDImageURL.FindAllString(text, -1) {
			appendURL(url)
		}
	}

	if len(urls) == 0 {
		blocks := make([]string, 0, 2)
		for _, m := range reSSDScreenshotsBlock.FindAllStringSubmatch(page, -1) {
			if len(m) >= 2 {
				blocks = append(blocks, m[1])
			}
		}
		for _, m := range reSSDScreenshotsDiv.FindAllStringSubmatch(page, -1) {
			if len(m) >= 2 {
				blocks = append(blocks, m[1])
			}
		}
		for _, block := range blocks {
			for _, url := range reSSDImageURL.FindAllString(block, -1) {
				appendURL(url)
			}
		}
	}

	return toBBCodeImages(urls)
}

func sanitizeSSDText(input string, keepLineBreak bool) string {
	text := input
	if keepLineBreak {
		text = reSSDHTMLBreakTag.ReplaceAllString(text, "\n")
	}
	text = reSSDAnyHTMLTag.ReplaceAllString(text, "")
	text = stdhtml.UnescapeString(text)
	if !keepLineBreak {
		return strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(out) == 0 || out[len(out)-1] == "" {
				continue
			}
		}
		out = append(out, trimmed)
	}
	cleaned := strings.TrimSpace(strings.Join(out, "\n"))
	return strings.TrimSpace(reSSDManyNewline.ReplaceAllString(cleaned, "\n\n"))
}

func appendUniqueString(values []string, item string) []string {
	trimmed := strings.TrimSpace(item)
	if trimmed == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(strings.TrimSpace(existing), trimmed) {
			return values
		}
	}
	return append(values, trimmed)
}

func toBBCodeImages(urls []string) string {
	if len(urls) == 0 {
		return ""
	}
	lines := make([]string, 0, len(urls))
	for _, url := range urls {
		clean := strings.TrimSpace(url)
		if clean == "" {
			continue
		}
		lines = append(lines, "[img]"+clean+"[/img]")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
