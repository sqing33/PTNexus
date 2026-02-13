package persist

import (
	"strings"
	"time"
)

// NormalizeSeedParameterDateTime 将任意时间值规范化为 seed_parameters 统一的 DATETIME 字符串。
// 参数/返回：value 可为 time.Time/字符串/[]byte；fallback 为兜底值；返回 `2006-01-02 15:04:05` 格式。
// 失败场景：无法解析或输入为空时返回 fallback。
// 副作用：无。
func NormalizeSeedParameterDateTime(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	const layout = "2006-01-02 15:04:05"
	switch typed := value.(type) {
	case time.Time:
		if typed.IsZero() {
			return fallback
		}
		return typed.Format(layout)
	case []byte:
		return NormalizeSeedParameterDateTime(string(typed), fallback)
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return fallback
		}

		// MySQL DATETIME 兼容格式：直接截取前 19 位（可剔除类似 “+0800 CST” 的尾巴）。
		if len(trimmed) >= len(layout) &&
			trimmed[4] == '-' && trimmed[7] == '-' && trimmed[10] == ' ' &&
			trimmed[13] == ':' && trimmed[16] == ':' {
			return trimmed[:19]
		}

		for _, candidate := range []string{
			layout,
			time.RFC3339,
			"2006-01-02T15:04:05Z07:00",
			"2006-01-02 15:04:05 -0700 MST",
			"2006-01-02 15:04:05 -0700",
		} {
			if parsed, err := time.ParseInLocation(candidate, trimmed, time.Local); err == nil {
				return parsed.Format(layout)
			}
		}
		return fallback
	default:
		return fallback
	}
}
