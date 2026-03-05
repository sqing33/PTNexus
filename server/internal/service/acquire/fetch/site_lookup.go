package fetch

import (
	"net/url"
	"strings"

	"gorm.io/gorm"
)

// FindSiteByDetailURL 根据详情页 URL 反查站点配置。
// 参数/返回：db 为站点配置所在数据库连接，detailURL 为详情页地址；命中返回站点配置行，未命中返回空 map。
// 失败场景：db 为空、查询失败或 URL 为空时返回空 map。
// 副作用：读取数据库 sites 表。
func FindSiteByDetailURL(db *gorm.DB, detailURL string) map[string]any {
	trimmed := strings.TrimSpace(detailURL)
	if db == nil || trimmed == "" {
		return map[string]any{}
	}

	detailHost := parseDetailHost(trimmed)
	detailCore := extractCoreDomain(detailHost)

	rows := make([]map[string]any, 0)
	if err := db.Table("sites").Select("nickname, site, base_url, cookie, passkey, migration, speed_limit").Where("base_url IS NOT NULL AND base_url != ''").Scan(&rows).Error; err != nil {
		return map[string]any{}
	}
	lowerURL := strings.ToLower(trimmed)
	for _, row := range rows {
		baseHost := parseBaseHost(toStringAny(row["base_url"], ""))
		if baseHost == "" {
			continue
		}
		if strings.Contains(lowerURL, baseHost) {
			return row
		}
		// 兼容站点更换域名：使用 core domain 做兜底匹配（如 rousi.xxx -> rousi.pro）。
		if detailCore != "" {
			baseCore := extractCoreDomain(baseHost)
			if baseCore != "" && baseCore == detailCore {
				return row
			}
		}
	}
	return map[string]any{}
}

func parseDetailHost(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	return normalizeHostForMatch(parsed.Hostname())
}

func parseBaseHost(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	normalized := trimmed
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		normalized = "https://" + trimmed
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return ""
	}
	return normalizeHostForMatch(parsed.Hostname())
}

func normalizeHostForMatch(host string) string {
	trimmed := strings.ToLower(strings.TrimSpace(host))
	if trimmed == "" {
		return ""
	}
	return strings.TrimPrefix(trimmed, "www.")
}

func extractCoreDomain(host string) string {
	cleaned := normalizeHostForMatch(host)
	if cleaned == "" {
		return ""
	}
	parts := strings.Split(cleaned, ".")
	if len(parts) == 0 {
		return ""
	}
	if len(parts) > 2 {
		last := parts[len(parts)-1]
		prev := parts[len(parts)-2]
		if len(last) <= 3 && len(prev) <= 3 && len(parts) >= 3 {
			return parts[len(parts)-3]
		}
	}
	if len(parts) > 1 {
		return parts[len(parts)-2]
	}
	return parts[0]
}
