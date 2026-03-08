package extract

import (
	"regexp"
	"strings"
)

var (
	reMediaTextDescriptionField = regexp.MustCompile(`(?i)^\s*Description\s*:`)
	reMediaTextSectionHeader    = regexp.MustCompile(`(?i)^(General|Video|Audio|Text|Menu|Chapters)(\s*#\d+)?$|^(DISC INFO|PLAYLIST REPORT|QUICK SUMMARY|VIDEO:|AUDIO:|SUBTITLES:|FILES:|CHAPTERS:|DISC SIZE)$`)
)

// SanitizeMediaTextForAnalysis 生成供校验/推断使用的媒体文本视图，当前仅忽略 Description 字段值。
// 参数/返回：text 为原始 MediaInfo/BDInfo 文本；返回保留原始结构但已清空 Description 值的副本。
// 失败场景：输入为空时直接返回空字符串。
// 副作用：无；不会修改原始文本或调用方持有的数据。
func SanitizeMediaTextForAnalysis(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}

	normalized := strings.ReplaceAll(trimmed, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")
	result := make([]string, 0, len(lines))
	skipContinuation := false

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		if skipContinuation {
			switch {
			case trimmedLine == "":
				continue
			case reMediaTextSectionHeader.MatchString(trimmedLine):
				skipContinuation = false
			case strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t"):
				continue
			default:
				skipContinuation = false
			}
		}

		if reMediaTextDescriptionField.MatchString(line) {
			if idx := strings.Index(line, ":"); idx >= 0 {
				result = append(result, line[:idx+1])
			} else {
				result = append(result, strings.TrimRight(line, " \t"))
			}
			skipContinuation = true
			continue
		}

		result = append(result, line)
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}
