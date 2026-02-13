package extract

import (
	"regexp"
	"strings"
)

type ReviewExtractedData = reviewExtractedData

func ExtractReviewDataFromHTML(pageHTML, fallbackTitle string) ReviewExtractedData {
	return extractReviewDataFromHTML(pageHTML, fallbackTitle)
}

func ExtractDescriptionSections(descrHTML, descrBBCode, extraStatementBBCode string) (string, string, string, string, string, []string, []string) {
	return extractDescriptionSections(descrHTML, descrBBCode, extraStatementBBCode)
}

func ExtractExtraTextBBCode(pageHTML string) string {
	return extractExtraTextBBCode(pageHTML)
}

func BuildStatementFromExtraBBCode(extraStatementBBCode string) string {
	return buildStatementFromExtraBBCode(extraStatementBBCode)
}

func ExtractSubtitle(page string) string {
	return extractSubtitle(page)
}

func ExtractElementInnerHTMLByID(pageHTML, tagName, elementID string) string {
	return extractElementInnerHTMLByID(pageHTML, tagName, elementID)
}

func HTMLToBBCode(fragment string) string {
	return htmlToBBCode(fragment)
}

func ExtractMediaInfoFromDetail(descrHTML, descrBBCode string) string {
	return extractMediaInfoFromDetail(descrHTML, descrBBCode)
}

func NormalizeExternalLink(link string, pattern *regexp.Regexp) string {
	return normalizeExternalLink(link, pattern)
}

func InferStandardizedValues(title, mediainfo, body string) map[string]string {
	return inferStandardizedValues(title, mediainfo, body)
}

func ExtractTeamFromPage(pageHTML string) string {
	return extractTeamFromPage(pageHTML)
}

func NormalizeTeamLabel(raw string) string {
	return normalizeTeamLabel(raw)
}

func ExtractTagsFromPage(page string) []string {
	return extractTagsFromPage(page)
}

func MergeExplicitSourceTags(items []string) []string {
	return mergeExplicitSourceTags(items)
}

func SanitizeHTMLText(input string, keepLineBreak bool) string {
	return sanitizeHTMLText(input, keepLineBreak)
}

func IsLikelyMediaInfoText(text string) bool {
	return isLikelyMediaInfoText(text)
}

func IsLikelyBDInfoText(text string) bool {
	return isLikelyBDInfoText(text)
}

func PickMediaInfoCandidate(candidates []string) string {
	return pickMediaInfoCandidate(candidates)
}

func PickBDInfoCandidate(candidates []string) string {
	return pickBDInfoCandidate(candidates)
}

func NormalizeSourceTagText(raw string) string {
	return normalizeSourceTagText(raw)
}

func ShouldIgnoreSourceTag(tag string) bool {
	return shouldIgnoreSourceTag(tag)
}

func ExtractMediaInfoByRegexes(pageHTML string, patterns []*regexp.Regexp) string {
	return extractMediaInfoByRegexes(pageHTML, patterns)
}

// ExtractRegexCandidates 使用正则提取第一个捕获组候选内容。
func ExtractRegexCandidates(pageHTML string, pattern *regexp.Regexp) []string {
	return extractRegexCandidates(pageHTML, pattern)
}

// ExtractRegexCandidatesAsText 使用正则提取候选内容并做 HTML 文本归一化。
func ExtractRegexCandidatesAsText(pageHTML string, pattern *regexp.Regexp) []string {
	return extractRegexCandidatesAsText(pageHTML, pattern)
}

// NormalizeHTMLBlockText 将 HTML 片段归一化为纯文本并保留换行。
func NormalizeHTMLBlockText(raw string) string {
	return normalizeHTMLBlockText(raw)
}

func ExtractKeepfrdsTitles(pageHTML string, fallbackTitle string) (string, string) {
	return extractKeepfrdsTitles(pageHTML, fallbackTitle)
}

func FetchTagsFromTorrentList(baseURL string, cookie string, mainTitle string, torrentID string) ([]string, error) {
	return fetchTagsFromTorrentList(baseURL, cookie, mainTitle, torrentID)
}

func ReHHClubMediaInfo() *regexp.Regexp {
	return reHHClubMediaInfo
}

func ReMediaInfoCodeMain() *regexp.Regexp {
	return reMediaInfoCodeMain
}

func ReKeepfrdsMediaInfo() *regexp.Regexp {
	return reKeepfrdsMediaInfo
}

func ReDoubanLink() *regexp.Regexp {
	return reDoubanLink
}

func ReIMDbLink() *regexp.Regexp {
	return reIMDbLink
}

func ReTMDbLink() *regexp.Regexp {
	return reTMDbLink
}

func NormalizeNonEmpty(value string) string {
	return strings.TrimSpace(value)
}
