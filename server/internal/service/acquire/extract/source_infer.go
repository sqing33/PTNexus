package extract

import (
	"regexp"
	"strings"
)

var (
	reDescriptionSourceLine  = regexp.MustCompile(`(?im)^[◎❁]\s*产\s*地\s*[:：]?\s*(.+?)(?:\r?\n|$)`)
	reDescriptionCountryLine = regexp.MustCompile(`(?im)^[◎❁]\s*(?:国\s*家|地\s*区)\s*[:：]?\s*(.+?)(?:\r?\n|$)`)
	reRegionSourceLine       = regexp.MustCompile(`(?im)^\s*(?:制片国家/地区|制片國家/地區)\s*[:：]\s*(.+?)(?:\r?\n|$)`)
)

// InferSourceFromDescription 从简介文本中提取“产地/制片国家/地区”并映射为标准 source.* 键。
// 参数/返回：description 为声明与正文拼接文本；成功返回 source.china/source.japan 等；未命中或无法识别返回空字符串。
// 失败场景：description 为空或不存在产地行时返回空字符串。
// 副作用：无。
func InferSourceFromDescription(description string) string {
	text := strings.TrimSpace(description)
	if text == "" {
		return ""
	}

	// 兼容“◎产　　地　日本”里的全角空格（U+3000）。
	normalized := strings.ReplaceAll(text, "\u3000", " ")

	sourceText := ""
	if matches := reDescriptionSourceLine.FindStringSubmatch(normalized); len(matches) >= 2 {
		sourceText = strings.TrimSpace(matches[1])
	} else if matches := reDescriptionCountryLine.FindStringSubmatch(normalized); len(matches) >= 2 {
		sourceText = strings.TrimSpace(matches[1])
	} else if matches := reRegionSourceLine.FindStringSubmatch(normalized); len(matches) >= 2 {
		sourceText = strings.TrimSpace(matches[1])
	}
	if sourceText == "" {
		return ""
	}

	// 常见形式：日本 / 韩国、美国, 英国、China/Hong Kong 等。
	sourceLower := strings.ToLower(sourceText)
	switch {
	case strings.Contains(sourceLower, "台湾") || strings.Contains(sourceLower, "taiwan") || strings.Contains(sourceLower, "twn"):
		return "source.taiwan"
	case strings.Contains(sourceLower, "香港") || strings.Contains(sourceLower, "hong kong") || strings.Contains(sourceLower, "hkg"):
		return "source.hongkong"
	case strings.Contains(sourceLower, "中国") || strings.Contains(sourceLower, "china") || strings.Contains(sourceLower, "chn"):
		return "source.china"
	case strings.Contains(sourceLower, "日本") || strings.Contains(sourceLower, "japan") || strings.Contains(sourceLower, "jpn"):
		return "source.japan"
	case strings.Contains(sourceLower, "韩国") || strings.Contains(sourceLower, "korea") || strings.Contains(sourceLower, "kor"):
		return "source.korea"
	case strings.Contains(sourceLower, "英国") || strings.Contains(sourceLower, "uk"):
		return "source.uk"
	case strings.Contains(sourceLower, "美国") || strings.Contains(sourceLower, "usa") || strings.Contains(sourceLower, "united states") || strings.Contains(sourceLower, "us "):
		return "source.western"
	default:
		return ""
	}
}
