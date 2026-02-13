package fetch

import (
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
	rows := make([]map[string]any, 0)
	if err := db.Table("sites").Select("nickname, site, base_url, cookie, passkey, migration, speed_limit").Where("base_url IS NOT NULL AND base_url != ''").Scan(&rows).Error; err != nil {
		return map[string]any{}
	}
	lowerURL := strings.ToLower(trimmed)
	for _, row := range rows {
		base := strings.ToLower(strings.TrimSpace(toStringAny(row["base_url"], "")))
		if base == "" {
			continue
		}
		base = strings.TrimPrefix(base, "http://")
		base = strings.TrimPrefix(base, "https://")
		base = strings.TrimSuffix(base, "/")
		if strings.Contains(lowerURL, base) {
			return row
		}
	}
	return map[string]any{}
}
