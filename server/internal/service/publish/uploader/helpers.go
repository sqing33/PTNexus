package uploader

import (
	"fmt"
	neturl "net/url"
	"regexp"
	"strings"
)

var (
	rePublishURLAbsolute = regexp.MustCompile(`(?is)https?://[^"'\s>]+/(?:details\.php\?id=\d+|offers\.php\?id=\d+|torrent/[0-9a-fA-F\-]{36})`)
	rePublishURLRelative = regexp.MustCompile(`(?is)/(?:details\.php\?id=\d+|offers\.php\?id=\d+|torrent/[0-9a-fA-F\-]{36})`)
)

// DetectRestrictedTags 检测上传参数中的禁转/限转/分集标签。
func DetectRestrictedTags(uploadData map[string]any) []string {
	restrictedMap := map[string]string{
		"禁转":     "tag.禁转",
		"tag.禁转": "tag.禁转",
		"限转":     "tag.限转",
		"tag.限转": "tag.限转",
		"分集":     "tag.分集",
		"tag.分集": "tag.分集",
	}
	result := []string{}
	seen := map[string]struct{}{}
	standardized := map[string]any{}
	if item, ok := uploadData["standardized_params"].(map[string]any); ok {
		standardized = item
	}
	rawTags := append(parseStringArray(standardized["tags"]), parseStringArray(uploadData["tags"])...)
	for _, tag := range rawTags {
		if mapped, ok := restrictedMap[tag]; ok {
			if _, exists := seen[mapped]; !exists {
				seen[mapped] = struct{}{}
				result = append(result, mapped)
			}
		}
	}
	return result
}

// BuildUploadDescription 按固定顺序拼接发布描述正文。
// 参数/返回：siteCode 为目标站点 code（用于处理少数站点的 MediaInfo 内嵌规则）；uploadData 为发布参数。
// 失败场景：无失败场景，字段缺失时自动跳过。
// 副作用：无。
func BuildUploadDescription(siteCode string, uploadData map[string]any) string {
	intro := map[string]any{}
	if item, ok := uploadData["intro"].(map[string]any); ok && item != nil {
		intro = item
	}

	statement := pickDescriptionSection(uploadData, intro, "statement")
	poster := pickDescriptionSection(uploadData, intro, "poster")
	body := pickDescriptionSection(uploadData, intro, "body")
	screenshots := pickDescriptionSection(uploadData, intro, "screenshots")
	mediainfo := strings.TrimSpace(toStringAny(uploadData["mediainfo"], ""))

	parts := make([]string, 0, 5)
	for _, section := range []string{statement, poster, body} {
		if strings.TrimSpace(section) != "" {
			parts = append(parts, section)
		}
	}

	if shouldInlineMediainfo(siteCode) && mediainfo != "" {
		parts = append(parts, "[quote]"+mediainfo+"[/quote]")
	}

	if screenshots != "" {
		parts = append(parts, screenshots)
	}

	if len(parts) == 0 {
		return strings.TrimSpace(toStringAny(uploadData["subtitle"], ""))
	}
	return strings.Join(parts, "\n")
}

func pickDescriptionSection(uploadData map[string]any, intro map[string]any, key string) string {
	fromTop := strings.TrimSpace(toStringAny(uploadData[key], ""))
	if fromTop != "" {
		return fromTop
	}
	return strings.TrimSpace(toStringAny(intro[key], ""))
}

func shouldInlineMediainfo(siteCode string) bool {
	switch strings.ToLower(strings.TrimSpace(siteCode)) {
	case "btschool", "carpt", "kufei", "muxuege", "ptskit", "sewerpt", "upxin", "zmpt":
		return true
	default:
		return false
	}
}

// ExtractPublishURLFromText 从上传响应文本中提取详情页/offer 链接。
func ExtractPublishURLFromText(baseURL, text string) string {
	if match := rePublishURLAbsolute.FindString(text); match != "" {
		return strings.TrimSpace(match)
	}
	if match := rePublishURLRelative.FindString(text); match != "" {
		return strings.TrimRight(baseURL, "/") + match
	}
	return ""
}

// NormalizePublishURL 标准化发布 URL 到绝对链接。
func NormalizePublishURL(baseURL, candidate string) string {
	trimmed := strings.TrimSpace(candidate)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "http://") || strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "/") {
		return strings.TrimRight(baseURL, "/") + trimmed
	}
	if strings.Contains(trimmed, "details.php") || strings.Contains(trimmed, "/torrent/") {
		return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(trimmed, "/")
	}
	if parsed, err := neturl.Parse(trimmed); err == nil && parsed.Path != "" {
		return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(parsed.String(), "/")
	}
	return ""
}

func parseStringArray(value any) []string {
	switch typed := value.(type) {
	case nil:
		return []string{}
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			trimmed := strings.TrimSpace(toStringAny(item, ""))
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case string:
		raw := strings.TrimSpace(typed)
		if raw == "" {
			return []string{}
		}
		if strings.Contains(raw, ",") {
			items := strings.Split(raw, ",")
			out := make([]string, 0, len(items))
			for _, item := range items {
				trimmed := strings.TrimSpace(item)
				if trimmed != "" {
					out = append(out, trimmed)
				}
			}
			return out
		}
		return []string{raw}
	default:
		return []string{}
	}
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
