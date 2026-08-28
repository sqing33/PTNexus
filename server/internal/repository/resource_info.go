package repository

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// ResourceInfo 表示资源信息库中的一条影视资源元数据。
// 用于按 豆瓣ID / IMDbID / TMDbID 复用标题、年份、国家、海报与简介，
// 避免同一资源重复走源站抓取与修复流程。
type ResourceInfo struct {
	ID          int64  `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Title       string `gorm:"column:title" json:"title"`
	Year        string `gorm:"column:year" json:"year"`
	Country     string `gorm:"column:country" json:"country"`
	DoubanID    string `gorm:"column:douban_id" json:"douban_id"`
	ImdbID      string `gorm:"column:imdb_id" json:"imdb_id"`
	TmdbID      string `gorm:"column:tmdb_id" json:"tmdb_id"`
	PosterURL   string `gorm:"column:poster_url" json:"poster_url"`
	Summary     string `gorm:"column:summary" json:"summary"`
	Screenshots string `gorm:"column:screenshots" json:"screenshots"`
	CreatedAt   string `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   string `gorm:"column:updated_at" json:"updated_at"`
}

// TableName 指定 ResourceInfo 对应的数据表名。
func (ResourceInfo) TableName() string { return "resource_info" }

// posterBBCodePattern 匹配 BBCode 图片标签 [img]url[/img] 或 [img=width]url[/img]。
var posterBBCodePattern = regexp.MustCompile(`(?i)\[img(?:\=[^\]]*)?\](.*?)\[/img\]`)

// normalizePosterURL 从海报文本中提取纯 URL。
// PT 站点常把海报存成 BBCode 形式（如 [img]https://x.jpg[/img]），
// 直接作为 <img src> 会失效；本函数剥离标签、回退为内部 URL，无法解析时返回原文本。
// 参数/返回：raw 为原始海报文本；返回可直链使用的 URL 或空字符串。
// 失败场景：无。
// 副作用：无。
func normalizePosterURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if m := posterBBCodePattern.FindStringSubmatch(raw); m != nil {
		if inner := strings.TrimSpace(m[1]); inner != "" {
			return inner
		}
	}
	return raw
}

// FindResourceInfoByDoubanID 按豆瓣 ID 查询资源信息。
// 参数/返回：doubanID 为豆瓣 subject 数字 ID；命中返回记录，未命中返回 nil。
// 失败场景：仓储未初始化或数据库查询失败时返回错误。
// 副作用：仅读取 resource_info 表。
func (r *MigrateRepository) FindResourceInfoByDoubanID(doubanID string) (*ResourceInfo, error) {
	return r.findResourceInfoByColumn("douban_id", doubanID)
}

// FindResourceInfoByImdbID 按 IMDb ID（tt 开头）查询资源信息。
// 参数/返回：imdbID 为 tt+数字；命中返回记录，未命中返回 nil。
// 失败场景：仓储未初始化或数据库查询失败时返回错误。
// 副作用：仅读取 resource_info 表。
func (r *MigrateRepository) FindResourceInfoByImdbID(imdbID string) (*ResourceInfo, error) {
	return r.findResourceInfoByColumn("imdb_id", imdbID)
}

// FindResourceInfoByTmdbID 按 TMDb ID 查询资源信息。
// 参数/返回：tmdbID 为 TMDB 数字 ID；命中返回记录，未命中返回 nil。
// 失败场景：仓储未初始化或数据库查询失败时返回错误。
// 副作用：仅读取 resource_info 表。
func (r *MigrateRepository) FindResourceInfoByTmdbID(tmdbID string) (*ResourceInfo, error) {
	return r.findResourceInfoByColumn("tmdb_id", tmdbID)
}

// UpsertResourceInfo 按“任一 ID 命中即视为同一资源”的策略写入资源信息。
// 参数/返回：info 为待写入资源；仅当库中不存在时才插入新记录；
// 若资源已存在则**直接复用库内数据、不回写、不更新任何字段**（已存在则不修改）。
// 失败场景：仓储未初始化、查询失败或写入失败时返回错误。
// 副作用：仅当资源不存在时写入 resource_info 表（INSERT），存在时不产生任何写操作。
func (r *MigrateRepository) UpsertResourceInfo(info *ResourceInfo) error {
	if r == nil || r.store == nil || r.store.DB == nil {
		return errors.New("migrate repo is nil")
	}
	if info == nil {
		return nil
	}
	info.Title = strings.TrimSpace(info.Title)
	info.Year = strings.TrimSpace(info.Year)
	info.Country = strings.TrimSpace(info.Country)
	info.DoubanID = strings.TrimSpace(info.DoubanID)
	info.ImdbID = strings.TrimSpace(info.ImdbID)
	info.TmdbID = strings.TrimSpace(info.TmdbID)
	info.PosterURL = normalizePosterURL(info.PosterURL)
	info.Summary = strings.TrimSpace(info.Summary)
	info.Screenshots = strings.TrimSpace(info.Screenshots)

	type finder struct {
		id     string
		lookup func(string) (*ResourceInfo, error)
	}
	lookups := []finder{
		{info.DoubanID, r.FindResourceInfoByDoubanID},
		{info.ImdbID, r.FindResourceInfoByImdbID},
		{info.TmdbID, r.FindResourceInfoByTmdbID},
	}
	var existing *ResourceInfo
	for _, item := range lookups {
		if item.id == "" {
			continue
		}
		row, err := item.lookup(item.id)
		if err != nil {
			return err
		}
		if row != nil {
			existing = row
			break
		}
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	if existing == nil {
		info.CreatedAt = now
		info.UpdatedAt = now
		info.ID = 0
		return r.store.DB.Create(info).Error
	}

	// 资源已存在：直接复用库内数据，不回写、不更新任何字段、也不把本次抓取信息合并回库内记录。
	// 仅在真正不存在时才插入新记录，从而保证“已存在则不修改”。
	return nil
}

// UpdateResourceInfo 按主键 ID 更新资源信息的可编辑字段（标题/年份/国家/三个 ID/海报/简介/截图），
// 并更新 updated_at。用于前端“修改资源信息”功能。
// 参数/返回：info 为含 ID 与待更新字段的资源；ID 无效或仓储未初始化时返回错误。
// 失败场景：数据库写入失败时返回错误。
// 副作用：写入 resource_info 表（UPDATE）。
func (r *MigrateRepository) UpdateResourceInfo(info *ResourceInfo) error {
	if r == nil || r.store == nil || r.store.DB == nil {
		return errors.New("migrate repo is nil")
	}
	if info == nil || info.ID <= 0 {
		return errors.New("资源 ID 无效")
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	updates := map[string]any{
		"title":       strings.TrimSpace(info.Title),
		"year":        strings.TrimSpace(info.Year),
		"country":     strings.TrimSpace(info.Country),
		"douban_id":   strings.TrimSpace(info.DoubanID),
		"imdb_id":     strings.TrimSpace(info.ImdbID),
		"tmdb_id":     strings.TrimSpace(info.TmdbID),
		"poster_url":  normalizePosterURL(info.PosterURL),
		"summary":     strings.TrimSpace(info.Summary),
		"screenshots": strings.TrimSpace(info.Screenshots),
		"updated_at":  now,
	}
	return r.store.DB.Table("resource_info").Where("id = ?", info.ID).Updates(updates).Error
}

// ListResourceInfos 分页查询资源信息列表，支持按标题/ID/国家模糊搜索。
// 参数/返回：keyword 为搜索关键字；page/pageSize 为分页参数；返回记录列表与总数。
// 失败场景：仓储未初始化或数据库查询失败时返回错误。
// 副作用：仅读取 resource_info 表。
func (r *MigrateRepository) ListResourceInfos(keyword string, page, pageSize int) ([]ResourceInfo, int64, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return nil, 0, errors.New("migrate repo is nil")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}

	query := r.store.DB.Table("resource_info")
	if trimmed := strings.TrimSpace(keyword); trimmed != "" {
		like := "%" + trimmed + "%"
		query = query.Where(
			"title LIKE ? OR country LIKE ? OR douban_id LIKE ? OR imdb_id LIKE ? OR tmdb_id LIKE ?",
			like, like, like, like, like,
		)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	rows := make([]ResourceInfo, 0, pageSize)
	offset := (page - 1) * pageSize
	if err := query.Order("updated_at DESC, id DESC").Limit(pageSize).Offset(offset).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	for i := range rows {
		rows[i].PosterURL = normalizePosterURL(rows[i].PosterURL)
	}
	return rows, total, nil
}

func (r *MigrateRepository) findResourceInfoByColumn(column, value string) (*ResourceInfo, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return nil, errors.New("migrate repo is nil")
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	rows := make([]ResourceInfo, 0, 1)
	if err := r.store.DB.Table("resource_info").Where(column+" = ?", trimmed).Limit(1).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	rows[0].PosterURL = normalizePosterURL(rows[0].PosterURL)
	return &rows[0], nil
}
