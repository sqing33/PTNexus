package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type MigrateRepository struct {
	store *Store
}

func NewMigrateRepository(store *Store) *MigrateRepository {
	return &MigrateRepository{store: store}
}

func (r *MigrateRepository) DB() *gorm.DB {
	return r.store.DB
}

// AggregatedTorrentDeleteTarget 描述“一种多站”聚合行中待删除的下载器种子。
type AggregatedTorrentDeleteTarget struct {
	Hash         string `gorm:"column:hash"`
	Name         string `gorm:"column:name"`
	SavePath     string `gorm:"column:save_path"`
	Size         int64  `gorm:"column:size"`
	DownloaderID string `gorm:"column:downloader_id"`
}

// ListAggregatedTorrentDeleteTargets 按 name+size 定位“一种多站”行内可删除的种子记录。
// 参数/返回：name 与 size 定位聚合行；downloaderIDs 为空时匹配所有下载器；返回 hash/downloader 列表。
// 失败场景：repo 未初始化或数据库查询失败时返回 error。
// 副作用：仅执行数据库只读查询。
func (r *MigrateRepository) ListAggregatedTorrentDeleteTargets(name string, size int64, downloaderIDs []string) ([]AggregatedTorrentDeleteTarget, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return nil, errors.New("repo is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" || size <= 0 {
		return []AggregatedTorrentDeleteTarget{}, nil
	}

	ids := normalizeStringList(downloaderIDs)
	rows := make([]AggregatedTorrentDeleteTarget, 0)
	query := r.store.DB.Table("torrents").
		Select("hash, name, save_path, size, downloader_id").
		Where("name = ? AND size = ? AND state NOT IN ? AND hash IS NOT NULL AND hash != '' AND downloader_id IS NOT NULL AND downloader_id != '' AND (is_hidden = 0 OR is_hidden IS NULL)", name, size, []string{"未做种", "不存在"})
	if len(ids) > 0 {
		query = query.Where("downloader_id IN ?", ids)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// HideTorrentRowsByHashes 将已从下载器删除的种子记录标记为隐藏。
// 参数/返回：downloaderID 与 hashes 精确定位记录；reason 写入隐藏原因；返回 torrents 影响行数。
// 失败场景：参数为空、事务开启失败或更新数据库失败时返回 error。
// 副作用：更新 torrents 与 torrent_upload_stats 的隐藏字段，不物理删除数据库行。
func (r *MigrateRepository) HideTorrentRowsByHashes(downloaderID string, hashes []string, reason string) (int64, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return 0, errors.New("repo is nil")
	}
	downloaderID = strings.TrimSpace(downloaderID)
	normalizedHashes := normalizeStringList(hashes)
	if downloaderID == "" || len(normalizedHashes) == 0 {
		return 0, nil
	}

	hiddenAt := time.Now().Format("2006-01-02 15:04:05")
	updateData := map[string]any{
		"is_hidden":     1,
		"hidden_reason": strings.TrimSpace(reason),
		"hidden_at":     hiddenAt,
	}

	tx := r.store.DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}

	hideResult := tx.Table("torrents").
		Where("downloader_id = ? AND hash IN ? AND (is_hidden = 0 OR is_hidden IS NULL)", downloaderID, normalizedHashes).
		Updates(updateData)
	if hideResult.Error != nil {
		tx.Rollback()
		return 0, hideResult.Error
	}

	hideUpload := tx.Table("torrent_upload_stats").
		Where("downloader_id = ? AND hash IN ? AND (is_hidden = 0 OR is_hidden IS NULL)", downloaderID, normalizedHashes).
		Updates(updateData)
	if hideUpload.Error != nil {
		tx.Rollback()
		return 0, hideUpload.Error
	}

	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return hideResult.RowsAffected, nil
}

func normalizeStringList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

// ReplaceUnseededPlaceholderHash 将 torrents 表中指定站点的“未做种”占位记录 hash 替换为真实 infohash。
// 参数/返回：按 name+size+downloader_id+sites 定位；返回旧 hash 与是否完成替换。
// 失败场景：查询失败、主键冲突或更新失败时返回 error。
// 副作用：写入 torrents（UPDATE），并同步更新 torrent_upload_stats（若存在）。
func (r *MigrateRepository) ReplaceUnseededPlaceholderHash(name string, size int64, downloaderID string, siteNickname string, newHash string) (string, bool, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return "", false, errors.New("repo is nil")
	}
	name = strings.TrimSpace(name)
	downloaderID = strings.TrimSpace(downloaderID)
	siteNickname = strings.TrimSpace(siteNickname)
	newHash = strings.TrimSpace(newHash)
	if name == "" || size <= 0 || downloaderID == "" || siteNickname == "" || newHash == "" {
		return "", false, nil
	}

	row := struct {
		Hash string `gorm:"column:hash"`
	}{}
	if err := r.store.DB.Raw(
		`SELECT hash
		 FROM torrents
		 WHERE name = ? AND size = ? AND downloader_id = ? AND sites = ? AND state = '未做种'
		   AND (is_hidden = 0 OR is_hidden IS NULL)
		 ORDER BY last_seen DESC
		 LIMIT 1`,
		name, size, downloaderID, siteNickname,
	).Scan(&row).Error; err != nil {
		return "", false, err
	}

	oldHash := strings.TrimSpace(row.Hash)
	if oldHash == "" {
		return "", false, nil
	}
	if oldHash == newHash {
		return oldHash, false, nil
	}

	updated := false
	txErr := r.store.DB.Transaction(func(tx *gorm.DB) error {
		conflict := struct {
			Count int64 `gorm:"column:cnt"`
		}{}
		if err := tx.Raw(
			`SELECT COUNT(1) AS cnt
			 FROM torrents
			 WHERE hash = ? AND downloader_id = ?
			   AND (is_hidden = 0 OR is_hidden IS NULL)`,
			newHash, downloaderID,
		).Scan(&conflict).Error; err != nil {
			return err
		}
		if conflict.Count > 0 {
			return fmt.Errorf("目标 hash 已存在，无法替换 old_hash=%s new_hash=%s downloader_id=%s", oldHash, newHash, downloaderID)
		}

		result := tx.Exec(
			`UPDATE torrents
			 SET hash = ?
			 WHERE hash = ? AND downloader_id = ? AND name = ? AND size = ? AND sites = ? AND state = '未做种'
			   AND (is_hidden = 0 OR is_hidden IS NULL)`,
			newHash, oldHash, downloaderID, name, size, siteNickname,
		)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		updated = true

		if err := tx.Exec(
			`UPDATE torrent_upload_stats
			 SET hash = ?
			 WHERE hash = ? AND downloader_id = ?
			   AND (is_hidden = 0 OR is_hidden IS NULL)`,
			newHash, oldHash, downloaderID,
		).Error; err != nil {
			return err
		}
		return nil
	})
	if txErr != nil {
		return oldHash, false, txErr
	}
	return oldHash, updated, nil
}

type siteGroupDescriptionRow struct {
	Description *string `gorm:"column:description"`
	GroupValue  *string `gorm:"column:group_value"`
}

// ListSitesGroupAndDescription 列出 sites 表中用于“官组致谢声明”匹配的字段。
// 参数/返回：无入参；返回每条站点的 group 与 description（允许为空）。
// 失败场景：数据库查询失败时返回 error。
// 副作用：无副作用，仅读取 sites 表。
func (r *MigrateRepository) ListSitesGroupAndDescription() ([]map[string]any, error) {
	rows := make([]siteGroupDescriptionRow, 0)
	groupColumn := r.store.GroupColumn()
	query := "SELECT description, " + groupColumn + " AS group_value FROM sites"
	if err := r.store.DB.Raw(query).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		desc := ""
		group := ""
		if row.Description != nil {
			desc = strings.TrimSpace(*row.Description)
		}
		if row.GroupValue != nil {
			group = strings.TrimSpace(*row.GroupValue)
		}
		out = append(out, map[string]any{
			"description": desc,
			"group":       group,
		})
	}
	return out, nil
}

type siteNicknameGroupRow struct {
	Nickname   string  `gorm:"column:nickname"`
	GroupValue *string `gorm:"column:group_value"`
}

// FindSiteNicknameByGroup 根据制作组名称匹配 sites.group，并返回命中的站点 nickname。
// 参数/返回：releaseGroup 为制作组名（支持包含 '@' 或前导 '-'）；命中返回 nickname，未命中返回空字符串。
// 失败场景：数据库查询失败时返回 error。
// 副作用：无副作用，仅读取 sites 表。
func (r *MigrateRepository) FindSiteNicknameByGroup(releaseGroup string) (string, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return "", errors.New("repo is nil")
	}

	normalized := normalizeReleaseGroup(releaseGroup)
	if normalized == "" {
		return "", nil
	}

	rows := make([]siteNicknameGroupRow, 0)
	groupColumn := r.store.GroupColumn()
	query := "SELECT nickname, " + groupColumn + " AS group_value FROM sites"
	if err := r.store.DB.Raw(query).Scan(&rows).Error; err != nil {
		return "", err
	}

	for _, row := range rows {
		if row.GroupValue == nil || strings.TrimSpace(*row.GroupValue) == "" {
			continue
		}
		if groupMatches(*row.GroupValue, normalized) {
			return strings.TrimSpace(row.Nickname), nil
		}
	}

	return "", nil
}

func normalizeReleaseGroup(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	if strings.Contains(v, "@") {
		parts := strings.Split(v, "@")
		if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
			v = strings.TrimSpace(parts[1])
		}
	}
	return strings.TrimSpace(strings.TrimLeft(v, "-"))
}

func groupMatches(siteGroup string, releaseGroup string) bool {
	sg := strings.TrimSpace(siteGroup)
	rg := strings.TrimSpace(releaseGroup)
	if sg == "" || rg == "" {
		return false
	}

	parts := strings.FieldsFunc(sg, func(r rune) bool {
		switch r {
		case '/', '\\', '|', ',', ' ', '\t', '\n', '\r':
			return true
		default:
			return false
		}
	})
	for _, part := range parts {
		cleaned := strings.TrimSpace(strings.TrimLeft(part, "-"))
		if cleaned == "" {
			continue
		}
		if strings.EqualFold(cleaned, rg) {
			return true
		}
	}
	return false
}

func (r *MigrateRepository) GetSiteByName(name string) (map[string]any, error) {
	row := map[string]any{}
	err := r.store.DB.Table("sites").Where("nickname = ? OR site = ?", name, name).Limit(1).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if len(row) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return row, nil
}

func (r *MigrateRepository) GetSeedParameter(torrentID, siteName string) (map[string]any, error) {
	row := map[string]any{}
	err := r.store.DB.Table("seed_parameters").Where("torrent_id = ? AND (site_name = ? OR nickname = ?)", torrentID, siteName, siteName).Order("updated_at DESC").Limit(1).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if len(row) == 0 {
		lowered := strings.ToLower(strings.TrimSpace(siteName))
		if lowered != "" {
			err = r.store.DB.Table("seed_parameters").Where("torrent_id = ? AND (LOWER(site_name) = ? OR LOWER(nickname) = ?)", torrentID, lowered, lowered).Order("updated_at DESC").Limit(1).Scan(&row).Error
			if err != nil {
				return nil, err
			}
		}
	}
	if len(row) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return row, nil
}

func (r *MigrateRepository) GetSeedParameterByKey(hash, torrentID, siteName string) (map[string]any, error) {
	row := map[string]any{}
	err := r.store.DB.Table("seed_parameters").Where("hash = ? AND torrent_id = ? AND site_name = ?", hash, torrentID, siteName).Limit(1).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if len(row) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return row, nil
}

func (r *MigrateRepository) GetSeedParametersByName(name string) ([]map[string]any, error) {
	rows := make([]map[string]any, 0)
	err := r.store.DB.Table("seed_parameters").
		Where("name = ?", name).
		Order("updated_at DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *MigrateRepository) GetCurrentTorrentByName(name string) (map[string]any, error) {
	row := map[string]any{}
	err := r.store.DB.Raw(`
		SELECT t.hash, t.name, t.save_path, t.downloader_id, t.sites, t.details, t.size
		FROM torrents t
		JOIN (
			SELECT name, MAX(last_seen) AS max_last_seen
			FROM torrents
			WHERE name = ? AND (is_hidden = 0 OR is_hidden IS NULL)
			GROUP BY name
		) latest ON latest.name = t.name AND latest.max_last_seen = t.last_seen
		WHERE (t.is_hidden = 0 OR t.is_hidden IS NULL)
		LIMIT 1
	`, name).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if len(row) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return row, nil
}

func (r *MigrateRepository) GetCurrentTorrentByHash(hash string) (map[string]any, error) {
	row := map[string]any{}
	err := r.store.DB.Raw(`
		SELECT t.hash, t.name, t.save_path, t.downloader_id, t.sites, t.details, t.size
		FROM torrents t
		JOIN (
			SELECT hash, MAX(last_seen) AS max_last_seen
			FROM torrents
			WHERE hash = ? AND (is_hidden = 0 OR is_hidden IS NULL)
			GROUP BY hash
		) latest ON latest.hash = t.hash AND latest.max_last_seen = t.last_seen
		WHERE (t.is_hidden = 0 OR t.is_hidden IS NULL)
		LIMIT 1
	`, hash).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if len(row) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return row, nil
}

func (r *MigrateRepository) UpsertSeedParameter(record map[string]any) error {
	torrentID, ok1 := record["torrent_id"]
	siteName, ok2 := record["site_name"]
	if !ok1 || !ok2 {
		return errors.New("missing torrent_id or site_name")
	}
	return r.store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("seed_parameters").Where("torrent_id = ? AND site_name = ?", torrentID, siteName).Delete(nil).Error; err != nil {
			return err
		}
		return tx.Table("seed_parameters").Create(record).Error
	})
}

func (r *MigrateRepository) UpdateSeedParameterByKey(hash, torrentID, siteName string, updates map[string]any) error {
	return r.store.DB.Table("seed_parameters").Where("hash = ? AND torrent_id = ? AND site_name = ?", hash, torrentID, siteName).Updates(updates).Error
}

func (r *MigrateRepository) ListTorrentsByNames(names []string) ([]map[string]any, error) {
	rows := make([]map[string]any, 0)
	if len(names) == 0 {
		return rows, nil
	}
	err := r.store.DB.Table("torrents").Select("name, save_path, size, downloader_id, sites, details, hash, state").Where("name IN ? AND state != ? AND (is_hidden = 0 OR is_hidden IS NULL)", names, "不存在").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *MigrateRepository) ListSitesStatus() ([]map[string]any, error) {
	rows := make([]map[string]any, 0)
	err := r.store.DB.Table("sites").Select("nickname, site, cookie, passkey, migration").Where("nickname IS NOT NULL AND nickname != ''").Order("nickname").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *MigrateRepository) ListBDInfoRecords(statusFilter string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 500
	}
	db := r.store.DB.Table("seed_parameters").Select("hash, torrent_id, site_name, title, nickname, mediainfo_status, bdinfo_task_id, bdinfo_started_at, bdinfo_completed_at, bdinfo_error, mediainfo")
	if statusFilter != "" {
		db = db.Where("mediainfo_status = ?", statusFilter)
	}
	rows := make([]map[string]any, 0)
	err := db.Order("updated_at DESC").Limit(limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
