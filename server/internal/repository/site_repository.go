package repository

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type SiteRepository struct {
	store *Store
}

func NewSiteRepository(store *Store) *SiteRepository {
	return &SiteRepository{store: store}
}

func (r *SiteRepository) ListSourceAndTargetSites() ([]string, []string, error) {
	sqlDB, err := r.store.DB.DB()
	if err != nil {
		return nil, nil, err
	}

	sourceRows, err := sqlDB.Query(`
		SELECT nickname FROM sites
		WHERE (migration = 1 OR migration = 3)
		AND cookie IS NOT NULL AND cookie != ''
		ORDER BY sort_order, nickname
	`)
	if err != nil {
		return nil, nil, err
	}
	defer sourceRows.Close()

	sourceSites := make([]string, 0)
	for sourceRows.Next() {
		var nickname string
		if err := sourceRows.Scan(&nickname); err != nil {
			return nil, nil, err
		}
		sourceSites = append(sourceSites, nickname)
	}

	targetRows, err := sqlDB.Query(`
		SELECT nickname FROM sites
		WHERE (migration = 2 OR migration = 3)
		ORDER BY sort_order, nickname
	`)
	if err != nil {
		return nil, nil, err
	}
	defer targetRows.Close()

	targetSites := make([]string, 0)
	for targetRows.Next() {
		var nickname string
		if err := targetRows.Scan(&nickname); err != nil {
			return nil, nil, err
		}
		targetSites = append(targetSites, nickname)
	}

	return sourceSites, targetSites, nil
}

func (r *SiteRepository) ListSites(filterByTorrents string) ([]map[string]any, error) {
	groupColumn := r.store.GroupColumn()
	selectFields := fmt.Sprintf(`
		s.id, s.nickname, s.site, s.base_url, s.special_tracker_domain, s.%s, s.speed_limit,
		s.ratio_threshold, s.seed_speed_limit, s.can_publish, s.sort_order,
		CASE WHEN s.cookie IS NOT NULL AND s.cookie != '' THEN 1 ELSE 0 END as has_cookie,
		CASE WHEN s.passkey IS NOT NULL AND s.passkey != '' THEN 1 ELSE 0 END as has_passkey,
		s.cookie, s.passkey
	`, groupColumn)

	query := ""
	if filterByTorrents == "active" {
		if r.store.DBType == "mysql" {
			query = fmt.Sprintf(`
					SELECT DISTINCT %s
					FROM sites s
					WHERE EXISTS (
						SELECT 1 FROM torrents t
						WHERE LOWER(s.nickname) COLLATE utf8mb4_unicode_ci = LOWER(t.sites) COLLATE utf8mb4_unicode_ci
						  AND (t.is_hidden = 0 OR t.is_hidden IS NULL)
					)
					OR (s.cookie IS NOT NULL AND s.cookie != '')
					ORDER BY s.sort_order, s.nickname COLLATE utf8mb4_unicode_ci
			`, selectFields)
		} else {
			query = fmt.Sprintf(`
					SELECT DISTINCT %s
					FROM sites s
					WHERE EXISTS (
						SELECT 1 FROM torrents t WHERE LOWER(s.nickname) = LOWER(t.sites) AND (t.is_hidden = 0 OR t.is_hidden IS NULL)
					)
					OR (s.cookie IS NOT NULL AND s.cookie != '')
					ORDER BY s.sort_order, s.nickname
			`, selectFields)
		}
	} else {
		if r.store.DBType == "mysql" {
			query = fmt.Sprintf("SELECT %s FROM sites s ORDER BY s.sort_order, s.nickname COLLATE utf8mb4_unicode_ci", selectFields)
		} else {
			query = fmt.Sprintf("SELECT %s FROM sites s ORDER BY s.sort_order, s.nickname", selectFields)
		}
	}

	sqlDB, err := r.store.DB.DB()
	if err != nil {
		return nil, err
	}
	rows, err := sqlDB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sites, err := rowsToMaps(rows)
	if err != nil {
		return nil, err
	}
	for _, s := range sites {
		s["can_publish"] = toIntWithDefault(s["can_publish"], 1) != 0
	}
	return sites, nil
}

func (r *SiteRepository) UpdateSiteDetails(data map[string]any) (bool, error) {
	siteID, err := toInt64(data["id"])
	if err != nil || siteID <= 0 {
		return false, errors.New("invalid site id")
	}

	cookie := strings.TrimSpace(toString(data["cookie"], ""))
	ratioThreshold := toFloat64WithDefault(data["ratio_threshold"], 3.0)
	if ratioThreshold <= 0 {
		ratioThreshold = 3.0
	}
	seedSpeedLimit := toIntWithDefault(data["seed_speed_limit"], 5)
	canPublish := toIntWithDefault(data["can_publish"], 1)
	sortOrder := toIntWithDefault(data["sort_order"], 0)

	groupColumn := r.store.GroupColumn()
	sql := fmt.Sprintf(`
		UPDATE sites
		SET nickname = ?,
			base_url = ?,
			special_tracker_domain = ?,
			%s = ?,
			description = ?,
			cookie = ?,
			passkey = ?,
			speed_limit = ?,
			ratio_threshold = ?,
			seed_speed_limit = ?,
			can_publish = ?,
			sort_order = ?
		WHERE id = ?
	`, groupColumn)

	result := r.store.DB.Exec(
		sql,
		toString(data["nickname"], ""),
		toString(data["base_url"], ""),
		toString(data["special_tracker_domain"], ""),
		toString(data["group"], ""),
		toString(data["description"], ""),
		cookie,
		toString(data["passkey"], ""),
		toIntWithDefault(data["speed_limit"], 0),
		ratioThreshold,
		seedSpeedLimit,
		canPublish,
		sortOrder,
		siteID,
	)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *SiteRepository) DeleteSite(siteID int64) (bool, error) {
	result := r.store.DB.Exec("DELETE FROM sites WHERE id = ?", siteID)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *SiteRepository) UpdateSiteCookie(nickname, cookie string) (bool, error) {
	result := r.store.DB.Exec("UPDATE sites SET cookie = ? WHERE nickname = ?", strings.TrimSpace(cookie), nickname)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// UpdateSiteCookieBySite 根据站点标识更新 Cookie。
// 参数/返回：siteCode 为 sites.site 字段，cookie 为标准 "k=v; k2=v2" 字符串；返回是否命中并更新。
// 失败场景：数据库执行失败时返回错误。
// 副作用：写入 sites 表的 cookie 字段。
func (r *SiteRepository) UpdateSiteCookieBySite(siteCode, cookie string) (bool, error) {
	result := r.store.DB.Exec("UPDATE sites SET cookie = ? WHERE site = ?", strings.TrimSpace(cookie), strings.TrimSpace(siteCode))
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *SiteRepository) SitesStatus() ([]map[string]any, error) {
	sqlDB, err := r.store.DB.DB()
	if err != nil {
		return nil, err
	}
	rows, err := sqlDB.Query("SELECT nickname, site, cookie, passkey, migration, can_publish, sort_order FROM sites ORDER BY sort_order, nickname")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	raw, err := rowsToMaps(rows)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]any, 0, len(raw))
	for _, row := range raw {
		migration := toIntWithDefault(row["migration"], 0)
		result = append(result, map[string]any{
			"name":        toString(row["nickname"], ""),
			"site":        toString(row["site"], ""),
			"has_cookie":  toString(row["cookie"], "") != "",
			"has_passkey": toString(row["passkey"], "") != "",
			"is_source":   migration == 1 || migration == 3,
			"is_target":   migration == 2 || migration == 3,
			"can_publish": toIntWithDefault(row["can_publish"], 1) != 0,
		})
	}
	return result, nil
}

func (r *SiteRepository) SetTorrentSiteNotExist(torrentName, siteName string) (bool, error) {
	result := r.store.DB.Exec("UPDATE torrents SET state = ? WHERE name = ? AND sites = ?", "不存在", torrentName, siteName)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *SiteRepository) UpdateTorrentComment(torrentName, siteName, comment string) (bool, error) {
	result := r.store.DB.Exec("UPDATE torrents SET details = ? WHERE name = ? AND sites = ?", comment, torrentName, siteName)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// UpdateSitesSortOrder 批量更新站点排序序号。
// 参数/返回：siteIDs 为按目标顺序排列的站点 ID 列表；返回更新条数与错误。
// 失败场景：事务或 SQL 执行失败时返回错误。
// 副作用：写入 sites 表的 sort_order 字段。
func (r *SiteRepository) UpdateSitesSortOrder(siteIDs []int64) (int, error) {
	if len(siteIDs) == 0 {
		return 0, nil
	}
	updated := 0
	err := r.store.DB.Transaction(func(tx *gorm.DB) error {
		for idx, id := range siteIDs {
			if id <= 0 {
				continue
			}
			result := tx.Exec("UPDATE sites SET sort_order = ? WHERE id = ?", idx+1, id)
			if result.Error != nil {
				return result.Error
			}
			updated += int(result.RowsAffected)
		}
		return nil
	})
	return updated, err
}
