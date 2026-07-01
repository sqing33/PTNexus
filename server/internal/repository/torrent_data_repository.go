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

// TorrentListFilters 表示种子聚合列表在数据库层可提前收敛的筛选条件。
// 参数/返回：由 Service 层按接口查询参数填充，Repository 使用这些条件减少返回行数。
// 失败场景：结构体本身不产生错误。
// 副作用：无。
type TorrentListFilters struct {
	OnlyCompleted     bool
	NameSearch        string
	PathFilters       []string
	StateFilters      []string
	DownloaderFilters []string
	ExcludeExisting   bool
}

// TorrentNameSizeKey 表示按种子名与体积聚合后的唯一业务分组。
type TorrentNameSizeKey struct {
	Name string `gorm:"column:name"`
	Size int64  `gorm:"column:size"`
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

// ListTorrents 读取当前可展示的下载器种子记录。
// 参数/返回：onlyCompleted 为 true 时保留“同 name+size 组内存在完成记录”的全组数据；返回值按数据库原始行返回，聚合由 Service 层完成。
// 失败场景：数据库查询失败时返回错误。
// 副作用：仅读取 torrents/seed_parameters，不写入数据。
func (r *TorrentDataRepository) ListTorrents(onlyCompleted bool) ([]TorrentRecord, error) {
	return r.ListTorrentsWithFilters(TorrentListFilters{OnlyCompleted: onlyCompleted})
}

// ListTorrentsWithFilters 按维护列表查询条件读取可展示的下载器种子记录。
// 参数/返回：filters 中的名称、路径、状态、下载器和已存在排除会在 SQL 层先收敛；返回值仍是原始 torrents 行，聚合由 Service 层完成。
// 失败场景：数据库查询失败时返回错误。
// 副作用：仅读取 torrents/seed_parameters，不写入数据。
func (r *TorrentDataRepository) ListTorrentsWithFilters(filters TorrentListFilters) ([]TorrentRecord, error) {
	groupColumn := r.store.GroupColumn()
	query := `SELECT hash, name, save_path, size, progress, state, sites, ` + groupColumn + ` AS torrent_group, details, downloader_id AS downloader, last_seen, iyuu_last_check AS iyuu_last, seeders
		FROM torrents t
		WHERE t.state != ? AND (t.is_hidden = 0 OR t.is_hidden IS NULL)
		  AND (
			NOT EXISTS (
				SELECT 1 FROM seed_parameters sp_any
				WHERE sp_any.hash = t.hash
			)
			OR EXISTS (
				SELECT 1 FROM seed_parameters sp
				WHERE sp.hash = t.hash
				  AND sp.type IN ('category.movie', 'category.tv_series', 'category.animation', 'category.documentaries', 'category.tv_shows')
			)
		  )`
	args := []any{"不存在"}

	if filters.OnlyCompleted {
		appendCompletedTorrentGroupFilter(&query, &args)
	}

	nameSearch := strings.ToLower(strings.TrimSpace(filters.NameSearch))
	if nameSearch != "" {
		query += " AND LOWER(t.name) LIKE ?"
		args = append(args, "%"+nameSearch+"%")
	}

	appendTorrentGroupStringFilter(&query, &args, "path_match", "save_path", filters.PathFilters)
	appendTorrentGroupStringFilter(&query, &args, "state_match", "state", filters.StateFilters)
	appendTorrentGroupStringFilter(&query, &args, "downloader_match", "downloader_id", filters.DownloaderFilters)

	if filters.ExcludeExisting {
		query += ` AND NOT EXISTS (
			SELECT 1 FROM seed_parameters sp_existing
			INNER JOIN torrents t_existing ON t_existing.hash = sp_existing.hash
			WHERE sp_existing.name = t.name
			  AND sp_existing.name IS NOT NULL
			  AND TRIM(sp_existing.name) != ''
			  AND t_existing.size = t.size
			  AND t_existing.size > 0
			  AND (t_existing.is_hidden = 0 OR t_existing.is_hidden IS NULL)
		)`
	}

	rows := make([]TorrentRecord, 0)
	if err := r.store.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListTorrentFilterOptions 读取列表筛选器需要的路径和状态候选值。
// 参数/返回：onlyCompleted 为 true 时仅统计存在完成记录的 name+size 分组；返回去重排序后的保存路径和状态。
// 失败场景：数据库查询失败时返回错误。
// 副作用：仅读取 torrents/seed_parameters，不写入数据。
func (r *TorrentDataRepository) ListTorrentFilterOptions(onlyCompleted bool) ([]string, []string, error) {
	baseQuery := ` FROM torrents t
		WHERE t.state != ? AND (t.is_hidden = 0 OR t.is_hidden IS NULL)
		  AND (
			NOT EXISTS (
				SELECT 1 FROM seed_parameters sp_any
				WHERE sp_any.hash = t.hash
			)
			OR EXISTS (
				SELECT 1 FROM seed_parameters sp
				WHERE sp.hash = t.hash
				  AND sp.type IN ('category.movie', 'category.tv_series', 'category.animation', 'category.documentaries', 'category.tv_shows')
			)
		  )`
	args := []any{"不存在"}
	if onlyCompleted {
		appendCompletedTorrentGroupFilter(&baseQuery, &args)
	}

	pathArgs := append([]any{}, args...)
	paths := make([]string, 0)
	if err := r.store.DB.Raw(
		`SELECT DISTINCT save_path`+baseQuery+` AND save_path IS NOT NULL AND TRIM(save_path) != '' ORDER BY save_path ASC`,
		pathArgs...,
	).Pluck("save_path", &paths).Error; err != nil {
		return nil, nil, err
	}

	stateArgs := append([]any{}, args...)
	states := make([]string, 0)
	if err := r.store.DB.Raw(
		`SELECT DISTINCT state`+baseQuery+` AND state IS NOT NULL AND TRIM(state) != '' ORDER BY state ASC`,
		stateArgs...,
	).Pluck("state", &states).Error; err != nil {
		return nil, nil, err
	}

	return paths, states, nil
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

// UploadTotalsByHashes 按指定 hash 集合汇总上传量。
// 参数/返回：hashes 为空时直接返回空 map；返回 map[hash]uploaded 供列表聚合展示。
// 失败场景：数据库查询失败时返回错误。
// 副作用：仅读取 torrent_upload_stats，不写入数据。
func (r *TorrentDataRepository) UploadTotalsByHashes(hashes []string) (map[string]int64, error) {
	cleaned := cleanStringValues(hashes)
	if len(cleaned) == 0 {
		return map[string]int64{}, nil
	}

	result := map[string]int64{}
	const chunkSize = 500
	for start := 0; start < len(cleaned); start += chunkSize {
		end := start + chunkSize
		if end > len(cleaned) {
			end = len(cleaned)
		}
		rows := make([]TorrentUploadTotal, 0)
		if err := r.store.DB.Table("torrent_upload_stats").
			Select("hash, SUM(uploaded) AS total_uploaded").
			Where("(is_hidden = 0 OR is_hidden IS NULL)").
			Where("hash IN ?", cleaned[start:end]).
			Group("hash").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[row.Hash] = row.TotalUploaded
		}
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

// ExistingSeedParameterGroups 查询已存在 seed_parameters 的 name+size 分组。
// 参数/返回：返回 seed_parameters 关联 torrents 后得到的去重 name+size，用于避免同名不同体积互相误排除。
// 失败场景：数据库查询失败时返回错误。
// 副作用：仅读取 seed_parameters/torrents，不写入数据。
func (r *TorrentDataRepository) ExistingSeedParameterGroups() ([]TorrentNameSizeKey, error) {
	rows := make([]TorrentNameSizeKey, 0)
	err := r.store.DB.Raw(`
		SELECT DISTINCT sp.name AS name, t.size AS size
		FROM seed_parameters sp
		INNER JOIN torrents t ON t.hash = sp.hash
		WHERE sp.name IS NOT NULL
		  AND TRIM(sp.name) != ''
		  AND t.size > 0
		  AND (t.is_hidden = 0 OR t.is_hidden IS NULL)
	`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
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

func appendCompletedTorrentGroupFilter(query *string, args *[]any) {
	*query += ` AND EXISTS (
		SELECT 1 FROM torrents completed
		WHERE completed.name = t.name
		  AND completed.size = t.size
		  AND completed.state != ?
		  AND (completed.is_hidden = 0 OR completed.is_hidden IS NULL)
		  AND completed.progress >= 100
	)`
	*args = append(*args, "不存在")
}

func appendTorrentGroupStringFilter(query *string, args *[]any, alias string, column string, values []string) {
	cleaned := cleanStringValues(values)
	if len(cleaned) == 0 {
		return
	}

	*query += ` AND EXISTS (
		SELECT 1 FROM torrents ` + alias + `
		WHERE ` + alias + `.name = t.name
		  AND ` + alias + `.size = t.size
		  AND ` + alias + `.state != ?
		  AND (` + alias + `.is_hidden = 0 OR ` + alias + `.is_hidden IS NULL)
		  AND ` + alias + `.` + column + ` IN (` + sqlPlaceholders(len(cleaned)) + `)
	)`
	*args = append(*args, "不存在")
	for _, value := range cleaned {
		*args = append(*args, value)
	}
}

func cleanStringValues(values []string) []string {
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

func sqlPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	parts := make([]string, count)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}
