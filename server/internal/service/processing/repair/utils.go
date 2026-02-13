package repair

import (
	"encoding/json"
	"strings"
)

// CompactLogText 压缩日志文本并截断到指定长度，避免日志过长影响阅读。
func CompactLogText(text string, maxLen int) string {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return ""
	}
	normalized = strings.ReplaceAll(normalized, "\r", " ")
	normalized = strings.ReplaceAll(normalized, "\n", " ")
	normalized = strings.Join(strings.Fields(normalized), " ")
	if maxLen <= 0 {
		maxLen = 120
	}

	runes := []rune(normalized)
	if len(runes) <= maxLen {
		return normalized
	}
	return string(runes[:maxLen]) + "..."
}

func toStringAny(value any, fallback string) string {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed != "" {
			return trimmed
		}
	case []byte:
		trimmed := strings.TrimSpace(string(typed))
		if trimmed != "" {
			return trimmed
		}
	case json.Number:
		trimmed := strings.TrimSpace(typed.String())
		if trimmed != "" {
			return trimmed
		}
	}
	return fallback
}
