package tagging

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
)

// extractRawTagsFromTitleComponents 从标题拆解组件中提取可用于映射的原始标签（不带 tag. 前缀）。
// 失败场景：组件为空或字段缺失时返回空数组，不影响主流程。
func extractRawTagsFromTitleComponents(components []map[string]any) []string {
	if len(components) == 0 {
		return []string{}
	}

	values := map[string]string{}
	for _, item := range components {
		key := strings.TrimSpace(toStringAny(item["key"], ""))
		if key == "" {
			continue
		}
		value := strings.TrimSpace(toStringAny(item["value"], ""))
		if value == "" {
			continue
		}
		values[key] = value
	}

	tags := make([]string, 0, 8)

	medium := strings.ToUpper(values["媒介"])
	if strings.Contains(medium, "DIY") {
		tags = appendUniqueStringLocal(tags, "DIY")
	}
	if strings.Contains(medium, "REMUX") {
		tags = appendUniqueStringLocal(tags, "Remux")
	}
	if strings.Contains(medium, "WEB-DL") || strings.Contains(medium, "WEBDL") {
		tags = appendUniqueStringLocal(tags, "WEB-DL")
	}

	team := values["制作组"]
	teamUpper := strings.ToUpper(team)
	if strings.Contains(teamUpper, "DIY") {
		tags = appendUniqueStringLocal(tags, "DIY")
	}
	if strings.Contains(team, "VCB-Studio") || strings.Contains(teamUpper, "&VCB-STUDIO") || strings.Contains(teamUpper, "VCB-STUDIO") || strings.Contains(teamUpper, "VCB") {
		tags = appendUniqueStringLocal(tags, "VCB-Studio")
	}

	hdr := strings.TrimSpace(values["HDR格式"])
	if hdr != "" {
		hdrUpper := strings.ToUpper(hdr)
		if strings.Contains(hdrUpper, "DOVI") || strings.Contains(hdrUpper, "DOLBY VISION") || regexp.MustCompile(`(?i)\bDV\b`).MatchString(hdr) {
			tags = appendUniqueStringLocal(tags, "Dolby Vision")
		}
		if strings.Contains(hdrUpper, "HDR10+") {
			tags = appendUniqueStringLocal(tags, "HDR10+")
		}
		if strings.Contains(hdrUpper, "HLG") {
			tags = appendUniqueStringLocal(tags, "HLG")
		}
		if strings.Contains(hdrUpper, "VIVID") || strings.Contains(hdr, "菁彩HDR") {
			tags = appendUniqueStringLocal(tags, "菁彩HDR")
		}
		// HDR10/HDR：当 HDR10+ 不存在时，再补 HDR，后续会由 deduplicateHDRTags 做去重。
		if !strings.Contains(hdrUpper, "HDR10+") && strings.Contains(hdrUpper, "HDR") {
			tags = appendUniqueStringLocal(tags, "HDR")
		}
	}

	// 音频编码：提取 Atmos 标签（对齐 Python：Atmos 可能无空格，如 Atmos7.1）
	audio := strings.TrimSpace(values["音频编码"])
	if audio != "" && reAtmosFromAudio.MatchString(audio) {
		tags = appendUniqueStringLocal(tags, "Atmos")
	}

	return tags
}

const tagDescriptionLogModule = "迁移-标签补全"

var (
	reSubtitleDelimiter       = regexp.MustCompile(`[\[\]【】\|\*\/]`)
	reDescriptionCategoryLine = regexp.MustCompile(`(?im)[◎❁]\s*类\s*别\s*(.+?)(?:\r?\n|$)`)
	reAtmosFromAudio          = regexp.MustCompile(`(?i)(\bAtmos\b|Atmos\d)`)
)

// extractRawTagsFromSubtitle 从副标题中提取语言/字幕/特效相关的原始标签（不带 tag. 前缀）。
// 支持：中字、粤语、国语、台配、特效。
func extractRawTagsFromSubtitle(subtitle string) []string {
	text := strings.TrimSpace(subtitle)
	if text == "" {
		return []string{}
	}

	tags := make([]string, 0, 5)
	if strings.Contains(text, "特效") {
		tags = appendUniqueStringLocal(tags, "特效")
	}

	// DIY：从副标题中提取 DIY 标签（对齐标题组件中的提取逻辑）
	if strings.Contains(strings.ToUpper(text), "DIY") {
		tags = appendUniqueStringLocal(tags, "DIY")
	}

	parts := reSubtitleDelimiter.Split(text, -1)
	if strings.Contains(text, "|") {
		for _, part := range strings.Split(text, "|") {
			part = strings.TrimSpace(part)
			if part != "" {
				parts = append(parts, part)
			}
		}
	}

	seenPart := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seenPart[part]; ok {
			continue
		}
		seenPart[part] = struct{}{}

		partLower := strings.ToLower(part)
		if containsAnySubstring(partLower, []string{"中字", "中文字幕", "chinese", "chs", "cht", "简体", "繁体", "简繁", "中英", "简英", "繁英", "官译", "双语字幕", "多国", "软字幕"}) {
			tags = appendUniqueStringLocal(tags, "中字")
		}

		// 处理 “陆/日/台/粤/闽” 这种单字语言标记。
		for _, token := range splitByCommonDelimiters(part) {
			switch token {
			case "粤":
				tags = appendUniqueStringLocal(tags, "粤语")
			case "台":
				tags = appendUniqueStringLocal(tags, "台配")
			case "陆", "国":
				tags = appendUniqueStringLocal(tags, "国语")
			}
		}

		if containsAnySubstring(partLower, []string{"粤语", "cantonese", "粤音", "港版", "港配", "广东话"}) {
			tags = appendUniqueStringLocal(tags, "粤语")
		}
		if containsAnySubstring(partLower, []string{"国语", "mandarin", "普通话", "汉语", "国配", "华语", "中文配音"}) {
			tags = appendUniqueStringLocal(tags, "国语")
		}
		if containsAnySubstring(partLower, []string{"台配", "taiwan", "taiwanese", "闽南语", "东森", "纬来"}) {
			tags = appendUniqueStringLocal(tags, "台配")
		}
	}

	return tags
}

// extractTagsFromDescriptionCategory 从简介的“类别”字段提取标准化标签（返回 tag.*）。
// 参数/返回：description 为声明与正文拼接文本，返回类别关键词映射后的标签列表。
// 失败场景：未找到类别行或类别内容为空时返回空切片，不影响后续流程。
// 副作用：记录类别命中与标签提取结果信息日志，便于定位标签补全过程。
func extractTagsFromDescriptionCategory(description string) []string {
	categoryText, ok := extractDescriptionCategoryText(description)
	if !ok {
		return []string{}
	}

	type rule struct {
		keyword string
		tag     string
	}
	rules := []rule{
		{"喜剧", "tag.喜剧"}, {"comedy", "tag.喜剧"},
		{"儿童", "tag.儿童"}, {"children", "tag.儿童"},
		{"动画", "tag.动画"}, {"animation", "tag.动画"},
		{"动作", "tag.动作"}, {"action", "tag.动作"},
		{"爱情", "tag.爱情"}, {"romance", "tag.爱情"},
		{"科幻", "tag.科幻"}, {"sci-fi", "tag.科幻"}, {"sci fi", "tag.科幻"},
		{"恐怖", "tag.恐怖"}, {"horror", "tag.恐怖"},
		{"惊悚", "tag.惊悚"}, {"thriller", "tag.惊悚"},
		{"悬疑", "tag.悬疑"}, {"mystery", "tag.悬疑"},
		{"犯罪", "tag.犯罪"}, {"crime", "tag.犯罪"},
		{"战争", "tag.战争"}, {"war", "tag.战争"},
		{"冒险", "tag.冒险"}, {"adventure", "tag.冒险"},
		{"奇幻", "tag.奇幻"}, {"fantasy", "tag.奇幻"},
		{"家庭", "tag.家庭"}, {"family", "tag.家庭"},
		{"剧情", "tag.剧情"}, {"drama", "tag.剧情"},
	}

	normalized := strings.ToLower(categoryText)
	tags := make([]string, 0, 5)
	for _, rule := range rules {
		if strings.Contains(normalized, strings.ToLower(rule.keyword)) {
			tags = appendUniqueStringLocal(tags, rule.tag)
		}
	}

	if len(tags) > 0 {
		logx.Infof(tagDescriptionLogModule, "简介类别提取到标签 category=%s tags=%v", categoryText, tags)
	} else {
		logx.Infof(tagDescriptionLogModule, "简介类别未提取到可映射标签 category=%s", categoryText)
	}
	return tags
}

// checkAnimationTypeFromDescription 判断简介类别字段是否包含 “动画/Animation”，用于修正类型为动漫。
// 参数/返回：description 为声明与正文拼接文本；命中动画类别返回 true。
// 失败场景：类别行缺失或为空时返回 false。
// 副作用：命中动画判定时记录信息日志，便于追踪类型修正来源。
func checkAnimationTypeFromDescription(description string) bool {
	categoryText, ok := extractDescriptionCategoryText(description)
	if !ok {
		return false
	}

	lower := strings.ToLower(categoryText)
	isAnimation := strings.Contains(lower, "动画") || strings.Contains(lower, "animation")
	if isAnimation {
		logx.Infof(tagDescriptionLogModule, "简介类别命中动画判定 category=%s", categoryText)
	}
	return isAnimation
}

// extractDescriptionCategoryText 从简介文本中提取“类别”字段原文。
// 参数/返回：description 为声明与正文拼接文本；返回类别文本与是否命中。
// 失败场景：简介为空、类别行不存在或类别值为空时返回 false。
// 副作用：无。
func extractDescriptionCategoryText(description string) (string, bool) {
	text := strings.TrimSpace(description)
	if text == "" {
		return "", false
	}

	// 兼容“◎类　　别　剧情 / 爱情”中的全角空格（U+3000）。
	normalizedText := strings.ReplaceAll(text, "\u3000", " ")
	matches := reDescriptionCategoryLine.FindStringSubmatch(normalizedText)
	if len(matches) < 2 {
		return "", false
	}

	categoryText := strings.TrimSpace(matches[1])
	if categoryText == "" {
		return "", false
	}
	return categoryText, true
}

// extractRawTagsFromMediaText 从 MediaInfo/BDInfo 文本中提取语言/字幕/HDR/高帧率/高码率等原始标签（不带 tag. 前缀）。
func extractRawTagsFromMediaText(mediaText string, isBDInfo bool) []string {
	text := strings.TrimSpace(parser.SanitizeMediaTextForAnalysis(mediaText))
	if text == "" {
		return []string{}
	}

	tags := make([]string, 0, 10)

	// HDR：优先使用既有的解析器（对齐标题组件的覆盖逻辑）。
	hdr := processingExtractHDRInfoFromMediaText(text, isBDInfo)
	if strings.TrimSpace(hdr.StandardTag) != "" {
		upper := strings.ToUpper(hdr.StandardTag)
		if strings.Contains(upper, "DOVI") {
			tags = appendUniqueStringLocal(tags, "Dolby Vision")
		}
		if strings.Contains(upper, "HDR10+") {
			tags = appendUniqueStringLocal(tags, "HDR10+")
		}
		if strings.Contains(upper, "HLG") {
			tags = appendUniqueStringLocal(tags, "HLG")
		}
		if strings.Contains(upper, "VIVID") {
			tags = appendUniqueStringLocal(tags, "菁彩HDR")
		}
		if !strings.Contains(upper, "HDR10+") && strings.Contains(upper, "HDR") {
			tags = appendUniqueStringLocal(tags, "HDR")
		}
	}

	if isBDInfo {
		// BDInfo 文本结构差异较大，这里用关键词扫描作为兜底。
		lowerAll := strings.ToLower(text)
		lang := detectLanguageByKeyword(lowerAll)
		applyLanguageTag(&tags, lang)
		if strings.Contains(lowerAll, "subtitle") || strings.Contains(lowerAll, "subtitles") {
			if strings.Contains(lowerAll, "chinese") || strings.Contains(lowerAll, "简体") || strings.Contains(lowerAll, "繁体") || strings.Contains(lowerAll, "chs") || strings.Contains(lowerAll, "cht") {
				tags = appendUniqueStringLocal(tags, "中字")
			}
			if strings.Contains(lowerAll, "english") {
				tags = appendUniqueStringLocal(tags, "英字")
			}
		}
	} else {
		// MediaInfo：分块处理 Audio/Text
		sections := splitMediaInfoSections(text)
		for _, sec := range sections {
			switch sec.Name {
			case "Audio":
				lang := detectLanguageInSection(sec.Lines)
				applyLanguageTag(&tags, lang)
			case "Text":
				applySubtitleTagsFromTextSection(&tags, sec.Lines)
			}
		}
	}

	// 高帧率/高码率：全局提取
	if fr := extractFrameRateFPS(text); fr > 50 {
		tags = appendUniqueStringLocal(tags, "高帧率")
	}
	if bitrate := extractOverallBitrateMbps(text); bitrate > 10 {
		tags = appendUniqueStringLocal(tags, "高码率")
	}

	return tags
}

type mediaInfoSection struct {
	Name  string
	Lines []string
}

func splitMediaInfoSections(text string) []mediaInfoSection {
	lines := strings.Split(text, "\n")
	reHeader := regexp.MustCompile(`(?i)^(General|Video|Audio|Text|Menu|Chapters)(\s*#\d+)?$`)

	sections := make([]mediaInfoSection, 0, 12)
	current := mediaInfoSection{Name: "", Lines: []string{}}
	flush := func() {
		if strings.TrimSpace(current.Name) == "" || len(current.Lines) == 0 {
			current = mediaInfoSection{Name: "", Lines: []string{}}
			return
		}
		sections = append(sections, current)
		current = mediaInfoSection{Name: "", Lines: []string{}}
	}

	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if reHeader.MatchString(stripped) {
			flush()
			current.Name = strings.Fields(stripped)[0]
			current.Lines = []string{}
			continue
		}
		if strings.TrimSpace(current.Name) == "" {
			continue
		}
		if stripped == "" {
			continue
		}
		current.Lines = append(current.Lines, stripped)
	}
	flush()
	return sections
}

func detectLanguageInSection(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "title") && strings.Contains(lower, ":") {
			value := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			if lang := detectLanguageByKeyword(strings.ToLower(value)); lang != "" {
				return lang
			}
		}
		if strings.HasPrefix(lower, "language") && strings.Contains(lower, ":") {
			value := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			if lang := detectLanguageByKeyword(strings.ToLower(value)); lang != "" {
				return lang
			}
		}
	}
	return ""
}

func detectLanguageByKeyword(lowerText string) string {
	type rule struct {
		lang     string
		keywords []string
	}
	rules := []rule{
		{"国语", []string{"mandarin", "cmn", "chinese", "中文", "国语", "普通话", "mainland", "mandrin"}},
		{"粤语", []string{"cantonese", "粤语", "广东话", "hongkong"}},
		{"台配", []string{"taiwan mandarin", "taiwanese", "taiwan", "台配", "台湾", "台语", "闽南语"}},
		{"英语", []string{"english", "英语"}},
		{"日语", []string{"japanese", "日语"}},
		{"韩语", []string{"korean", "韩语"}},
		{"法语", []string{"french", "法语"}},
		{"德语", []string{"german", "德语"}},
		{"俄语", []string{"russian", "俄语"}},
		{"印地语", []string{"hindi", "印地语"}},
		{"西班牙语", []string{"spanish", "西班牙语", "latin america"}},
		{"葡萄牙语", []string{"portuguese", "葡萄牙语", "br"}},
		{"意大利语", []string{"italian", "意大利语"}},
		{"泰语", []string{"thai", "泰语"}},
		{"阿拉伯语", []string{"arabic", "阿拉伯语", "sa"}},
	}
	for _, rule := range rules {
		for _, keyword := range rule.keywords {
			if keyword == "" {
				continue
			}
			if strings.Contains(lowerText, strings.ToLower(keyword)) {
				return rule.lang
			}
		}
	}
	return ""
}

func applyLanguageTag(tags *[]string, language string) {
	if tags == nil || strings.TrimSpace(language) == "" {
		return
	}
	switch language {
	case "国语", "粤语", "台配":
		*tags = appendUniqueStringLocal(*tags, language)
	default:
		*tags = appendUniqueStringLocal(*tags, language)
		*tags = appendUniqueStringLocal(*tags, "外语")
	}
}

func applySubtitleTagsFromTextSection(tags *[]string, lines []string) {
	if tags == nil || len(lines) == 0 {
		return
	}
	for _, line := range lines {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "language") && !strings.Contains(lower, "title") {
			continue
		}
		if !strings.Contains(lower, ":") {
			continue
		}
		value := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		valueLower := strings.ToLower(value)
		if containsAnySubstring(valueLower, []string{"chinese", "简体", "繁体", "chs", "cht"}) {
			*tags = appendUniqueStringLocal(*tags, "中字")
		}
		if containsAnySubstring(valueLower, []string{"english"}) {
			*tags = appendUniqueStringLocal(*tags, "英字")
		}
	}
}

func extractFrameRateFPS(text string) float64 {
	text = normalizeMediaInfoWhitespace(text)
	re := regexp.MustCompile(`(?i)Frame\s*rate\s*:\s*([\d.]+)\s*(?:\([\d/]+\))?\s*FPS`)
	match := re.FindStringSubmatch(text)
	if len(match) < 2 {
		return 0
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(match[1]), 64)
	if err != nil {
		return 0
	}
	return value
}

func extractOverallBitrateMbps(text string) float64 {
	text = normalizeMediaInfoWhitespace(text)
	re := regexp.MustCompile(`(?i)Overall\s*bit\s*rate\s*:\s*([\d\s.]+)\s*(Mb/s|kb/s|Kbps|Mbps)`)
	match := re.FindStringSubmatch(text)
	if len(match) < 3 {
		return 0
	}
	number := strings.NewReplacer(" ", "", "\u00a0", "", "\u2007", "", "\u202f", "").Replace(strings.TrimSpace(match[1]))
	value, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return 0
	}
	unit := strings.ToLower(strings.TrimSpace(match[2]))
	switch unit {
	case "kb/s", "kbps":
		return value / 1000
	default:
		return value
	}
}

var mediaInfoWhitespaceReplacer = strings.NewReplacer(
	"\u00a0", " ", // &nbsp;
	"\u2007", " ", // &numsp;
	"\u202f", " ", // narrow no-break space
)

func normalizeMediaInfoWhitespace(text string) string {
	if strings.IndexAny(text, "\u00a0\u2007\u202f") == -1 {
		return text
	}
	return mediaInfoWhitespaceReplacer.Replace(text)
}

func containsAnySubstring(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if keyword == "" {
			continue
		}
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func splitByCommonDelimiters(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case '/', '／', '|', '、', ',', '，', ';', '；', ' ':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// ExtractRawTagsFromTitleComponents 从标题组件提取原始标签。
func ExtractRawTagsFromTitleComponents(components []map[string]any) []string {
	return extractRawTagsFromTitleComponents(components)
}

// ExtractRawTagsFromSubtitle 从副标题提取原始标签。
func ExtractRawTagsFromSubtitle(subtitle string) []string {
	return extractRawTagsFromSubtitle(subtitle)
}

// ExtractTagsFromDescriptionCategory 从简介类别提取标准 tag.* 标签。
func ExtractTagsFromDescriptionCategory(description string) []string {
	return extractTagsFromDescriptionCategory(description)
}

// CheckAnimationTypeFromDescription 判断简介是否命中动画类型。
func CheckAnimationTypeFromDescription(description string) bool {
	return checkAnimationTypeFromDescription(description)
}

// ExtractRawTagsFromMediaText 从媒体文本提取原始标签。
func ExtractRawTagsFromMediaText(mediaText string, isBDInfo bool) []string {
	return extractRawTagsFromMediaText(mediaText, isBDInfo)
}

func processingExtractHDRInfoFromMediaText(text string, isBDInfo bool) processingmedia.HDRInfo {
	return processingmedia.ExtractHDRInfoFromMediaText(text, isBDInfo)
}

func appendUniqueStringLocal(items []string, value string) []string {
	for _, item := range items {
		if strings.EqualFold(item, value) {
			return items
		}
	}
	return append(items, value)
}

func toStringAny(value any, fallback string) string {
	switch typed := value.(type) {
	case nil:
		return fallback
	case string:
		return typed
	case []byte:
		return string(typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int8:
		return fmt.Sprintf("%d", typed)
	case int16:
		return fmt.Sprintf("%d", typed)
	case int32:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case uint:
		return fmt.Sprintf("%d", typed)
	case uint8:
		return fmt.Sprintf("%d", typed)
	case uint16:
		return fmt.Sprintf("%d", typed)
	case uint32:
		return fmt.Sprintf("%d", typed)
	case uint64:
		return fmt.Sprintf("%d", typed)
	case float32:
		return fmt.Sprintf("%v", typed)
	case float64:
		return fmt.Sprintf("%v", typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fallback
	}
}
