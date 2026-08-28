package repository

import (
	"errors"
	"strings"
	"time"
)

// ResourceInfo 表示资源信息库中的一条影视资源元数据。
// 用于按 豆瓣ID / IMDbID / TMDbID 复用标题、年份、国家、海报与简介，
// 避免同一资源重复走源站抓取与修复流程。
type ResourceInfo struct {
	ID        int64  `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Title     string `gorm:"column:title" json:"title"`
	Year      string `gorm:"column:year" json:"year"`
	Country   string `gorm:"column:country" json:"country"`
	DoubanID  string `gorm:"column:douban_id" json:"douban_id"`
	ImdbID    string `gorm:"column:imdb_id" json:"imdb_id"`
	TmdbID    string `gorm:"column:tmdb_id" json:"tmdb_id"`
	PosterURL string `gorm:"column:poster_url" json:"poster_url"`
	Summary   string `gorm:"column:summary" json:"summary"`
	CreatedAt string `gorm:"column:created_at" json:"created_at"`
	UpdatedAt string `gorm:"column:updated_at" json:"updated_at"`
}

// TableName 指定 ResourceInfo 对应的数据表名。
func (ResourceInfo) TableName() string { return "resource_info" }

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
// 参数/返回：info 为待写入资源；已存在时补齐空字段（补字段更新），不存在时插入新记录。
// 失败场景：仓储未初始化、查询失败或写入失败时返回错误。
// 副作用：写入 resource_info 表（INSERT 或 UPDATE）。
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
	info.PosterURL = strings.TrimSpace(info.PosterURL)
	info.Summary = strings.TrimSpace(info.Summary)

	type finder struct {
		id   string
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

	merged := false
	fill := func(dst *string, src string) {
		src = strings.TrimSpace(src)
		if strings.TrimSpace(*dst) == "" && src != "" {
			*dst = src
			merged = true
		}
	}
	fill(&existing.Title, info.Title)
	fill(&existing.Year, info.Year)
	fill(&existing.Country, info.Country)
	fill(&existing.DoubanID, info.DoubanID)
	fill(&existing.ImdbID, info.ImdbID)
	fill(&existing.TmdbID, info.TmdbID)
	fill(&existing.PosterURL, info.PosterURL)
	fill(&existing.Summary, info.Summary)
	if !merged {
		return nil
	}
	existing.UpdatedAt = now
	return r.store.DB.Save(existing).Error
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
	return &rows[0], nil
}
