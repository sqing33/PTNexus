package repository

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

type SiteConfig struct {
	Nickname  string
	Migration int
	Cookie    string
}

type SiteLinkRuleRow struct {
	Nickname string `gorm:"column:nickname"`
	BaseURL  string `gorm:"column:base_url"`
}

type TorrentRecord struct {
	Hash         string
	Name         string
	SavePath     string
	Size         int64
	Progress     float64
	State        string
	Sites        string
	TorrentGroup string
	Details      *string
	Downloader   string
	LastSeen     string
	IYUULast     string
	Seeders      int64
}

type TorrentUploadTotal struct {
	Hash          string
	TotalUploaded int64
}

type TorrentDataRepository struct {
	store *Store
}

type SeedParameterSourceStatus struct {
	HasRecord            bool
	HasFetchedSourceData bool
	IsReviewed           bool
}

func NewTorrentDataRepository(store *Store) *TorrentDataRepository {
	return &TorrentDataRepository{store: store}
}

func (r *TorrentDataRepository) ListSiteConfigs() ([]SiteConfig, error) {
	rows := make([]SiteConfig, 0)
	err := r.store.DB.Raw("SELECT nickname, migration, cookie FROM sites").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *TorrentDataRepository) SiteLinkRules() (map[string]any, error) {
	rows := make([]SiteLinkRuleRow, 0)
	if err := r.store.DB.Raw("SELECT nickname, base_url FROM sites").Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := map[string]any{}
	for _, row := range rows {
		nickname := strings.TrimSpace(row.Nickname)
		baseURL := strings.TrimSpace(row.BaseURL)
		if nickname == "" || baseURL == "" {
			continue
		}
		result[nickname] = map[string]any{"base_url": baseURL}
	}
	return result, nil
}

func (r *TorrentDataRepository) ListAllDiscoveredSites() ([]string, error) {
	sitesSet := map[string]struct{}{}

	var torrentSites []string
	if err := r.store.DB.Raw("SELECT DISTINCT sites FROM torrents WHERE sites IS NOT NULL AND sites != '' AND (is_hidden = 0 OR is_hidden IS NULL)").Pluck("sites", &torrentSites).Error; err != nil {
		return nil, err
	}
	for _, site := range torrentSites {
		site = strings.TrimSpace(site)
		if site != "" {
			sitesSet[site] = struct{}{}
		}
	}

	// 从 sites 表读取排序序号
	type siteOrder struct {
		Nickname  string
		SortOrder int
	}
	var siteOrders []siteOrder
	if err := r.store.DB.Raw("SELECT nickname, sort_order FROM sites").Scan(&siteOrders).Error; err != nil {
		return nil, err
	}
	orderMap := map[string]int{}
	for _, so := range siteOrders {
		name := strings.TrimSpace(so.Nickname)
		if name != "" {
			orderMap[name] = so.SortOrder
		}
	}

	result := make([]string, 0, len(sitesSet))
	for site := range sitesSet {
		result = append(result, site)
	}
	sort.Slice(result, func(i, j int) bool {
		oi, oj := orderMap[result[i]], orderMap[result[j]]
		// sort_order 0 的站点排在最后
		if (oi == 0) != (oj == 0) {
			return oi != 0
		}
		if oi != oj {
			return oi < oj
		}
		return result[i] < result[j]
	})
	return result, nil
}

// ListDistinctTorrentSites 读取 torrents 表中出现过的站点昵称列表（支持逗号分隔的历史格式）。
// 参数/返回：无输入；返回去重后的站点昵称列表。
// 失败场景：数据库查询失败。
// 副作用：读取数据库。
func (r *TorrentDataRepository) ListDistinctTorrentSites() ([]string, error) {
	sitesSet := map[string]struct{}{}
	rawSites := make([]string, 0)
	if err := r.store.DB.Raw("SELECT DISTINCT sites FROM torrents WHERE sites IS NOT NULL AND sites != '' AND (is_hidden = 0 OR is_hidden IS NULL)").Pluck("sites", &rawSites).Error; err != nil {
		return nil, err
	}
	for _, raw := range rawSites {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.Contains(raw, ",") {
			for _, part := range strings.Split(raw, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					sitesSet[part] = struct{}{}
				}
			}
			continue
		}
		sitesSet[raw] = struct{}{}
	}
	result := make([]string, 0, len(sitesSet))
	for site := range sitesSet {
		result = append(result, site)
	}
	sort.Strings(result)
	return result, nil
}

// SiteFieldToNicknameMap 返回 sites 表中 IYUU site 字段到本地 nickname 的映射。
// 参数/返回：无输入；返回 map[sites.site]sites.nickname。
// 失败场景：数据库查询失败。
// 副作用：读取数据库。
func (r *TorrentDataRepository) SiteFieldToNicknameMap() (map[string]string, error) {
	rows := make([]struct {
		Site     string `gorm:"column:site"`
		Nickname string `gorm:"column:nickname"`
	}, 0)
	if err := r.store.DB.Raw("SELECT site, nickname FROM sites WHERE site IS NOT NULL AND site != '' AND nickname IS NOT NULL AND nickname != ''").Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, row := range rows {
		site := strings.TrimSpace(row.Site)
		nick := strings.TrimSpace(row.Nickname)
		if site == "" || nick == "" {
			continue
		}
		result[site] = nick
	}
	return result, nil
}

// IYUUTorrentRow 表示 IYUU 查询所需的 torrents 行字段（只读）。
type IYUUTorrentRow struct {
	Hash       string  `gorm:"column:hash"`
	Name       string  `gorm:"column:name"`
	SavePath   *string `gorm:"column:save_path"`
	Size       int64   `gorm:"column:size"`
	State      *string `gorm:"column:state"`
	Sites      *string `gorm:"column:sites"`
	Details    *string `gorm:"column:details"`
	Downloader string  `gorm:"column:downloader_id"`
}

// ListTorrentsByNameAndSizeForIYUU 按 name+size 查询可用于 IYUU 的 torrents 行。
// 参数/返回：pathFilters 为空则不加路径限制；返回匹配的行列表。
// 失败场景：数据库查询失败。
// 副作用：读取数据库。
func (r *TorrentDataRepository) ListTorrentsByNameAndSizeForIYUU(name string, size int64, pathFilters []string) ([]IYUUTorrentRow, error) {
	name = strings.TrimSpace(name)
	if name == "" || size <= 0 {
		return []IYUUTorrentRow{}, nil
	}
	args := []any{name, size, "不存在", int64(207374182)}
	query := "SELECT hash, name, save_path, size, state, sites, details, downloader_id FROM torrents WHERE name = ? AND size = ? AND state != ? AND size > ? AND (is_hidden = 0 OR is_hidden IS NULL)"
	if len(pathFilters) > 0 {
		placeholders := make([]string, 0, len(pathFilters))
		for _, p := range pathFilters {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, p)
		}
		if len(placeholders) > 0 {
			query += " AND save_path IN (" + strings.Join(placeholders, ",") + ")"
		}
	}

	rows := make([]IYUUTorrentRow, 0)
	if err := r.store.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// InsertMissingIYUUSiteTorrents 为缺失站点插入“未做种”的 torrents 记录。
// 参数/返回：matchedSites 为 db站点昵称->详情链接；返回新增记录数。
// 失败场景：数据库写入失败。
// 副作用：写入数据库（INSERT torrents）。
func (r *TorrentDataRepository) InsertMissingIYUUSiteTorrents(name string, size int64, baseSavePath string, baseDownloaderID string, selectedHash string, matchedSites map[string]string, now string) (int, error) {
	if strings.TrimSpace(name) == "" || size <= 0 || strings.TrimSpace(baseDownloaderID) == "" {
		return 0, nil
	}
	if len(matchedSites) == 0 {
		return 0, nil
	}

	// 获取已存在站点集合（支持逗号分隔历史格式）
	existingRaw := make([]string, 0)
	if err := r.store.DB.Raw("SELECT DISTINCT sites FROM torrents WHERE name = ? AND size = ? AND (is_hidden = 0 OR is_hidden IS NULL)", name, size).Pluck("sites", &existingRaw).Error; err != nil {
		return 0, err
	}
	existing := map[string]struct{}{}
	for _, raw := range existingRaw {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.Contains(raw, ",") {
			for _, part := range strings.Split(raw, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					existing[part] = struct{}{}
				}
			}
			continue
		}
		existing[raw] = struct{}{}
	}

	inserted := 0
	err := r.store.DB.Transaction(func(db *gorm.DB) error {
		for site, detailsURL := range matchedSites {
			site = strings.TrimSpace(site)
			if site == "" {
				continue
			}
			if _, ok := existing[site]; ok {
				continue
			}

			unique := selectedHash + "_" + site + "_" + now
			sum := sha1.Sum([]byte(unique))
			newHash := hex.EncodeToString(sum[:])

			if err := db.Exec(
				"INSERT INTO torrents (hash, name, save_path, size, progress, state, sites, details, downloader_id, last_seen, iyuu_last_check, seeders, is_hidden) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
				newHash,
				name,
				baseSavePath,
				size,
				0.0,
				"未做种",
				site,
				strings.TrimSpace(detailsURL),
				baseDownloaderID,
				now,
				now,
				0,
				0,
			).Error; err != nil {
				return err
			}
			inserted++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return inserted, nil
}

// UpdateIYUUCheckAndFillDetails 更新 torrents.iyuu_last_check，并为缺少 details 的记录回填详情链接。
// 参数/返回：matchedSites 为 db站点昵称->详情链接；返回 filledDetails 为回填的 details 条数。
// 失败场景：数据库写入失败。
// 副作用：写入数据库（UPDATE torrents）。
func (r *TorrentDataRepository) UpdateIYUUCheckAndFillDetails(name string, size int64, matchedSites map[string]string, now string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" || size <= 0 {
		return 0, nil
	}
	if now == "" {
		now = time.Now().Format("2006-01-02 15:04:05")
	}

	if err := r.store.DB.Exec("UPDATE torrents SET iyuu_last_check = ? WHERE name = ? AND size = ? AND (is_hidden = 0 OR is_hidden IS NULL)", now, name, size).Error; err != nil {
		return 0, err
	}

	filled := 0
	for site, detailsURL := range matchedSites {
		site = strings.TrimSpace(site)
		if site == "" || strings.TrimSpace(detailsURL) == "" {
			continue
		}
		result := r.store.DB.Exec(
			"UPDATE torrents SET details = ? WHERE name = ? AND size = ? AND sites = ? AND (COALESCE(details, '') = '') AND (is_hidden = 0 OR is_hidden IS NULL)",
			strings.TrimSpace(detailsURL),
			name,
			size,
			site,
		)
		if result.Error != nil {
			return filled, result.Error
		}
		filled += int(result.RowsAffected)
	}
	return filled, nil
}

func (r *TorrentDataRepository) ListTorrents(onlyCompleted bool) ([]TorrentRecord, error) {
	groupColumn := r.store.GroupColumn()
	query := "SELECT hash, name, save_path, size, progress, state, sites, " + groupColumn + " AS torrent_group, details, downloader_id AS downloader, last_seen, iyuu_last_check AS iyuu_last, seeders FROM torrents WHERE state != ? AND (is_hidden = 0 OR is_hidden IS NULL)"
	args := []any{"不存在"}
	if onlyCompleted {
		query += " AND progress >= 100"
	}

	rows := make([]TorrentRecord, 0)
	if err := r.store.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// FindTorrentByHash 按 hash 读取一种多站列表对应的当前种子记录。
// 参数/返回：hash 为下载器种子 hash；返回首条未隐藏 torrents 记录、是否命中以及查询错误。
// 失败场景：数据库查询失败时返回 error；hash 为空或记录不存在时返回 found=false。
// 副作用：只读取数据库，不修改下载器或文件。
func (r *TorrentDataRepository) FindTorrentByHash(hash string) (TorrentRecord, bool, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return TorrentRecord{}, false, nil
	}
	rows := make([]TorrentRecord, 0, 1)
	query := "SELECT hash, name, save_path, size, progress, state, sites, details, downloader_id AS downloader, last_seen, iyuu_last_check AS iyuu_last, seeders FROM torrents WHERE LOWER(TRIM(hash)) = LOWER(?) AND (is_hidden = 0 OR is_hidden IS NULL) ORDER BY last_seen DESC LIMIT 1"
	if err := r.store.DB.Raw(query, hash).Scan(&rows).Error; err != nil {
		return TorrentRecord{}, false, err
	}
	if len(rows) == 0 {
		return TorrentRecord{}, false, nil
	}
	return rows[0], true, nil
}

// DeleteTorrentByHash 删除 hash 对应的一种多站当前记录和上传统计。
// 参数/返回：hash 为 torrents.hash；返回删除的 torrents 记录数与错误。
// 失败场景：数据库删除失败时返回 error。
// 副作用：从 torrents 和 torrent_upload_stats 表中物理删除指定 hash 的数据。
func (r *TorrentDataRepository) DeleteTorrentByHash(hash string) (int64, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return 0, nil
	}
	var deleted int64
	err := r.store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM torrent_upload_stats WHERE LOWER(TRIM(hash)) = LOWER(?)", hash).Error; err != nil {
			return err
		}
		result := tx.Exec("DELETE FROM torrents WHERE LOWER(TRIM(hash)) = LOWER(?)", hash)
		if result.Error != nil {
			return result.Error
		}
		deleted = result.RowsAffected
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

func (r *TorrentDataRepository) UploadTotalsByHash() (map[string]int64, error) {
	rows := make([]TorrentUploadTotal, 0)
	if err := r.store.DB.Raw("SELECT hash, SUM(uploaded) AS total_uploaded FROM torrent_upload_stats WHERE (is_hidden = 0 OR is_hidden IS NULL) GROUP BY hash").Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := map[string]int64{}
	for _, row := range rows {
		result[row.Hash] = row.TotalUploaded
	}
	return result, nil
}

func (r *TorrentDataRepository) DistinctSeedParameterNames() ([]string, error) {
	result := make([]string, 0)
	if err := r.store.DB.Table("seed_parameters").Distinct("name").Pluck("name", &result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (r *TorrentDataRepository) CachedSitesByNameAndSize(name string, size int64) ([]string, []string, error) {
	seedHashes := make([]string, 0)
	if err := r.store.DB.Table("seed_parameters").Distinct("hash").Where("name = ?", name).Pluck("hash", &seedHashes).Error; err != nil {
		return nil, nil, err
	}
	if len(seedHashes) == 0 {
		return []string{}, []string{}, nil
	}

	matchedHashes := make([]string, 0)
	if err := r.store.DB.Table("torrents").Distinct("hash").Where("hash IN ? AND size = ? AND (is_hidden = 0 OR is_hidden IS NULL)", seedHashes, size).Pluck("hash", &matchedHashes).Error; err != nil {
		return nil, nil, err
	}
	if len(matchedHashes) == 0 {
		return []string{}, []string{}, nil
	}

	cachedSites := make([]string, 0)
	if err := r.store.DB.Table("seed_parameters").Distinct("nickname").Where("hash IN ?", matchedHashes).Pluck("nickname", &cachedSites).Error; err != nil {
		return nil, nil, err
	}
	sort.Strings(cachedSites)
	sort.Strings(matchedHashes)
	return cachedSites, matchedHashes, nil
}

type namePublishAt struct {
	Name      string  `gorm:"column:name"`
	PublishAt *string `gorm:"column:publish_at"`
}

type nameSeedParameterSourceStatus struct {
	Name     string `gorm:"column:name"`
	Fetched  int    `gorm:"column:fetched"`
	Reviewed int    `gorm:"column:reviewed"`
}

func (r *TorrentDataRepository) PublishAtByNames() (map[string]string, error) {
	rows := make([]namePublishAt, 0)
	if err := r.store.DB.Raw("SELECT name, MIN(publish_at) AS publish_at FROM seed_parameters WHERE name IS NOT NULL AND name != '' AND publish_at IS NOT NULL GROUP BY name").Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, row := range rows {
		if row.PublishAt != nil && *row.PublishAt != "" {
			result[row.Name] = *row.PublishAt
		}
	}
	return result, nil
}

func (r *TorrentDataRepository) SeedParameterSourceStatusByNames() (map[string]SeedParameterSourceStatus, error) {
	rows := make([]nameSeedParameterSourceStatus, 0)
	query := `
		SELECT name,
		       MAX(CASE WHEN (
		         COALESCE(title, '') != '' OR COALESCE(subtitle, '') != '' OR
		         COALESCE(type, '') != '' OR COALESCE(medium, '') != '' OR
		         COALESCE(video_codec, '') != '' OR COALESCE(audio_codec, '') != '' OR
		         COALESCE(resolution, '') != '' OR COALESCE(team, '') != '' OR
		         COALESCE(source, '') != '' OR COALESCE(tags, '') NOT IN ('', '[]', 'null') OR
		         COALESCE(title_components, '') NOT IN ('', '[]', 'null') OR
		         COALESCE(poster, '') != '' OR COALESCE(screenshots, '') != '' OR
		         COALESCE(statement, '') != '' OR COALESCE(body, '') != '' OR
		         COALESCE(mediainfo, '') != '' OR COALESCE(imdb_link, '') != '' OR
		         COALESCE(douban_link, '') != '' OR COALESCE(tmdb_link, '') != ''
		       ) THEN 1 ELSE 0 END) AS fetched,
		       MAX(CASE WHEN is_reviewed THEN 1 ELSE 0 END) AS reviewed
		FROM seed_parameters
		WHERE name IS NOT NULL AND name != ''
		GROUP BY name
	`
	if err := r.store.DB.Raw(query).Scan(&rows).Error; err != nil {
		return nil, err
	}

	result := map[string]SeedParameterSourceStatus{}
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		result[name] = SeedParameterSourceStatus{
			HasRecord:            true,
			HasFetchedSourceData: row.Fetched > 0 || row.Reviewed > 0,
			IsReviewed:           row.Reviewed > 0,
		}
	}
	return result, nil
}
func (r *TorrentDataRepository) UpdatePublishAtByName(name string, publishAt any) (int64, error) {
	result := r.store.DB.Exec("UPDATE seed_parameters SET publish_at = ? WHERE name = ?", publishAt, name)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
