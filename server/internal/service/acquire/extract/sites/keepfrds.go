package sites

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	reKeepfrdsQuoteBlock          = regexp.MustCompile(`(?is)\[quote\](.*?)\[/quote\]`)
	reKeepfrdsAcknowledgmentLine  = regexp.MustCompile(`(?m)^\s*[-－—]{2,}[^\n]*?来自[^\n]*?[-－—]{2,}\s*$`)
	reKeepfrdsManyNewlines        = regexp.MustCompile(`\n{3,}`)
	keepfrdsAcknowledgmentKeyword = []string{"官组", "感谢", "原制作者", "FRDS", "FraMeSToR", "CHD", "字幕组"}
)

type keepfrdsAcknowledgmentSegment struct {
	start int
	end   int
	value string
}

// ExtractKeepfrds 提取月月站点的详情页参数。
// 参数/返回：输入详情页上下文与运行期依赖，返回统一 SeedData。
// 失败场景：公共提取失败、标签列表抓取失败。
// 副作用：可能会发起列表页网络请求（用于补全标签）。
func ExtractKeepfrds(input Input, runtime Runtime) (SeedData, error) {
	if runtime.ExtractWithPublic == nil {
		return SeedData{}, errors.New("缺少公共提取器依赖")
	}
	data, err := runtime.ExtractWithPublic(input)
	if err != nil {
		return data.Normalize(input.FallbackTitle), err
	}

	if runtime.ExtractKeepfrdsTitles != nil {
		mainTitle, subtitle := runtime.ExtractKeepfrdsTitles(input.PageHTML, data.Title)
		if strings.TrimSpace(mainTitle) != "" {
			data.Title = strings.TrimSpace(mainTitle)
		}
		if strings.TrimSpace(subtitle) != "" {
			data.Subtitle = strings.TrimSpace(subtitle)
		}
	}

	if runtime.ExtractMediaInfoByRegexes != nil {
		patterns := make([]*regexp.Regexp, 0, 2)
		if runtime.ReKeepfrdsMediaInfo != nil {
			patterns = append(patterns, runtime.ReKeepfrdsMediaInfo)
		}
		if runtime.ReMediaInfoCodeMain != nil {
			patterns = append(patterns, runtime.ReMediaInfoCodeMain)
		}
		if len(patterns) > 0 {
			if media := runtime.ExtractMediaInfoByRegexes(input.PageHTML, patterns); media != "" {
				data.MediaInfo = media
			}
		}
	}

	extras, cleanedBody := extractKeepfrdsAcknowledgmentsFromBody(data.Intro.Body)
	if len(extras) > 0 {
		data.Intro.Body = cleanedBody
		data.Intro.Statement = insertKeepfrdsExtrasAfterAcknowledgmentQuote(data.Intro.Statement, extras)
	}

	if runtime.FetchTagsFromTorrentList != nil {
		tags, tagErr := runtime.FetchTagsFromTorrentList(input.BaseURL, input.Cookie, data.Title, input.TorrentID)
		if tagErr != nil {
			return data.Normalize(input.FallbackTitle), fmt.Errorf("月月标签提取失败: %w", tagErr)
		}
		if len(tags) > 0 {
			data.Tags = tags
		}
	}

	if runtime.BuildSourceParams != nil {
		data.SourceParams = runtime.BuildSourceParams(data)
	}
	return data.Normalize(input.FallbackTitle), nil
}

func extractKeepfrdsAcknowledgmentsFromBody(body string) ([]string, string) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return nil, body
	}

	segments := make([]keepfrdsAcknowledgmentSegment, 0, 4)
	for _, idx := range reKeepfrdsQuoteBlock.FindAllStringSubmatchIndex(trimmed, -1) {
		if len(idx) < 4 {
			continue
		}
		full := strings.TrimSpace(trimmed[idx[0]:idx[1]])
		if full == "" {
			continue
		}
		if strings.Contains(full, "PT补档") {
			segments = append(segments, keepfrdsAcknowledgmentSegment{start: idx[0], end: idx[1], value: full})
		}
	}

	for _, idx := range reKeepfrdsAcknowledgmentLine.FindAllStringIndex(trimmed, -1) {
		if len(idx) < 2 {
			continue
		}
		line := strings.TrimSpace(trimmed[idx[0]:idx[1]])
		if line == "" {
			continue
		}
		segments = append(segments, keepfrdsAcknowledgmentSegment{start: idx[0], end: idx[1], value: "[quote]" + line + "[/quote]"})
	}

	if len(segments) == 0 {
		return nil, body
	}

	// 按正文出现顺序排列，保证声明追加顺序稳定可控。
	sort.Slice(segments, func(i, j int) bool {
		if segments[i].start == segments[j].start {
			return segments[i].end < segments[j].end
		}
		return segments[i].start < segments[j].start
	})

	extras := make([]string, 0, len(segments))
	for _, seg := range segments {
		extras = append(extras, seg.value)
	}

	// 从后往前剔除匹配片段，避免索引漂移。
	cleaned := trimmed
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if seg.start < 0 || seg.end <= seg.start || seg.end > len(cleaned) {
			continue
		}
		cleaned = cleaned[:seg.start] + cleaned[seg.end:]
	}

	cleaned = strings.TrimSpace(reKeepfrdsManyNewlines.ReplaceAllString(strings.TrimSpace(cleaned), "\n\n"))
	return filterKeepfrdsExtras(extras), cleaned
}

func insertKeepfrdsExtrasAfterAcknowledgmentQuote(statement string, extras []string) string {
	extras = filterKeepfrdsExtras(extras)
	if len(extras) == 0 {
		return statement
	}

	stmt := strings.TrimSpace(statement)
	extraText := strings.TrimSpace(strings.Join(extras, "\n"))
	if extraText == "" {
		return statement
	}
	if stmt == "" {
		return extraText
	}

	filtered := make([]string, 0, len(extras))
	for _, extra := range extras {
		if strings.Contains(stmt, extra) {
			continue
		}
		filtered = append(filtered, extra)
	}
	if len(filtered) == 0 {
		return stmt
	}
	extraText = strings.TrimSpace(strings.Join(filtered, "\n"))

	quoteIdx := reKeepfrdsQuoteBlock.FindAllStringSubmatchIndex(stmt, -1)
	if len(quoteIdx) == 0 {
		return strings.TrimSpace(stmt + "\n" + extraText)
	}

	insertAt := -1
	for _, idx := range quoteIdx {
		if len(idx) < 2 {
			continue
		}
		full := strings.TrimSpace(stmt[idx[0]:idx[1]])
		if isKeepfrdsAcknowledgmentQuote(full) {
			insertAt = idx[1]
		}
	}
	if insertAt < 0 {
		insertAt = len(stmt)
	}

	left := strings.TrimRight(stmt[:insertAt], " \t\r\n")
	right := strings.TrimLeft(stmt[insertAt:], " \t\r\n")
	if right == "" {
		return strings.TrimSpace(left + "\n" + extraText)
	}
	return strings.TrimSpace(left + "\n" + extraText + "\n" + right)
}

func isKeepfrdsAcknowledgmentQuote(quote string) bool {
	trimmed := strings.TrimSpace(quote)
	if trimmed == "" {
		return false
	}
	for _, kw := range keepfrdsAcknowledgmentKeyword {
		if strings.Contains(trimmed, kw) {
			return true
		}
	}
	return false
}

func filterKeepfrdsExtras(extras []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(extras))
	for _, extraRaw := range extras {
		extra := strings.TrimSpace(extraRaw)
		if extra == "" {
			continue
		}
		normalized := strings.TrimSpace(reKeepfrdsManyNewlines.ReplaceAllString(extra, "\n\n"))
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}
