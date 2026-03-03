package extract

import (
	"fmt"
	stdhtml "html"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
)

var (
	reTopTitle               = regexp.MustCompile(`(?is)<h1[^>]*id=["']top["'][^>]*>(.*?)</h1>`)
	rePageTitle              = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reSubTitleRow            = regexp.MustCompile(`(?is)(?:副标题|副標題|subtitle)\s*[:：]\s*([^\n<]{2,200})`)
	reSubtitleByAby          = regexp.MustCompile(`(?i)\s*\|\s*aby\s+[^|]+$`)
	reSubtitleByBy           = regexp.MustCompile(`(?i)\s*\|\s*by\s+[^|]+$`)
	reSubtitleByA            = regexp.MustCompile(`(?i)\s*\|\s*a\s+[^|]+$`)
	reSubtitleToolID         = regexp.MustCompile(`(?i)\s*\|\s*(?:atu|dtu|pter)\s*$`)
	reQuoteBlock             = regexp.MustCompile(`(?is)\[quote\](.*?)\[/quote\]`)
	rePreBlock               = regexp.MustCompile(`(?is)<pre[^>]*>(.*?)</pre>`)
	reTagsFieldRow           = regexp.MustCompile(`(?is)<td[^>]*>.*?(?:标签|標籤|tags?|类别与标签|類別與標籤).*?</td>\s*<td[^>]*>(.*?)</td>`)
	reTeamFieldRow           = regexp.MustCompile(`(?is)<td[^>]*>.*?(?:制作组|製作組|团队|團隊|team).*?</td>\s*<td[^>]*>(.*?)</td>`)
	reTeamInline             = regexp.MustCompile(`(?is)(?:制作组|製作組|团队|團隊|team)\s*[:：]\s*([^\n<]{1,80})`)
	reCellSpan               = regexp.MustCompile(`(?is)<span[^>]*>(.*?)</span>`)
	reCellAnchor             = regexp.MustCompile(`(?is)<a[^>]*>(.*?)</a>`)
	reBBCodeTag              = regexp.MustCompile(`(?is)\[(?:/?(?:img|url|quote|b|i|u|color|size)[^\]]*)\]`)
	reManyNewlines           = regexp.MustCompile(`\n{3,}`)
	reLeadingBullets         = regexp.MustCompile(`(?m)^\s*[-*]\s+`)
	reTitleBadgeHTML         = regexp.MustCompile(`(?is)(?:\s*<(?:b|span|font)[^>]*>.*?</(?:b|span|font)>\s*|\s*<img[^>]*>\s*)+$`)
	reTitleBadgeText         = regexp.MustCompile(`(?i)\s*\[[^\]]*(?:免费|free|hot|置顶|促销|活动|推荐|通过)[^\]]*\]\s*$`)
	reTitleBadgeWord         = regexp.MustCompile(`(?i)\s*(?:免费|free|hot|置顶|促销|活动|推荐|通过)\s*$`)
	reTitleRemainingTimeText = regexp.MustCompile(`(?i)\s*(?:剩余时间|剩餘時間|remaining\s*time)\s*[:：].*$`)
	reQuotePrefix            = regexp.MustCompile(`(?im)^\s*(?:\[?(?:引用|quote)\]?\s*[:：]?\s*)`)
	reQuoteOpen              = regexp.MustCompile(`(?is)^\s*\[quote\]\s*`)
	reQuoteClose             = regexp.MustCompile(`(?is)\s*\[/quote\]\s*$`)
	reNestedQuoteIn          = regexp.MustCompile(`(?is)\[quote\]\s*\[quote\]`)
	reNestedQuoteOut         = regexp.MustCompile(`(?is)\[/quote\]\s*\[/quote\]`)
	reQuoteOrImage           = regexp.MustCompile(`(?is)\[quote\].*?\[/quote\]|\[img\].*?\[/img\]`)
	reByARDTU                = regexp.MustCompile(`(?i)\s*By ARDTU\s*`)
	reFontSizeCSS            = regexp.MustCompile(`(?i)^\s*([0-9]+(?:\.[0-9]+)?)\s*(px|pt)\s*$`)
	reRGBColorCSS            = regexp.MustCompile(`(?i)^\s*rgba?\(\s*([0-9]{1,3})\s*,\s*([0-9]{1,3})\s*,\s*([0-9]{1,3})(?:\s*,\s*([0-9.]+))?\s*\)\s*$`)
	// 详情页 HTML 往往会在 <br> 后面带源码换行与缩进（例如：<br>\n    ◎片名...）。
	// x/net/html 会把这些换行当作文本节点保留，导致 HTML→BBCode 时把单个 <br> 误变成空白行（\n\n）。
	// 这里在解析前清理 <br> 后面的源码换行，仅保留 <br> 语义本身。
	reBRFollowedBySourceNewline = regexp.MustCompile(`(?i)(<br\s*/?>)[ \t]*\n+[ \t]*`)
	reVideoSection              = regexp.MustCompile(`(?is)Video[\s\S]*?(?:\n\s*\n|$)`)
	reWidthPixels               = regexp.MustCompile(`(?i)\bwidth\s*:\s*(\d+)\s*(\d*)\s*pixels?`)
	reHeightPixels              = regexp.MustCompile(`(?i)\bheight\s*:\s*(\d+)\s*(\d*)\s*pixels?`)
	reResolutionSlash           = regexp.MustCompile(`(?i)\b(\d{3,5})\s*/\s*(\d{3,5})\b`)
	reResolutionX               = regexp.MustCompile(`(?i)\b(\d{3,5})\s*[xX]\s*(\d{3,5})\b`)
	reResolutionToken           = regexp.MustCompile(`(?i)\b(4320p|8k|2160p|4k|1080i|1080p|720p|480p)\b`)
	reMediaInfoGeneralAnchor    = regexp.MustCompile(`(?is)\bGeneral\s*(?:\r?\n|\s{2,})\s*Unique ID\b`)
	reMediaInfoSectionStartLine = regexp.MustCompile(`(?im)^\s*(General|Video|Audio|Text(?:\s*#\d+)?|Menu|Chapters)\s*$`)
	reBDInfoDiscAnchor          = regexp.MustCompile(`(?is)\bDISC INFO\b`)
	reBDInfoSectionStartLine    = regexp.MustCompile(`(?im)^\s*(DISC INFO|PLAYLIST REPORT|QUICK SUMMARY|FILES:|CHAPTERS:|DISC SIZE)\s*$`)
)

var movieIntroQuotePatterns = []*regexp.Regexp{
	regexp.MustCompile(`[◎❁]片　　名`),
	regexp.MustCompile(`[◎❁]译　　名`),
	regexp.MustCompile(`[◎❁]年　　代`),
	regexp.MustCompile(`[◎❁]产　　地`),
	regexp.MustCompile(`[◎❁]类　　别`),
	regexp.MustCompile(`[◎❁]语　　言`),
	regexp.MustCompile(`[◎❁]导　　演`),
	regexp.MustCompile(`[◎❁]主　　演`),
	regexp.MustCompile(`[◎❁]简　　介`),
	regexp.MustCompile(`[◎❁]演　　员`),
	regexp.MustCompile(`[◎❁]演  员`),
	regexp.MustCompile(`[◎❁]IMDB评分`),
	regexp.MustCompile(`[◎❁]IMDb评分`),
	regexp.MustCompile(`[◎❁]获奖情况`),
	regexp.MustCompile(`制片国家/地区`),
}

type reviewExtractedData struct {
	Title                    string
	Subtitle                 string
	Statement                string
	Poster                   string
	Body                     string
	Screens                  string
	Mediainfo                string
	RemovedARDTUDeclarations []string
	Tags                     []string
	Type                     string
	Medium                   string
	VideoCodec               string
	AudioCodec               string
	Resolution               string
	Team                     string
	Source                   string
	IMDbLink                 string
	DoubanLink               string
	TMDbLink                 string
}

func extractReviewDataFromHTML(pageHTML, fallbackTitle string) reviewExtractedData {
	result := reviewExtractedData{}
	page := strings.TrimSpace(pageHTML)
	if page == "" {
		result.Title = strings.TrimSpace(fallbackTitle)
		return result
	}

	title := extractTopTitle(page)
	if title == "" {
		title = strings.TrimSpace(fallbackTitle)
	}
	result.Title = title

	result.Subtitle = extractSubtitle(page)
	descrHTML := extractElementInnerHTMLByID(page, "div", "kdescr")
	if strings.TrimSpace(descrHTML) == "" {
		descrHTML = extractElementInnerHTMLByID(page, "td", "kdescr")
	}

	descrBBCode := htmlToBBCode(descrHTML)
	extraStatementBBCode := extractExtraTextBBCode(page)
	statement, poster, body, screens, mediainfo, statementTags, ardtuDeclarations := extractDescriptionSections(descrHTML, descrBBCode, extraStatementBBCode)
	result.Statement = statement
	result.Poster = poster
	result.Screens = screens
	if strings.TrimSpace(mediainfo) == "" {
		mediainfo = extractMediaInfoFallbackFromPage(page)
	}
	result.Mediainfo = mediainfo
	result.Body = body
	result.RemovedARDTUDeclarations = ardtuDeclarations

	if imdb := normalizeExternalLink(reIMDbLink.FindString(page+"\n"+descrBBCode), reIMDbLink); imdb != "" {
		result.IMDbLink = imdb
	}
	if douban := normalizeExternalLink(reDoubanLink.FindString(page+"\n"+descrBBCode), reDoubanLink); douban != "" {
		result.DoubanLink = douban
	}
	if tmdb := normalizeExternalLink(reTMDbLink.FindString(page+"\n"+descrBBCode), reTMDbLink); tmdb != "" {
		result.TMDbLink = tmdb
	}

	inferred := inferStandardizedValues(result.Title, result.Mediainfo, result.Body)
	result.Type = inferred["type"]
	result.Medium = inferred["medium"]
	result.VideoCodec = inferred["video_codec"]
	result.AudioCodec = inferred["audio_codec"]
	result.Resolution = inferred["resolution"]
	result.Team = inferred["team"]
	if teamFromPage := extractTeamFromPage(page); teamFromPage != "" {
		teamKey := normalizeTeamKey(teamFromPage)
		if teamKey != "" && teamKey != "team.other" {
			result.Team = teamKey
		}
	}
	result.Source = inferred["source"]
	mergedExtraTags := append([]string{}, statementTags...)
	mergedExtraTags = append(mergedExtraTags, extractTagsFromPage(page)...)
	result.Tags = mergeExplicitSourceTags(mergedExtraTags)

	return result
}

// mergeExplicitSourceTags 合并“显式来源”的标签：声明类标签（tag.*）与页面标签字段（原始文本）。
// 约束：不在这里做关键词推断/降级（例如不把 HDR10+ 变成 HDR），只做 trim/去重/保序。
func mergeExplicitSourceTags(items []string) []string {
	result := make([]string, 0, len(items))
	for _, raw := range items {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if len(tag) >= 4 && strings.EqualFold(tag[:4], "tag.") {
			rest := strings.TrimSpace(tag[4:])
			if rest == "" {
				continue
			}
			tag = "tag." + rest
		}
		result = appendUniqueString(result, tag)
	}
	return result
}

type quoteBlock struct {
	Full  string
	Inner string
	Start int
}

func extractDescriptionSections(descrHTML, descrBBCode, extraStatementBBCode string) (string, string, string, string, string, []string, []string) {
	normalizedBBCode := normalizeNestedQuoteBlocks(strings.TrimSpace(descrBBCode))
	extraStatementBBCode = normalizeNestedQuoteBlocks(strings.TrimSpace(normalizeExtraTextBBCode(extraStatementBBCode)))
	statementTags := detectStatementTags(strings.TrimSpace(normalizedBBCode + "\n" + extraStatementBBCode))
	useDescrStatement := strings.TrimSpace(extraStatementBBCode) == ""

	poster, screens := splitPosterAndScreenshots(normalizedBBCode)
	quoteBlocks := extractQuoteBlocks(normalizedBBCode)
	posterIndex := findPosterIndexInBBCode(normalizedBBCode)
	beforePoster, afterPoster := splitQuotesByPoster(quoteBlocks, posterIndex)

	finalStatementQuotes := make([]string, 0, len(beforePoster))
	ardtuDeclarations := make([]string, 0, 8)
	quotesForBody := make([]string, 0, len(afterPoster))
	mediainfoFromQuote := ""
	foundMediainfoInQuote := false

	for _, block := range beforePoster {
		quoteFull := strings.TrimSpace(block.Full)
		if quoteFull == "" {
			continue
		}
		quoteText := cleanQuoteInnerText(block.Inner)
		if quoteText == "" {
			continue
		}

		isMediainfo := isLikelyMediaInfoText(quoteText)
		isBDInfo := isLikelyBDInfoText(quoteText)
		isReleaseInfoStyle := isReleaseInfoStyleQuote(quoteText)
		if !foundMediainfoInQuote && (isMediainfo || isBDInfo || isReleaseInfoStyle) {
			preserved := cleanQuoteInnerTextPreserveSpacing(block.Inner)
			if preserved == "" {
				preserved = quoteText
			}
			mediainfoFromQuote = preserved
			foundMediainfoInQuote = true
			continue
		}

		if isUnwantedPatternQuote(quoteText) || isTechnicalParamsQuote(quoteText) {
			ardtuDeclarations = appendUniqueString(ardtuDeclarations, quoteText)
			continue
		}

		isArdtuAutoPublish := strings.Contains(quoteFull, "ARDTU工具自动发布")
		isDisclaimer := strings.Contains(quoteFull, "郑重声明：")
		isCswebDisclaimer := strings.Contains(quoteFull, "财神CSWEB提供的所有资源均是在网上搜集且由用户上传")
		isByArdtuGroupInfo := strings.Contains(quoteFull, "By ARDTU") && strings.Contains(quoteFull, "官组作品")
		hasAtuToolSignature := strings.Contains(quoteFull, "| A | By ATU")

		if isArdtuAutoPublish || isDisclaimer || isCswebDisclaimer || hasAtuToolSignature {
			ardtuDeclarations = appendUniqueString(ardtuDeclarations, quoteText)
			continue
		}

		if isByArdtuGroupInfo {
			filteredQuote := strings.TrimSpace(reByARDTU.ReplaceAllString(quoteFull, " "))
			if normalized := normalizeQuoteBlockForOutput(filteredQuote); normalized != "" {
				finalStatementQuotes = append(finalStatementQuotes, normalized)
			}
			continue
		}

		if strings.Contains(quoteFull, "ARDTU") {
			ardtuDeclarations = appendUniqueString(ardtuDeclarations, quoteText)
			continue
		}

		if useDescrStatement {
			if normalized := normalizeQuoteBlockForOutput(quoteFull); normalized != "" {
				finalStatementQuotes = append(finalStatementQuotes, normalized)
			}
		}
	}

	for _, block := range afterPoster {
		quoteFull := strings.TrimSpace(block.Full)
		if quoteFull == "" {
			continue
		}
		quoteText := cleanQuoteInnerText(block.Inner)
		if quoteText == "" {
			continue
		}

		isMediainfo := isLikelyMediaInfoText(quoteText)
		isBDInfo := isLikelyBDInfoText(quoteText)
		isReleaseInfoStyle := isReleaseInfoStyleQuote(quoteText)
		isTechnical := isTechnicalParamsQuote(quoteText)
		isUnwanted := isUnwantedPatternQuote(quoteText)

		if isMediainfo || isBDInfo || isReleaseInfoStyle {
			if !foundMediainfoInQuote {
				preserved := cleanQuoteInnerTextPreserveSpacing(block.Inner)
				if preserved == "" {
					preserved = quoteText
				}
				mediainfoFromQuote = preserved
				foundMediainfoInQuote = true
			}
			continue
		}
		if isTechnical || isUnwanted {
			ardtuDeclarations = appendUniqueString(ardtuDeclarations, quoteText)
			continue
		}

		if normalized := normalizeQuoteBlockForOutput(quoteFull); normalized != "" {
			quotesForBody = append(quotesForBody, normalized)
		}
	}

	statement := strings.TrimSpace(strings.Join(finalStatementQuotes, "\n"))
	if statement != "" {
		statement = strings.TrimSpace(reManyNewlines.ReplaceAllString(statement, "\n\n"))
	}
	if !useDescrStatement {
		statement = buildStatementFromExtraBBCode(extraStatementBBCode)
	}

	body := buildBodyFromQuoteFlow(normalizedBBCode, quotesForBody)
	if useDescrStatement && statement != "" {
		body = removeStatementResidueFromBody(body, statement)
	}
	for _, declaration := range ardtuDeclarations {
		trimmed := strings.TrimSpace(declaration)
		if trimmed == "" {
			continue
		}
		body = strings.ReplaceAll(body, trimmed, "")
	}
	body = strings.TrimSpace(reManyNewlines.ReplaceAllString(strings.TrimSpace(body), "\n\n"))
	body = collapseLineBreaks(body, 1)

	mediainfo := extractMediaInfoFromDetail(descrHTML, "")
	if mediainfo == "" && strings.TrimSpace(mediainfoFromQuote) != "" {
		mediainfo = strings.TrimSpace(mediainfoFromQuote)
	}

	return statement, poster, body, screens, mediainfo, statementTags, ardtuDeclarations
}

func extractExtraTextBBCode(pageHTML string) string {
	page := strings.TrimSpace(pageHTML)
	if page == "" {
		return ""
	}
	for _, tag := range []string{"div", "textarea", "pre"} {
		inner := extractElementInnerHTMLByID(page, tag, "torrent-extra-text-bbcode")
		if strings.TrimSpace(inner) == "" {
			continue
		}
		clean := strings.TrimSpace(normalizeExtraTextBBCode(inner))
		if clean != "" {
			return clean
		}
	}
	return ""
}

func normalizeExtraTextBBCode(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, "\r\n", "\n")
	trimmed = strings.ReplaceAll(trimmed, "\r", "\n")
	trimmed = reBreakLineTag.ReplaceAllString(trimmed, "\n")
	trimmed = reAnyHTMLTag.ReplaceAllString(trimmed, "")
	trimmed = stdhtml.UnescapeString(trimmed)
	trimmed = strings.ReplaceAll(trimmed, "\u00a0", " ")
	return strings.TrimSpace(trimmed)
}

func buildStatementFromExtraBBCode(extraStatementBBCode string) string {
	trimmed := strings.TrimSpace(extraStatementBBCode)
	if trimmed == "" {
		return ""
	}

	if !strings.Contains(strings.ToLower(trimmed), "[quote]") {
		trimmed = "[quote]" + trimmed + "[/quote]"
	}
	trimmed = normalizeNestedQuoteBlocks(trimmed)
	blocks := extractQuoteBlocks(trimmed)
	if len(blocks) == 0 {
		return ""
	}

	finalStatementQuotes := make([]string, 0, len(blocks))
	for _, block := range blocks {
		quoteFull := strings.TrimSpace(block.Full)
		if quoteFull == "" {
			continue
		}
		quoteText := cleanQuoteInnerText(block.Inner)
		if quoteText == "" {
			continue
		}

		if isUnwantedPatternQuote(quoteText) || isTechnicalParamsQuote(quoteText) {
			continue
		}

		isArdtuAutoPublish := strings.Contains(quoteFull, "ARDTU工具自动发布")
		isDisclaimer := strings.Contains(quoteFull, "郑重声明：")
		isCswebDisclaimer := strings.Contains(quoteFull, "财神CSWEB提供的所有资源均是在网上搜集且由用户上传")
		isByArdtuGroupInfo := strings.Contains(quoteFull, "By ARDTU") && strings.Contains(quoteFull, "官组作品")
		hasAtuToolSignature := strings.Contains(quoteFull, "| A | By ATU")
		if isArdtuAutoPublish || isDisclaimer || isCswebDisclaimer || hasAtuToolSignature {
			continue
		}
		if strings.Contains(quoteFull, "ARDTU") {
			continue
		}

		if isByArdtuGroupInfo {
			filteredQuote := strings.TrimSpace(reByARDTU.ReplaceAllString(quoteFull, " "))
			if normalized := normalizeQuoteBlockForOutput(filteredQuote); normalized != "" {
				finalStatementQuotes = append(finalStatementQuotes, normalized)
			}
			continue
		}

		if normalized := normalizeQuoteBlockForOutput(quoteFull); normalized != "" {
			finalStatementQuotes = append(finalStatementQuotes, normalized)
		}
	}

	statement := strings.TrimSpace(strings.Join(finalStatementQuotes, "\n"))
	if statement == "" {
		return ""
	}
	return strings.TrimSpace(reManyNewlines.ReplaceAllString(statement, "\n\n"))
}

func normalizeNestedQuoteBlocks(bbcode string) string {
	normalized := strings.TrimSpace(bbcode)
	if normalized == "" {
		return ""
	}

	for {
		next := reNestedQuoteIn.ReplaceAllString(normalized, "[quote]")
		next = reNestedQuoteOut.ReplaceAllString(next, "[/quote]")
		if next == normalized {
			return strings.TrimSpace(next)
		}
		normalized = next
	}
}

func detectStatementTags(_ string) []string {
	// 对齐 Python：不再从声明文本推断“禁转/限转/分集”标签。
	// 受限标签仅允许来自页面标签字段或站点特殊提取器（如种子列表显式标签）。
	return []string{}
}

func extractQuoteBlocks(bbcode string) []quoteBlock {
	if strings.TrimSpace(bbcode) == "" {
		return []quoteBlock{}
	}
	indexes := reQuoteBlock.FindAllStringSubmatchIndex(bbcode, -1)
	blocks := make([]quoteBlock, 0, len(indexes))
	for _, idx := range indexes {
		if len(idx) < 4 {
			continue
		}
		full := strings.TrimSpace(bbcode[idx[0]:idx[1]])
		inner := strings.TrimSpace(bbcode[idx[2]:idx[3]])
		if full == "" {
			continue
		}
		blocks = append(blocks, quoteBlock{
			Full:  full,
			Inner: inner,
			Start: idx[0],
		})
	}
	return blocks
}

func findPosterIndexInBBCode(bbcode string) int {
	images := extractImageURLsFromText(bbcode)
	if len(images) == 0 {
		return -1
	}
	first := strings.TrimSpace(images[0])
	if first == "" {
		return -1
	}
	return strings.Index(bbcode, first)
}

func splitQuotesByPoster(blocks []quoteBlock, posterIndex int) ([]quoteBlock, []quoteBlock) {
	before := make([]quoteBlock, 0, len(blocks))
	after := make([]quoteBlock, 0, len(blocks))
	for _, block := range blocks {
		if posterIndex >= 0 {
			if block.Start < posterIndex {
				before = append(before, block)
			} else {
				after = append(after, block)
			}
			continue
		}

		if isAcknowledgmentQuote(block.Full) {
			before = append(before, block)
		} else {
			after = append(after, block)
		}
	}
	return before, after
}

func isAcknowledgmentQuote(quote string) bool {
	if strings.TrimSpace(quote) == "" {
		return false
	}
	keywords := []string{"官组", "感谢", "原制作者", "FRDS", "FraMeSToR", "CHD", "字幕组"}
	for _, kw := range keywords {
		if strings.Contains(quote, kw) {
			return true
		}
	}
	return len([]rune(strings.TrimSpace(quote))) < 200
}

func cleanQuoteInnerText(inner string) string {
	clean := strings.TrimSpace(inner)
	if clean == "" {
		return ""
	}
	clean = reBBCodeTag.ReplaceAllString(clean, "")
	clean = strings.TrimSpace(sanitizeBBCodeText(clean))
	clean = strings.TrimSpace(removeQuoteMarkerFromText(clean))
	return strings.TrimSpace(clean)
}

func cleanQuoteInnerTextPreserveSpacing(inner string) string {
	clean := strings.TrimSpace(inner)
	if clean == "" {
		return ""
	}
	clean = strings.TrimSpace(sanitizeBBCodeTextPreserveSpacing(clean))
	clean = strings.TrimSpace(removeQuoteMarkerFromText(clean))
	return strings.TrimSpace(clean)
}

func normalizeQuoteBlockForOutput(quote string) string {
	trimmed := strings.TrimSpace(quote)
	if trimmed == "" {
		return ""
	}
	inner := reQuoteOpen.ReplaceAllString(trimmed, "")
	inner = reQuoteClose.ReplaceAllString(inner, "")
	inner = strings.TrimSpace(sanitizeBBCodeText(inner))
	inner = strings.TrimSpace(removeQuoteMarkerFromText(inner))
	if inner == "" {
		return ""
	}
	return "[quote]" + inner + "[/quote]"
}

func isReleaseInfoStyleQuote(text string) bool {
	upper := strings.ToUpper(strings.TrimSpace(text))
	if upper == "" {
		return false
	}
	return strings.Contains(upper, ".RELEASE.INFO") && strings.Contains(upper, "ENCODER")
}

func isUnwantedPatternQuote(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	unwantedPatterns := []string{
		"ARDTU工具自动发布",
		"CSAUTO工具自动发布",
		"FWAUTO工具自动发布",
		"有错误请评论或举报",
		"郑重声明：",
		"本站提供的所有作品均是用户自行搜集并且上传",
		"用户自行搜集",
		"禁止任何涉及商业盈利目的使用",
		"请在下载后24小时内尽快删除",
		"自动发布",
		"财神CSWEB提供的所有资源均是在网上搜集且由用户上传",
		"不可用于任何形式的商业盈利活动",
		"网上搜集",
		"宽带测试",
		"| A | By ATU",
		"A | By ATU",
		"| By ARDTU",
		"| ARDTU",
		"ARDTU",
		"| A",
		"By ARDTU",
		"By CSAUTO",
		"By FWAUTO",
		".Release.Info",
	}
	for _, pattern := range unwantedPatterns {
		if strings.Contains(trimmed, pattern) {
			return true
		}
	}
	return false
}

func isTechnicalParamsQuote(content string) bool {
	upper := strings.ToUpper(strings.TrimSpace(content))
	if upper == "" {
		return false
	}

	if containsAllKeywords(upper, ".RELEASE.INFO", "ENCODER") {
		return true
	}
	if containsAllKeywords(upper, "ENCODER", "RELEASE NAME") {
		return true
	}
	if containsAllKeywords(upper, ".RELEASE.INFO", ".MEDIA.INFO") {
		return true
	}
	if containsAllKeywords(upper, "VIDEO CODEC", "AUDIO CODEC") {
		return true
	}
	if containsAllKeywords(upper, ".X265.INFO", "X265") {
		return true
	}
	if containsAllKeywords(upper, "RELEASE.NAME", "VIDEO.CODEC") {
		return true
	}
	if containsAllKeywords(upper, "RELEASE NAME", "VIDEO CODEC") {
		return true
	}
	if containsAllKeywords(upper, "RELEASE.NAME", "FRAME.RATE") {
		return true
	}
	if containsAllKeywords(upper, "RELEASE NAME", "FRAME RATE") {
		return true
	}
	if containsAllKeywords(upper, "SUBTITLES", "RELEASE") && strings.Count(content, ".") >= 5 {
		return true
	}
	if containsAllKeywords(upper, "GENERAL INFORMATION", "RELEASE") {
		return true
	}
	if containsAllKeywords(upper, "GENERAL INFORMATION", "VIDEO") {
		return true
	}
	if containsAllKeywords(upper, "COMPARISON", "SOURCE", "ENCODE") && (strings.Contains(content, "___") || strings.Contains(content, "____")) {
		return true
	}
	if containsAllKeywords(upper, "文件名称", "文件体积") {
		return true
	}
	if strings.Contains(upper, "★★★★★ GENERAL INFORMATION ★★★★★") {
		return true
	}
	return false
}

func containsAllKeywords(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if !strings.Contains(text, strings.ToUpper(strings.TrimSpace(keyword))) {
			return false
		}
	}
	return true
}

func isMovieIntroQuote(quoteText string) bool {
	for _, pattern := range movieIntroQuotePatterns {
		if pattern.MatchString(quoteText) {
			return true
		}
	}
	return false
}

func buildBodyFromQuoteFlow(bbcode string, quotesForBody []string) string {
	body := strings.TrimSpace(reQuoteOrImage.ReplaceAllString(bbcode, ""))
	body = strings.ReplaceAll(body, "\r", "")
	body = filterComparisonGuideLines(body)

	if len(quotesForBody) > 0 {
		mergedQuotes := make([]string, 0, len(quotesForBody))
		for _, quote := range quotesForBody {
			normalized := strings.TrimSpace(quote)
			if normalized == "" {
				continue
			}
			mergedQuotes = append(mergedQuotes, normalized)
		}
		if len(mergedQuotes) > 0 {
			if body == "" {
				body = strings.Join(mergedQuotes, "\n")
			} else {
				body = body + "\n\n" + strings.Join(mergedQuotes, "\n")
			}
		}
	}

	body = filterStandaloneKeywordLines(body)
	return sanitizeBBCodeText(body)
}

func filterComparisonGuideLines(body string) string {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	filtered := make([]string, 0, len(lines))
	skipNext := false
	for idx, line := range lines {
		if skipNext {
			skipNext = false
			continue
		}

		plainLine := strings.TrimSpace(reBBCodeTag.ReplaceAllString(line, ""))
		upper := strings.ToUpper(plainLine)

		if strings.Contains(upper, "COMPARISON") && (strings.Contains(upper, "RIGHT") || strings.Contains(upper, "CLICK")) {
			if idx+1 < len(lines) {
				nextPlain := strings.TrimSpace(reBBCodeTag.ReplaceAllString(lines[idx+1], ""))
				nextUpper := strings.ToUpper(nextPlain)
				if strings.Contains(nextUpper, "SOURCE") && strings.Contains(nextUpper, "ENCODE") && strings.Count(lines[idx+1], "_") >= 10 {
					skipNext = true
				}
			}
			continue
		}

		if strings.HasPrefix(upper, "SOURCE") && strings.HasSuffix(upper, "ENCODE") && strings.Count(line, "_") >= 10 {
			continue
		}
		if strings.Contains(upper, "COMPARISON") && strings.Contains(upper, "SOURCE") && strings.Contains(upper, "ENCODE") && strings.Count(line, "_") >= 5 {
			continue
		}

		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

func filterStandaloneKeywordLines(body string) string {
	keywords := map[string]struct{}{
		"mediainfo":  {},
		"screenshot": {},
		"source":     {},
		"encode":     {},
	}

	lines := strings.Split(strings.TrimSpace(body), "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		plainLine := strings.TrimSpace(reBBCodeTag.ReplaceAllString(line, ""))
		if plainLine == "" {
			cleaned = append(cleaned, line)
			continue
		}
		if _, exists := keywords[strings.ToLower(plainLine)]; exists {
			continue
		}
		cleaned = append(cleaned, line)
	}
	return strings.Join(cleaned, "\n")
}

func collapseLineBreaks(text string, maxConsecutive int) string {
	if maxConsecutive < 1 {
		maxConsecutive = 1
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	out := make([]string, 0, len(lines))
	emptyCount := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			emptyCount++
			if emptyCount > maxConsecutive {
				continue
			}
			out = append(out, "")
			continue
		}
		emptyCount = 0
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func extractTopTitle(page string) string {
	if match := reTopTitle.FindStringSubmatch(page); len(match) >= 2 {
		clean := cleanTopTitleText(match[1])
		if clean != "" {
			return clean
		}
	}
	if match := rePageTitle.FindStringSubmatch(page); len(match) >= 2 {
		clean := cleanTopTitleText(match[1])
		if clean != "" {
			clean = strings.TrimSuffix(clean, " - PT Nexus")
			clean = strings.TrimSuffix(clean, " - PTNexus")
			return strings.TrimSpace(clean)
		}
	}
	return ""
}

func cleanTopTitleText(rawHTML string) string {
	trimmed := strings.TrimSpace(rawHTML)
	if trimmed == "" {
		return ""
	}

	// 先移除标题尾部常见的状态徽标节点（如 [免费]、通过图标）。
	cleanHTML := reTitleBadgeHTML.ReplaceAllString(trimmed, "")
	if strings.TrimSpace(cleanHTML) == "" {
		cleanHTML = trimmed
	}

	text := strings.TrimSpace(sanitizeHTMLText(cleanHTML, true))
	if text == "" {
		text = strings.TrimSpace(sanitizeHTMLText(trimmed, true))
	}
	text = strings.Join(strings.Fields(text), " ")

	// 去除“剩余时间：...”这类非标题信息（通常出现在促销徽标后面）。
	text = strings.TrimSpace(reTitleRemainingTimeText.ReplaceAllString(text, ""))

	// 再兜底移除文本层面的尾部状态词，避免把“免费”等误当成标题。
	for {
		next := reTitleBadgeText.ReplaceAllString(text, "")
		next = reTitleBadgeWord.ReplaceAllString(next, "")
		next = reTitleRemainingTimeText.ReplaceAllString(next, "")
		next = strings.TrimSpace(next)
		if next == text {
			break
		}
		text = next
	}
	return strings.TrimSpace(text)
}

func extractSubtitle(page string) string {
	// 站点详情页通常是表格结构：<td>副标题</td><td>xxx</td>，优先用 DOM 方式提取以对齐 Python 行为。
	if fromTable := strings.TrimSpace(extractSubtitleFromTable(page)); fromTable != "" {
		if cleaned := cleanSubtitleValue(fromTable); cleaned != "" {
			return cleaned
		}
		return fromTable
	}

	if match := reSubTitleRow.FindStringSubmatch(page); len(match) >= 2 {
		if text := strings.TrimSpace(sanitizeHTMLText(match[1], true)); text != "" {
			if cleaned := cleanSubtitleValue(text); cleaned != "" {
				return cleaned
			}
			return text
		}
	}
	if summary := strings.TrimSpace(extractDoubanSummary(page)); summary != "" {
		if len([]rune(summary)) > 220 {
			runes := []rune(summary)
			return string(runes[:220]) + "..."
		}
		return summary
	}
	return ""
}

func cleanSubtitleValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}

	value = strings.TrimSpace(reSubtitleByAby.ReplaceAllString(value, ""))
	value = strings.TrimSpace(reSubtitleByBy.ReplaceAllString(value, ""))
	value = strings.TrimSpace(reSubtitleByA.ReplaceAllString(value, ""))
	value = strings.TrimSpace(reSubtitleToolID.ReplaceAllString(value, ""))
	return strings.TrimSpace(value)
}

func extractSubtitleFromTable(pageHTML string) string {
	page := strings.TrimSpace(pageHTML)
	if page == "" {
		return ""
	}

	doc, err := xhtml.Parse(strings.NewReader(page))
	if err != nil || doc == nil {
		return ""
	}

	isSubtitleLabel := func(text string) bool {
		normalized := strings.ToLower(strings.TrimSpace(text))
		normalized = strings.Join(strings.Fields(normalized), "")
		return normalized == "副标题" || normalized == "副標題" || normalized == "subtitle"
	}

	var found string
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node == nil || found != "" {
			return
		}
		if node.Type == xhtml.ElementNode && strings.EqualFold(node.Data, "td") {
			label := strings.TrimSpace(extractVisibleText(node))
			if isSubtitleLabel(label) {
				if next := nextSiblingElement(node, "td"); next != nil {
					candidate := strings.TrimSpace(extractVisibleText(next))
					if candidate != "" {
						found = candidate
						return
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
			if found != "" {
				return
			}
		}
	}

	walk(doc)
	return strings.TrimSpace(found)
}

func nextSiblingElement(node *xhtml.Node, tag string) *xhtml.Node {
	if node == nil {
		return nil
	}
	for sib := node.NextSibling; sib != nil; sib = sib.NextSibling {
		if sib.Type != xhtml.ElementNode {
			continue
		}
		if tag == "" || strings.EqualFold(strings.TrimSpace(sib.Data), strings.TrimSpace(tag)) {
			return sib
		}
		return nil
	}
	return nil
}

func extractVisibleText(node *xhtml.Node) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n == nil {
			return
		}
		switch n.Type {
		case xhtml.TextNode:
			text := stdhtml.UnescapeString(n.Data)
			text = strings.ReplaceAll(text, "\u00a0", " ")
			builder.WriteString(text)
			builder.WriteString(" ")
		case xhtml.ElementNode:
			// 跳过不影响可见文本的节点
			if strings.EqualFold(n.Data, "script") || strings.EqualFold(n.Data, "style") {
				return
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		default:
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
	}
	walk(node)

	text := strings.TrimSpace(builder.String())
	text = strings.Join(strings.Fields(text), " ")
	return strings.TrimSpace(text)
}

func extractTeamFromPage(page string) string {
	trimmed := strings.TrimSpace(page)
	if trimmed == "" {
		return ""
	}

	if match := reTeamFieldRow.FindStringSubmatch(trimmed); len(match) >= 2 {
		cell := strings.TrimSpace(sanitizeHTMLText(match[1], true))
		if cell != "" {
			return cell
		}
	}

	if match := reTeamInline.FindStringSubmatch(trimmed); len(match) >= 2 {
		inline := strings.TrimSpace(sanitizeHTMLText(match[1], true))
		if inline != "" {
			return inline
		}
	}
	return ""
}

func extractTagsFromPage(page string) []string {
	trimmed := strings.TrimSpace(page)
	if trimmed == "" {
		return []string{}
	}

	match := reTagsFieldRow.FindStringSubmatch(trimmed)
	if len(match) < 2 {
		return []string{}
	}
	cell := strings.TrimSpace(match[1])
	if cell == "" {
		return []string{}
	}

	tags := make([]string, 0, 8)
	appendTag := func(raw string) {
		tag := normalizeSourceTagText(raw)
		if tag == "" {
			return
		}
		if shouldIgnoreSourceTag(tag) {
			return
		}
		tags = appendUniqueString(tags, tag)
	}

	for _, item := range reCellSpan.FindAllStringSubmatch(cell, -1) {
		if len(item) >= 2 {
			appendTag(item[1])
		}
	}
	for _, item := range reCellAnchor.FindAllStringSubmatch(cell, -1) {
		if len(item) >= 2 {
			appendTag(item[1])
		}
	}

	if len(tags) == 0 {
		plain := normalizeSourceTagText(sanitizeHTMLText(cell, true))
		for _, token := range strings.FieldsFunc(plain, func(r rune) bool {
			return r == ',' || r == '，' || r == '、' || r == '/' || r == '|' || r == ';'
		}) {
			appendTag(token)
		}
	}

	return tags
}

func normalizeSourceTagText(raw string) string {
	plain := strings.TrimSpace(sanitizeHTMLText(raw, true))
	if plain == "" {
		return ""
	}
	plain = strings.Trim(plain, "[](){}<> ")
	plain = strings.TrimSpace(reMultiSpace.ReplaceAllString(plain, " "))
	return plain
}

func shouldIgnoreSourceTag(tag string) bool {
	ignore := []string{"官方", "官种", "首发", "自购", "自抓", "应求"}
	for _, item := range ignore {
		if strings.EqualFold(strings.TrimSpace(tag), item) {
			return true
		}
	}
	return false
}

func extractElementInnerHTMLByID(pageHTML, tagName, elementID string) string {
	if strings.TrimSpace(pageHTML) == "" || strings.TrimSpace(tagName) == "" || strings.TrimSpace(elementID) == "" {
		return ""
	}
	lower := strings.ToLower(pageHTML)
	tagStart := "<" + strings.ToLower(strings.TrimSpace(tagName))
	idPattern1 := `id="` + strings.ToLower(strings.TrimSpace(elementID)) + `"`
	idPattern2 := `id='` + strings.ToLower(strings.TrimSpace(elementID)) + `'`

	searchPos := 0
	for {
		relStart := strings.Index(lower[searchPos:], tagStart)
		if relStart < 0 {
			return ""
		}
		start := searchPos + relStart
		endTag := strings.Index(lower[start:], ">")
		if endTag < 0 {
			return ""
		}
		startTagEnd := start + endTag + 1
		startTagText := lower[start:startTagEnd]
		if strings.Contains(startTagText, idPattern1) || strings.Contains(startTagText, idPattern2) {
			closeTag := "</" + strings.ToLower(strings.TrimSpace(tagName))
			depth := 1
			cursor := startTagEnd
			for cursor < len(lower) {
				nextOpenRel := strings.Index(lower[cursor:], tagStart)
				nextCloseRel := strings.Index(lower[cursor:], closeTag)
				if nextCloseRel < 0 {
					return ""
				}
				nextClose := cursor + nextCloseRel
				nextOpen := -1
				if nextOpenRel >= 0 {
					nextOpen = cursor + nextOpenRel
				}

				if nextOpen >= 0 && nextOpen < nextClose {
					depth++
					cursor = nextOpen + len(tagStart)
					continue
				}

				depth--
				if depth == 0 {
					return pageHTML[startTagEnd:nextClose]
				}
				cursor = nextClose + len(closeTag)
			}
			return ""
		}
		searchPos = startTagEnd
	}
}

func htmlToBBCode(fragment string) string {
	trimmed := strings.TrimSpace(fragment)
	if trimmed == "" {
		return ""
	}

	trimmed = strings.ReplaceAll(trimmed, "\r\n", "\n")
	trimmed = strings.ReplaceAll(trimmed, "\r", "\n")
	trimmed = reBRFollowedBySourceNewline.ReplaceAllString(trimmed, "$1")

	doc, err := xhtml.Parse(strings.NewReader("<div>" + trimmed + "</div>"))
	if err != nil {
		return sanitizeBBCodeText(sanitizeHTMLText(trimmed, true))
	}

	root := findFirstElementByTag(doc, "div")
	if root == nil {
		return sanitizeBBCodeText(sanitizeHTMLText(trimmed, true))
	}

	var builder strings.Builder
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		builder.WriteString(renderNodeAsBBCode(child))
	}
	return sanitizeBBCodeText(builder.String())
}

func normalizeCSSColorToBBCode(color string) string {
	c := strings.ToLower(strings.TrimSpace(color))
	c = strings.TrimSuffix(c, ";")
	if c == "" {
		return ""
	}
	if match := reRGBColorCSS.FindStringSubmatch(c); len(match) >= 4 {
		r, _ := strconv.Atoi(match[1])
		g, _ := strconv.Atoi(match[2])
		b, _ := strconv.Atoi(match[3])
		if r < 0 || r > 255 || g < 0 || g > 255 || b < 0 || b > 255 {
			return ""
		}
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
	if strings.HasPrefix(c, "#") {
		if len(c) == 4 {
			return fmt.Sprintf("#%c%c%c%c%c%c", c[1], c[1], c[2], c[2], c[3], c[3])
		}
		if len(c) == 7 {
			return c
		}
		return ""
	}
	for _, r := range c {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return ""
	}
	return c
}

func mapPixelSizeToBBCodeSize(px float64) string {
	if px <= 0 {
		return ""
	}
	type item struct {
		size string
		px   float64
	}
	candidates := []item{
		{size: "1", px: 12},
		{size: "2", px: 14},
		{size: "3", px: 16},
		{size: "4", px: 18},
		{size: "5", px: 24},
		{size: "6", px: 32},
		{size: "7", px: 48},
	}
	best := candidates[0]
	bestDiff := math.Abs(px - best.px)
	for _, cand := range candidates[1:] {
		diff := math.Abs(px - cand.px)
		if diff < bestDiff {
			best = cand
			bestDiff = diff
		}
	}
	return best.size
}

func extractFontSizeBBCode(styleValue string) string {
	s := strings.TrimSpace(styleValue)
	if s == "" {
		return ""
	}
	if match := reFontSizeCSS.FindStringSubmatch(s); len(match) >= 3 {
		value, err := strconv.ParseFloat(match[1], 64)
		if err != nil || value <= 0 {
			return ""
		}
		unit := strings.ToLower(strings.TrimSpace(match[2]))
		px := value
		if unit == "pt" {
			px = value * 4.0 / 3.0
		}
		return mapPixelSizeToBBCodeSize(px)
	}
	return ""
}

func styleValue(style, key string) string {
	s := strings.TrimSpace(style)
	if s == "" || key == "" {
		return ""
	}
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	parts := strings.Split(s, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		v := strings.TrimSpace(kv[1])
		if k == lowerKey && v != "" {
			return v
		}
	}
	return ""
}

func findFirstElementByTag(node *xhtml.Node, tag string) *xhtml.Node {
	if node == nil {
		return nil
	}
	if node.Type == xhtml.ElementNode && strings.EqualFold(node.Data, tag) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if matched := findFirstElementByTag(child, tag); matched != nil {
			return matched
		}
	}
	return nil
}

func renderNodeAsBBCode(node *xhtml.Node) string {
	if node == nil {
		return ""
	}
	if node.Type == xhtml.TextNode {
		text := stdhtml.UnescapeString(node.Data)
		return text
	}
	if node.Type != xhtml.ElementNode {
		var builder strings.Builder
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			builder.WriteString(renderNodeAsBBCode(child))
		}
		return builder.String()
	}

	tag := strings.ToLower(strings.TrimSpace(node.Data))
	renderChildren := func() string {
		var b strings.Builder
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			b.WriteString(renderNodeAsBBCode(child))
		}
		return b.String()
	}

	switch tag {
	case "br":
		return "\n"
	case "legend":
		// fieldset 标题（如“引用”）是 UI 标签，不属于简介正文。
		return ""
	case "fieldset":
		content := strings.TrimSpace(renderChildren())
		if content == "" {
			return ""
		}
		return "[quote]" + content + "[/quote]\n\n"
	case "p", "div", "section", "article":
		content := strings.TrimSpace(renderChildren())
		if content == "" {
			return "\n"
		}
		return content + "\n\n"
	case "li":
		content := strings.TrimSpace(renderChildren())
		if content == "" {
			return ""
		}
		return "- " + content + "\n"
	case "img":
		src := strings.TrimSpace(getAttr(node, "src"))
		if src == "" {
			return ""
		}
		return "[img]" + src + "[/img]"
	case "a":
		href := strings.TrimSpace(getAttr(node, "href"))
		content := strings.TrimSpace(renderChildren())
		if href == "" {
			return content
		}
		if content == "" {
			content = href
		}
		return "[url=" + href + "]" + content + "[/url]"
	case "strong", "b":
		content := strings.TrimSpace(renderChildren())
		if content == "" {
			return ""
		}
		return "[b]" + content + "[/b]"
	case "em", "i":
		content := strings.TrimSpace(renderChildren())
		if content == "" {
			return ""
		}
		return "[i]" + content + "[/i]"
	case "u":
		content := strings.TrimSpace(renderChildren())
		if content == "" {
			return ""
		}
		return "[u]" + content + "[/u]"
	case "blockquote", "pre", "code":
		content := strings.TrimSpace(renderChildren())
		if content == "" {
			return ""
		}
		return "[quote]" + content + "[/quote]\n\n"
	case "span", "font":
		content := renderChildren()
		if strings.TrimSpace(content) == "" {
			return ""
		}
		style := getAttr(node, "style")

		color := normalizeCSSColorToBBCode(styleValue(style, "color"))
		if color == "" {
			color = normalizeCSSColorToBBCode(getAttr(node, "color"))
		}

		size := ""
		if rawSize := strings.TrimSpace(getAttr(node, "size")); rawSize != "" {
			if _, err := strconv.Atoi(rawSize); err == nil {
				size = rawSize
			}
		}
		if size == "" {
			size = extractFontSizeBBCode(styleValue(style, "font-size"))
		}

		fontWeight := strings.ToLower(strings.TrimSpace(styleValue(style, "font-weight")))
		isBold := fontWeight == "bold"
		if !isBold && fontWeight != "" {
			if n, err := strconv.Atoi(fontWeight); err == nil && n >= 600 {
				isBold = true
			}
		}
		isItalic := strings.EqualFold(strings.TrimSpace(styleValue(style, "font-style")), "italic")
		textDecoration := strings.ToLower(strings.TrimSpace(styleValue(style, "text-decoration")))
		isUnderline := strings.Contains(textDecoration, "underline")

		if color == "" && size == "" && !isBold && !isItalic && !isUnderline {
			return content
		}

		inner := strings.TrimSpace(content)
		wrapped := inner
		if color != "" {
			wrapped = "[color=" + color + "]" + wrapped + "[/color]"
		}
		if size != "" {
			wrapped = "[size=" + size + "]" + wrapped + "[/size]"
		}
		if isUnderline {
			wrapped = "[u]" + wrapped + "[/u]"
		}
		if isItalic {
			wrapped = "[i]" + wrapped + "[/i]"
		}
		if isBold {
			wrapped = "[b]" + wrapped + "[/b]"
		}
		return wrapped
	default:
		return renderChildren()
	}
}

func getAttr(node *xhtml.Node, key string) string {
	if node == nil {
		return ""
	}
	for _, attr := range node.Attr {
		if strings.EqualFold(strings.TrimSpace(attr.Key), strings.TrimSpace(key)) {
			return attr.Val
		}
	}
	return ""
}

func sanitizeBBCodeText(text string) string {
	clean := strings.ReplaceAll(text, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")
	clean = reLeadingBullets.ReplaceAllString(clean, "- ")
	lines := strings.Split(clean, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		// 说明：简介正文里常见 &nbsp; 用于首行缩进（如演员表），需要保留。
		// 因此这里不做全量 TrimSpace（会把 \u00a0 也当作空白去掉），而是：
		// - 将“纯空白行”视为真正空行
		// - 对非空行保留行首缩进（包含 \u00a0），仅压缩正文部分多余空格
		line = strings.TrimRight(line, " \t\f\v")
		if strings.TrimSpace(strings.ReplaceAll(line, "\u00a0", " ")) == "" {
			out = append(out, "")
			continue
		}

		prefixEnd := 0
		for i, r := range line {
			if unicode.IsSpace(r) {
				prefixEnd = i + utf8.RuneLen(r)
				continue
			}
			break
		}
		prefix := line[:prefixEnd]
		body := strings.TrimSpace(line[prefixEnd:])
		body = reMultiSpace.ReplaceAllString(body, " ")

		// 将行首 ASCII 空白提升为 NBSP，避免前端预览（HTML）把缩进折叠掉；
		// 全角空格等非 ASCII 空白保留原宽度，便于对齐（例如演员表）。
		if prefix != "" {
			var b strings.Builder
			b.Grow(len(prefix))
			for _, r := range prefix {
				switch r {
				case ' ', '\t', '\f', '\v':
					b.WriteRune('\u00a0')
				default:
					b.WriteRune(r)
				}
			}
			prefix = b.String()
		}
		out = append(out, prefix+body)
	}
	joined := strings.Join(out, "\n")
	joined = reManyNewlines.ReplaceAllString(joined, "\n\n")
	joined = strings.ReplaceAll(joined, "[quote]\n", "[quote]")
	joined = strings.ReplaceAll(joined, "\n[/quote]", "[/quote]")
	return strings.TrimSpace(joined)
}

func sanitizeBBCodeTextPreserveSpacing(text string) string {
	clean := strings.ReplaceAll(text, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")
	clean = reBBCodeTag.ReplaceAllString(clean, "")
	clean = stdhtml.UnescapeString(clean)
	clean = strings.ReplaceAll(clean, "\u00a0", " ")

	lines := strings.Split(clean, "\n")
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
	joined = reManyNewlines.ReplaceAllString(joined, "\n\n")
	return joined
}

func detectOfficialStatement(bbcode string) (string, []string) {
	trimmed := strings.TrimSpace(bbcode)
	if trimmed == "" {
		return "", nil
	}

	officialKeywords := []string{
		"禁转", "限转", "谢绝转载", "严禁转载", "禁止转载", "官组", "官方发布", "本站首发",
		"原盘来自", "字幕来自", "转载务必", "保留原作者", "如有侵权", "联系删除", "在此感谢", "感谢各位",
	}
	statementTags := make([]string, 0, 3)
	addTag := func(tag string) {
		if tag == "" {
			return
		}
		if !containsString(statementTags, tag) {
			statementTags = append(statementTags, tag)
		}
	}

	matches := reQuoteBlock.FindAllStringSubmatch(trimmed, 3)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		quoteBody := strings.TrimSpace(match[1])
		if quoteBody == "" {
			continue
		}

		cleanQuote := strings.TrimSpace(sanitizeBBCodeText(quoteBody))
		cleanQuote = strings.TrimSpace(removeQuoteMarkerFromText(cleanQuote))
		if cleanQuote == "" {
			continue
		}
		if isLikelyMediaInfoText(cleanQuote) || isLikelyBDInfoText(cleanQuote) {
			continue
		}

		// 仅当 quote 块看起来像声明文本时才检测标签
		if looksLikeDeclarationText(cleanQuote) {
			if strings.Contains(cleanQuote, "禁转") {
				addTag("tag.禁转")
			}
			if strings.Contains(cleanQuote, "限转") {
				addTag("tag.限转")
			}
			if strings.Contains(cleanQuote, "分集") {
				addTag("tag.分集")
			}
			return "[quote]" + cleanQuote + "[/quote]", statementTags
		}
		for _, kw := range officialKeywords {
			if strings.Contains(cleanQuote, kw) {
				if strings.Contains(cleanQuote, "禁转") {
					addTag("tag.禁转")
				}
				if strings.Contains(cleanQuote, "限转") {
					addTag("tag.限转")
				}
				if strings.Contains(cleanQuote, "分集") {
					addTag("tag.分集")
				}
				return "[quote]" + cleanQuote + "[/quote]", statementTags
			}
		}
	}

	header := trimmed
	if len([]rune(header)) > 220 {
		r := []rune(header)
		header = string(r[:220])
	}
	for _, kw := range officialKeywords {
		cleanHeader := strings.TrimSpace(sanitizeBBCodeText(header))
		cleanHeader = strings.TrimSpace(removeQuoteMarkerFromText(cleanHeader))
		if cleanHeader == "" {
			continue
		}
		if looksLikeDeclarationText(cleanHeader) || strings.Contains(cleanHeader, kw) {
			return "[quote]" + cleanHeader + "[/quote]", statementTags
		}
	}
	return "", statementTags
}

func splitPosterAndScreenshots(bbcode string) (string, string) {
	images := extractImageURLsFromText(bbcode)
	images = filterUnwantedImageURLs(images)
	if len(images) == 0 {
		return "", ""
	}

	posterURL := images[0]
	for _, img := range images {
		lower := strings.ToLower(img)
		if strings.Contains(lower, "doubanio") || strings.Contains(lower, "tmdb") || strings.Contains(lower, "poster") {
			posterURL = img
			break
		}
	}

	screens := make([]string, 0, len(images)-1)
	for _, img := range images {
		if img == posterURL {
			continue
		}
		screens = appendUniqueString(screens, img)
	}

	poster := ""
	if posterURL != "" {
		poster = "[img]" + posterURL + "[/img]"
	}
	return poster, toBBCodeImages(screens)
}

func extractMediaInfoFromDetail(descrHTML, descrBBCode string) string {
	preCandidates := make([]string, 0, 4)
	for _, match := range rePreBlock.FindAllStringSubmatch(descrHTML, -1) {
		if len(match) < 2 {
			continue
		}
		clean := sanitizeHTMLPreText(match[1], true)
		if clean != "" {
			preCandidates = append(preCandidates, clean)
		}
	}
	quoteCandidates := make([]string, 0, 4)
	for _, match := range reQuoteBlock.FindAllStringSubmatch(descrBBCode, -1) {
		if len(match) < 2 {
			continue
		}
		clean := strings.TrimSpace(sanitizeBBCodeTextPreserveSpacing(match[1]))
		if clean != "" {
			quoteCandidates = append(quoteCandidates, clean)
		}
	}

	// 优先从 <pre> 块提取 MediaInfo，避免被 quote 中的 BDInfo 抢占。
	if picked := pickMediaInfoCandidate(preCandidates); picked != "" {
		return picked
	}
	if picked := pickMediaInfoCandidate(quoteCandidates); picked != "" {
		return picked
	}

	// 找不到 MediaInfo 时再回退 BDInfo。
	if picked := pickBDInfoCandidate(preCandidates); picked != "" {
		return picked
	}
	if picked := pickBDInfoCandidate(quoteCandidates); picked != "" {
		return picked
	}
	return ""
}

// extractMediaInfoFallbackFromPage 在简介区域未提取到媒体文本时，从整页 HTML 中回退扫描 MediaInfo/BDInfo。
// 参数/返回：入参为完整详情页 HTML；返回命中的 MediaInfo/BDInfo 文本，若无则返回空字符串。
// 失败场景：无（内部按空值兜底）。
// 副作用：无。
func extractMediaInfoFallbackFromPage(pageHTML string) string {
	page := strings.TrimSpace(pageHTML)
	if page == "" {
		return ""
	}

	candidates := make([]string, 0, 12)

	// 优先收集 <pre> 块：大部分站点会把原始 MediaInfo/BDInfo 放在 <pre> 内。
	for _, match := range rePreBlock.FindAllStringSubmatch(page, -1) {
		if len(match) < 2 {
			continue
		}
		clean := sanitizeHTMLPreText(match[1], true)
		if clean != "" {
			candidates = append(candidates, clean)
		}
	}

	// 补充收集 codemain 内容：适配部分站点不直接暴露 <pre> 的场景。
	for _, item := range extractRegexCandidates(page, reMediaInfoCodeMain) {
		clean := sanitizeHTMLPreText(item, true)
		if clean != "" {
			candidates = append(candidates, clean)
		}
	}

	if picked := pickMediaInfoCandidate(candidates); picked != "" {
		return picked
	}
	if picked := pickBDInfoCandidate(candidates); picked != "" {
		return picked
	}
	return ""
}

func pickMediaInfoCandidate(candidates []string) string {
	mediaInfoCandidates := make([]string, 0, len(candidates))
	for _, item := range candidates {
		trimmed := trimMediaInfoLeadingNoise(item)
		if isLikelyMediaInfoText(trimmed) {
			mediaInfoCandidates = append(mediaInfoCandidates, trimmed)
		}
	}
	if picked := pickLongestCandidate(mediaInfoCandidates, 40); picked != "" {
		return picked
	}
	return ""
}

func trimMediaInfoLeadingNoise(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}

	// 青蛙等站点会把“简化摘要 + 原始 MediaInfo”塞进同一个 <pre>，
	// 这里优先从 General 区段起裁剪，避免摘要污染最终结果。
	if loc := reMediaInfoGeneralAnchor.FindStringIndex(trimmed); len(loc) == 2 && loc[0] >= 0 {
		cut := strings.TrimSpace(trimmed[loc[0]:])
		if cut != "" {
			return cut
		}
	}

	if loc := reMediaInfoSectionStartLine.FindStringIndex(trimmed); len(loc) == 2 && loc[0] > 0 {
		cut := strings.TrimSpace(trimmed[loc[0]:])
		if cut != "" {
			return cut
		}
	}
	return trimmed
}

func pickBDInfoCandidate(candidates []string) string {
	bdinfoCandidates := make([]string, 0, len(candidates))
	for _, item := range candidates {
		trimmed := trimBDInfoLeadingNoise(item)
		if isLikelyBDInfoText(trimmed) {
			bdinfoCandidates = append(bdinfoCandidates, trimmed)
		}
	}
	return pickLongestCandidate(bdinfoCandidates, 40)
}

func trimBDInfoLeadingNoise(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}

	// 与 MediaInfo 相同，优先从 BDInfo 正文锚点截取，去掉前置“摘要文本”污染。
	if loc := reBDInfoDiscAnchor.FindStringIndex(trimmed); len(loc) == 2 && loc[0] >= 0 {
		cut := strings.TrimSpace(trimmed[loc[0]:])
		if cut != "" {
			return cut
		}
	}

	if loc := reBDInfoSectionStartLine.FindStringIndex(trimmed); len(loc) == 2 && loc[0] > 0 {
		cut := strings.TrimSpace(trimmed[loc[0]:])
		if cut != "" {
			return cut
		}
	}
	return trimmed
}

func isLikelyMediaInfoText(text string) bool {
	upper := strings.ToUpper(strings.TrimSpace(text))
	if upper == "" {
		return false
	}
	if isLikelyBDInfoText(upper) {
		return false
	}

	strong := 0
	for _, marker := range []string{"GENERAL", "VIDEO", "AUDIO", "COMPLETE NAME", "DURATION", "FILE SIZE"} {
		if strings.Contains(upper, marker) {
			strong++
		}
	}
	if strong >= 2 {
		return true
	}

	extra := []string{"MEDIAINFO", "FORMAT", "BIT RATE", "LANGUAGE", "UNIQUE ID", "OVERALL BIT RATE"}
	hits := 0
	for _, marker := range extra {
		if strings.Contains(upper, marker) {
			hits++
		}
	}
	if hits >= 2 && (strings.Contains(upper, "VIDEO") || strings.Contains(upper, "AUDIO")) {
		return true
	}
	if strings.Contains(upper, "COMPLETE NAME") && strings.Contains(upper, "FORMAT") {
		return true
	}
	if strings.Contains(upper, ".RELEASE.INFO") && strings.Contains(upper, "ENCODER") {
		return true
	}
	return false
}

func isLikelyBDInfoText(text string) bool {
	upper := strings.ToUpper(strings.TrimSpace(text))
	if upper == "" {
		return false
	}
	markers := []string{"DISC INFO", "PLAYLIST REPORT", "QUICK SUMMARY", "FILES:", "CHAPTERS:", "DISC SIZE"}
	for _, marker := range markers {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

func pickLongestCandidate(candidates []string, minLen int) string {
	best := ""
	bestLen := -1
	for _, item := range candidates {
		length := len([]rune(strings.TrimSpace(item)))
		if length < minLen {
			continue
		}
		if length > bestLen {
			best = item
			bestLen = length
		}
	}
	return strings.TrimSpace(best)
}

func buildBodyBBCode(descr, statement, poster, screens string) string {
	body := strings.TrimSpace(descr)
	if body == "" {
		return ""
	}

	// 先按规则剔除声明类 quote，避免声明和正文混在一起。
	body = stripDeclarationBlocks(body, statement)

	if statement != "" {
		body = strings.Replace(body, statement, "", 1)
	}
	if poster != "" {
		body = strings.Replace(body, poster, "", 1)
	}
	if screens != "" {
		for _, line := range strings.Split(screens, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			body = strings.ReplaceAll(body, line, "")
		}
	}
	body = reBBCodeTag.ReplaceAllStringFunc(body, func(tag string) string {
		lower := strings.ToLower(tag)
		if strings.HasPrefix(lower, "[url") || strings.HasPrefix(lower, "[/url") || strings.HasPrefix(lower, "[b") || strings.HasPrefix(lower, "[/b") || strings.HasPrefix(lower, "[i") || strings.HasPrefix(lower, "[/i") || strings.HasPrefix(lower, "[u") || strings.HasPrefix(lower, "[/u") || strings.HasPrefix(lower, "[quote") || strings.HasPrefix(lower, "[/quote") {
			return tag
		}
		return ""
	})
	body = stripDeclarationLines(body)
	body = removeStatementResidueFromBody(body, statement)
	return sanitizeBBCodeText(body)
}

func stripDeclarationBlocks(text, statement string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}

	statementNorm := normalizeForStatementCompare(statement)
	return reQuoteBlock.ReplaceAllStringFunc(trimmed, func(full string) string {
		match := reQuoteBlock.FindStringSubmatch(full)
		inner := ""
		if len(match) >= 2 {
			inner = match[1]
		}
		innerNorm := normalizeForStatementCompare(inner)
		fullNorm := normalizeForStatementCompare(full)

		if statementNorm != "" {
			if strings.Contains(fullNorm, statementNorm) || strings.Contains(statementNorm, innerNorm) || strings.Contains(innerNorm, statementNorm) {
				return ""
			}
		}
		if looksLikeDeclarationText(inner) {
			return ""
		}
		return full
	})
}

func stripDeclarationLines(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		plain := strings.TrimSpace(reBBCodeTag.ReplaceAllString(line, ""))
		if isQuoteMarkerLine(plain) {
			continue
		}
		if plain != "" && looksLikeDeclarationText(plain) && len([]rune(plain)) <= 220 {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func looksLikeDeclarationText(text string) bool {
	plain := strings.TrimSpace(reBBCodeTag.ReplaceAllString(text, ""))
	if plain == "" {
		return false
	}
	keywords := []string{
		"禁转", "限转", "谢绝转载", "严禁转载", "禁止转载", "官组", "官方发布", "本站首发",
		"ARDTU", "自动发布", "郑重声明", "宽带测试", "用户自行搜集", "网上搜集", "商业盈利活动",
		"原盘来自", "字幕来自", "转载务必", "保留原作者", "如有侵权", "联系删除", "在此感谢", "感谢各位",
	}
	for _, kw := range keywords {
		if strings.Contains(plain, kw) {
			return true
		}
	}
	return false
}

func normalizeForStatementCompare(text string) string {
	clean := strings.TrimSpace(reBBCodeTag.ReplaceAllString(text, ""))
	if clean == "" {
		return ""
	}
	clean = strings.ToLower(clean)
	clean = strings.NewReplacer(
		"，", " ", "。", " ", "！", " ", "？", " ", "：", " ", "；", " ", "、", " ",
		",", " ", ".", " ", "!", " ", "?", " ", ":", " ", ";", " ", "|", " ",
		"[", " ", "]", " ", "(", " ", ")", " ", "{", " ", "}", " ",
		"【", " ", "】", " ", "<", " ", ">", " ", "\"", " ", "'", " ", "`", " ",
		"-", " ", "_", " ", "/", " ", "\\", " ",
	).Replace(clean)
	return strings.Join(strings.Fields(clean), "")
}

func removeStatementResidueFromBody(body, statement string) string {
	trimmedBody := strings.TrimSpace(body)
	if trimmedBody == "" || strings.TrimSpace(statement) == "" {
		return trimmedBody
	}

	// 直接清理整段声明和去标签后的声明内容。
	plainStatement := strings.TrimSpace(reBBCodeTag.ReplaceAllString(statement, ""))
	for _, candidate := range []string{strings.TrimSpace(statement), plainStatement} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		trimmedBody = strings.ReplaceAll(trimmedBody, candidate, "")
	}

	statementLines := collectComparableStatementLines(plainStatement)
	if len(statementLines) == 0 {
		return strings.TrimSpace(trimmedBody)
	}

	lines := strings.Split(trimmedBody, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		plainLine := strings.TrimSpace(reBBCodeTag.ReplaceAllString(line, ""))
		if isQuoteMarkerLine(plainLine) {
			continue
		}
		lineNorm := normalizeForStatementCompare(line)
		if lineNorm == "" {
			kept = append(kept, line)
			continue
		}
		shouldDrop := false
		for _, stmtLine := range statementLines {
			if len([]rune(stmtLine)) < 3 {
				continue
			}
			if lineNorm == stmtLine || strings.Contains(lineNorm, stmtLine) || strings.Contains(stmtLine, lineNorm) {
				shouldDrop = true
				break
			}
			if lineSimilarByTokens(lineNorm, stmtLine) {
				shouldDrop = true
				break
			}
		}
		if !shouldDrop {
			kept = append(kept, line)
		}
	}

	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func collectComparableStatementLines(statement string) []string {
	lines := strings.Split(strings.TrimSpace(statement), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		plainLine := strings.TrimSpace(reBBCodeTag.ReplaceAllString(line, ""))
		if isQuoteMarkerLine(plainLine) {
			continue
		}
		norm := normalizeForStatementCompare(line)
		if norm == "" {
			continue
		}
		result = append(result, norm)
	}
	return result
}

func removeQuoteMarkerFromText(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		raw := strings.TrimSpace(line)
		if raw == "" {
			kept = append(kept, "")
			continue
		}
		plain := strings.TrimSpace(reBBCodeTag.ReplaceAllString(raw, ""))
		plain = strings.TrimSpace(reQuotePrefix.ReplaceAllString(plain, ""))
		if isQuoteMarkerLine(plain) {
			continue
		}
		if strings.TrimSpace(plain) == "" {
			continue
		}
		kept = append(kept, raw)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func isQuoteMarkerLine(text string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	if trimmed == "" {
		return false
	}
	switch trimmed {
	case "引用", "引用:", "引用：", "[引用]", "quote", "quote:", "quote：", "[quote]":
		return true
	default:
		return false
	}
}

func lineSimilarByTokens(lineNorm, stmtNorm string) bool {
	lineTokens := splitCompactTokens(lineNorm)
	stmtTokens := splitCompactTokens(stmtNorm)
	if len(lineTokens) == 0 || len(stmtTokens) == 0 {
		return false
	}
	matches := 0
	for _, token := range lineTokens {
		if len([]rune(token)) < 2 {
			continue
		}
		for _, candidate := range stmtTokens {
			if token == candidate {
				matches++
				break
			}
		}
	}
	return matches >= 2
}

func splitCompactTokens(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n'
	})
	if len(parts) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func inferStandardizedValues(title, mediainfo, body string) map[string]string {
	upperTech := strings.ToUpper(strings.TrimSpace(title + "\n" + mediainfo))
	upperAll := strings.ToUpper(strings.TrimSpace(title + "\n" + mediainfo + "\n" + body))
	values := map[string]string{
		"type":        "category.movie",
		"medium":      "medium.other",
		"video_codec": "video.other",
		"audio_codec": "audio.other",
		"resolution":  "resolution.other",
		"team":        inferTeamKey(title),
		"source":      "source.other",
		"tags_array":  "",
	}

	normalizedTitleForAudio := normalizeAudioCodecTokensForInference(title)
	normalizedUpperTechForAudio := strings.ToUpper(normalizeAudioCodecTokensForInference(title + "\n" + mediainfo))

	inferAudioCodec := func(upperText string) string {
		upper := strings.ToUpper(strings.TrimSpace(upperText))
		if upper == "" {
			return ""
		}
		switch {
		case strings.Contains(upper, "TRUEHD"):
			if strings.Contains(upper, "ATMOS") {
				return "audio.truehd_atmos"
			}
			return "audio.truehd"
		case strings.Contains(upper, "DTS:X") || strings.Contains(upper, "DTS X"):
			return "audio.dtsx"
		case strings.Contains(upper, "DTS-HD MA") || strings.Contains(upper, "DTS HD MA"):
			return "audio.dts_hd_ma"
		case strings.Contains(upper, "DTS"):
			return "audio.dts"
		case strings.Contains(upper, "E-AC-3") || strings.Contains(upper, "DDP") || strings.Contains(upper, "DD+"):
			return "audio.ddp"
		case strings.Contains(upper, "AC-3") || strings.Contains(upper, "AC3"):
			return "audio.ac3"
		case strings.Contains(upper, "FLAC"):
			return "audio.flac"
		case strings.Contains(upper, "AAC"):
			return "audio.aac"
		case strings.Contains(upper, "MP3"):
			return "audio.mp3"
		default:
			return ""
		}
	}

	hasTech := func(parts ...string) bool {
		for _, part := range parts {
			if part != "" && strings.Contains(upperTech, strings.ToUpper(part)) {
				return true
			}
		}
		return false
	}
	hasAll := func(parts ...string) bool {
		for _, part := range parts {
			if part != "" && strings.Contains(upperAll, strings.ToUpper(part)) {
				return true
			}
		}
		return false
	}

	if hasTech("SEASON", "S01", "S02", "EP", "E01", "剧集") {
		values["type"] = "category.tv_series"
	} else if hasTech("ANIME", "动画") {
		values["type"] = "category.animation"
	} else if hasTech("DOCUMENTARY", "纪录片") {
		values["type"] = "category.documentaries"
	} else if hasTech("MUSIC", "演唱会", "音乐") {
		values["type"] = "category.music"
	}

	switch {
	case hasTech("REMUX"):
		values["medium"] = "medium.remux"
	case hasTech("UHD", "ULTRA HD"):
		values["medium"] = "medium.uhd_bluray"
	case hasTech("BLURAY", "BLU-RAY", "BDMV", "BD25", "BD50", "BD66", "BD100"):
		values["medium"] = "medium.bluray"
	case hasTech("BDRIP", "BD-RIP", "BD RIP"):
		values["medium"] = "medium.bdrip"
	case hasTech("WEB-DL", "WEBDL"):
		values["medium"] = "medium.webdl"
	case hasTech("WEBRIP"):
		values["medium"] = "medium.webrip"
	case hasTech("HDTV"):
		values["medium"] = "medium.hdtv"
	case hasTech("DVD", "DVDRIP"):
		values["medium"] = "medium.dvd"
	}

	switch {
	case hasTech("AV1"):
		values["video_codec"] = "video.av1"
	case hasTech("X265"):
		values["video_codec"] = "video.x265"
	case hasTech("H.265", "H265", "HEVC"):
		values["video_codec"] = "video.h265"
	case hasTech("X264", "H.264", "H264", "AVC"):
		values["video_codec"] = "video.h264"
	case hasTech("VC-1", "VC1"):
		values["video_codec"] = "video.vc1"
	case hasTech("MPEG-2"):
		values["video_codec"] = "video.mpeg2"
	}

	// 对齐 Python：音频编码优先使用标题提取结果，避免 MediaInfo/BDInfo 的多音轨信息覆盖标题音频编码。
	// 示例：标题为 DTS-HD MA 5.1，但 BDInfo 同时包含 TrueHD/DTS 两条音轨时，应优先 DTS-HD MA。
	if fromTitle := inferAudioCodec(normalizedTitleForAudio); fromTitle != "" {
		values["audio_codec"] = fromTitle
	} else if fromTech := inferAudioCodec(normalizedUpperTechForAudio); fromTech != "" {
		values["audio_codec"] = fromTech
	}

	switch {
	case true:
		// 对齐 Python：优先使用 MediaInfo 的 Width/Height 推断分辨率，避免 "k" 子串误判 8K。
		if fromMediaInfo := inferResolutionFromMediainfo(mediainfo); fromMediaInfo != "" {
			values["resolution"] = fromMediaInfo
			break
		}
		if fromToken := inferResolutionFromTokens(title + "\n" + mediainfo); fromToken != "" {
			values["resolution"] = fromToken
		}
	}

	switch {
	// 产地推断只接受“明确国家/地区”字样，不再使用“国语/国配/CHN”等音轨语言提示，避免误判日本片为中国。
	case hasAll("中国", "CHINA"):
		values["source"] = "source.china"
	case hasAll("香港", "HKG"):
		values["source"] = "source.hongkong"
	case hasAll("台湾", "TWN"):
		values["source"] = "source.taiwan"
	case hasAll("日本", "日语", "JAPAN", "JPN"):
		values["source"] = "source.japan"
	case hasAll("韩国", "韩语", "KOREA", "KOR"):
		values["source"] = "source.korea"
	case hasAll("英国", "UK"):
		values["source"] = "source.uk"
	case hasAll("美国", "USA", "ENGLISH", "US "):
		values["source"] = "source.western"
	}

	// 对齐 Python：标签补全不在这里基于“正文/全页文本”做推断，避免噪声误判。
	values["tags_array"] = ""
	return values
}

// inferResolutionFromMediainfo 从 MediaInfo 文本提取 Width/Height 并映射标准分辨率键。
// 规则与 Python 版 extract_resolution_from_mediainfo 对齐：先解析 Video 区块中的宽高，不命中再尝试 "W/H" 与 "WxH"。
func inferResolutionFromMediainfo(mediainfo string) string {
	trimmed := strings.TrimSpace(mediainfo)
	if trimmed == "" {
		return ""
	}

	videoSection := ""
	if m := reVideoSection.FindString(trimmed); strings.TrimSpace(m) != "" {
		videoSection = m
	} else {
		videoSection = trimmed
	}

	width, height := 0, 0
	if w, h := parseWidthHeightFromPixels(videoSection); w > 0 && h > 0 {
		width, height = w, h
	}
	if (width == 0 || height == 0) && strings.TrimSpace(videoSection) != "" {
		if m := reResolutionSlash.FindStringSubmatch(videoSection); len(m) >= 3 {
			if w, err := strconv.Atoi(strings.TrimSpace(m[1])); err == nil {
				width = w
			}
			if h, err := strconv.Atoi(strings.TrimSpace(m[2])); err == nil {
				height = h
			}
		}
	}
	if (width == 0 || height == 0) && strings.TrimSpace(trimmed) != "" {
		if m := reResolutionX.FindStringSubmatch(trimmed); len(m) >= 3 {
			if w, err := strconv.Atoi(strings.TrimSpace(m[1])); err == nil {
				width = w
			}
			if h, err := strconv.Atoi(strings.TrimSpace(m[2])); err == nil {
				height = h
			}
		}
	}

	if width <= 0 || height <= 0 {
		return ""
	}

	switch {
	case height <= 480:
		return "resolution.r480p"
	case height <= 576:
		return "resolution.r576p"
	case height <= 720:
		return "resolution.r720p"
	case height <= 1080:
		return "resolution.r1080p"
	case height <= 1440:
		return "resolution.r1440p"
	case height <= 2160:
		return "resolution.r2160p"
	case height <= 4320:
		return "resolution.r8k"
	default:
		return "resolution.other"
	}
}

// parseWidthHeightFromPixels 解析 "Width: 1 920 pixels / Height: 1 080 pixels" 形式的宽高。
func parseWidthHeightFromPixels(section string) (int, int) {
	width := 0
	height := 0

	if m := reWidthPixels.FindStringSubmatch(section); len(m) >= 3 {
		raw := strings.TrimSpace(m[1] + m[2])
		raw = strings.ReplaceAll(raw, " ", "")
		if v, err := strconv.Atoi(raw); err == nil {
			width = v
		}
	}
	if m := reHeightPixels.FindStringSubmatch(section); len(m) >= 3 {
		raw := strings.TrimSpace(m[1] + m[2])
		raw = strings.ReplaceAll(raw, " ", "")
		if v, err := strconv.Atoi(raw); err == nil {
			height = v
		}
	}
	return width, height
}

// inferResolutionFromTokens 从标题/媒体文本里的标准 token 兜底提取分辨率。
// 使用词边界正则，避免 "23800 kb/s" 这类片段误命中 "8k"。
func inferResolutionFromTokens(text string) string {
	match := strings.TrimSpace(reResolutionToken.FindString(text))
	if match == "" {
		return ""
	}
	switch strings.ToUpper(match) {
	case "8K", "4320P":
		return "resolution.r8k"
	case "4K", "2160P":
		return "resolution.r2160p"
	case "1080I":
		return "resolution.r1080i"
	case "1080P":
		return "resolution.r1080p"
	case "720P":
		return "resolution.r720p"
	case "480P":
		return "resolution.r480p"
	default:
		return ""
	}
}

func inferTeamKey(title string) string {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return "team.other"
	}
	if idx := strings.LastIndex(trimmed, "-"); idx > 0 && idx < len(trimmed)-1 {
		rawTeam := strings.TrimSpace(trimmed[idx+1:])
		if rawTeam != "" {
			return NormalizeTeamKey(rawTeam)
		}
	}
	return "team.other"
}

func normalizeTeamLabel(raw string) string {
	team := strings.TrimSpace(raw)
	if team == "" {
		return ""
	}
	team = strings.Trim(team, "[](){}<> ")
	team = strings.TrimLeft(team, "-@")

	// 与 Python 的标准化阶段对齐：优先使用 @ 后半段做映射输入。
	if strings.Contains(team, "@") {
		parts := strings.Split(team, "@")
		for idx := len(parts) - 1; idx >= 0; idx-- {
			part := strings.TrimSpace(parts[idx])
			part = strings.Trim(part, "[](){}<> ")
			part = strings.TrimLeft(part, "-@")
			if part != "" {
				team = part
				break
			}
		}
	}

	team = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(team, "team."), "TEAM."))
	team = strings.Trim(team, "._- ")
	return strings.TrimSpace(team)
}

func normalizeTeamKey(value string) string {
	// 兼容旧调用点：制作组键统一由 NormalizeTeamKey 负责，未命中一律返回 team.other。
	return NormalizeTeamKey(value)
}

func mergeAndNormalizeTags(tagCSV string, extra []string) []string {
	result := []string{}
	for _, item := range strings.Split(strings.TrimSpace(tagCSV), ",") {
		item = normalizeTag(item)
		if item == "" {
			continue
		}
		result = appendUniqueString(result, item)
	}
	for _, item := range extra {
		item = normalizeTag(item)
		if item == "" {
			continue
		}
		result = appendUniqueString(result, item)
	}
	return result
}

func normalizeTag(tag string) string {
	trimmed := strings.TrimSpace(tag)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "tag.") {
		return "tag." + strings.TrimPrefix(strings.TrimPrefix(trimmed, "tag."), "TAG.")
	}
	if strings.EqualFold(trimmed, "DIY") {
		return "tag.DIY"
	}
	if strings.Contains(trimmed, "中字") {
		return "tag.中字"
	}
	if strings.Contains(trimmed, "HDR") {
		return "tag.HDR"
	}
	if strings.Contains(trimmed, "禁转") {
		return "tag.禁转"
	}
	if strings.Contains(trimmed, "限转") {
		return "tag.限转"
	}
	if strings.Contains(trimmed, "分集") {
		return "tag.分集"
	}
	return "tag." + trimmed
}

func containsString(items []string, item string) bool {
	for _, existing := range items {
		if existing == item {
			return true
		}
	}
	return false
}
