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

	var sitesWithCookie []string
	if err := r.store.DB.Raw("SELECT nickname FROM sites WHERE cookie IS NOT NULL AND cookie != ''").Pluck("nickname", &sitesWithCookie).Error; err != nil {
		return nil, err
	}
	for _, site := range sitesWithCookie {
		site = strings.TrimSpace(site)
		if site != "" {
			sitesSet[site] = struct{}{}
		}
	}

	result := make([]string, 0, len(sitesSet))
	for site := range sitesSet {
		result = append(result, site)
	}
	sort.Strings(result)
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
	query := `SELECT hash, name, save_path, size, progress, state, sites, ` + groupColumn + ` AS torrent_group, details, downloader_id AS downloader, last_seen, iyuu_last_check AS iyuu_last, seeders
		FROM torrents t
		WHERE t.state != ? AND (t.is_hidden = 0 OR t.is_hidden IS NULL)
		  AND EXISTS (
			SELECT 1 FROM seed_parameters sp
			WHERE sp.hash = t.hash
			  AND sp.type IN ('category.movie', 'category.tv_series', 'category.animation', 'category.documentaries', 'category.tv_shows')
		  )`
	args := []any{"不存在"}
	if onlyCompleted {
		query += " AND t.progress >= 100"
	}

	rows := make([]TorrentRecord, 0)
	if err := r.store.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
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
