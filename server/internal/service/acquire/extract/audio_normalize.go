package extract

import (
	"regexp"
	"strings"
)

// normalizeAudioCodecTokensForInference 对标题/技术文本中的音频编码 token 做容错纠偏。
// 参数/返回：输入为任意文本；返回做过标准化替换后的文本（不会改变原始语义，只修正常见拼写/分隔符问题）。
// 失败场景：空字符串直接返回空字符串。
// 副作用：无。
func normalizeAudioCodecTokensForInference(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}

	out := trimmed
	out = strings.ReplaceAll(out, "。", ".")
	out = strings.ReplaceAll(out, "．", ".")

	// 对齐 Python：先修正 codec token，再进入 contains 判断，避免 "DTS-HDMA" 被误判为 "DTS"。
	rules := []struct {
		pattern     *regexp.Regexp
		replacement string
	}{
		{regexp.MustCompile(`(?i)\bDTS[-\s\.]*HD[-\s\.]*MA\b`), "DTS-HD MA"},
		{regexp.MustCompile(`(?i)\bDTS[-\s\.]*HD[-\s\.]*HR\b`), "DTS-HD HR"},
		{regexp.MustCompile(`(?i)\bTrue[-\s\.]*HD\b`), "TrueHD"},
		{regexp.MustCompile(`(?i)\bDTS[-\s\.]*X\b`), "DTS:X"},
		{regexp.MustCompile(`(?i)\bDTS\s*X\b`), "DTS:X"},
		{regexp.MustCompile(`(?i)\bE[-\s\.]*AC[-\s\.]*3\b`), "DDP"},
		// 对齐 Python：DD+ 等价于 DDP（E-AC-3），这里统一输出 DDP，且兼容全角加号等变体。
		{regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}_])DD\s*[\+＋﹢]([^\p{L}\p{N}_]|$)`), "${1}DDP${2}"},
	}
	for _, rule := range rules {
		out = rule.pattern.ReplaceAllString(out, rule.replacement)
	}

	out = strings.Join(strings.Fields(out), " ")
	return strings.TrimSpace(out)
}
