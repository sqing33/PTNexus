package repository

import (
	"errors"
	"strings"
	"time"
)

const publishLogTimeLayout = "2006-01-02 15:04:05"

// PublishLogEntry 表示一条发种（发布）记录，通常按“单站点发布一次”为一条。
type PublishLogEntry struct {
	ID uint64 `json:"id" gorm:"column:id;primaryKey"`

	Trigger     string `json:"trigger" gorm:"column:publish_trigger"`
	Scene       string `json:"scene" gorm:"column:scene"`
	QueueTaskID *int64 `json:"queue_task_id" gorm:"column:queue_task_id"`

	TaskID     string `json:"task_id" gorm:"column:task_id"`
	TorrentID  string `json:"torrent_id" gorm:"column:torrent_id"`
	SourceSite string `json:"source_site" gorm:"column:source_site"`
	TargetSite string `json:"target_site" gorm:"column:target_site"`

	DownloaderID string `json:"downloader_id" gorm:"column:downloader_id"`
	Title        string `json:"title" gorm:"column:title"`
	Subtitle     string `json:"subtitle" gorm:"column:subtitle"`

	Status    string `json:"status" gorm:"column:status"`
	ResultURL string `json:"result_url" gorm:"column:result_url"`
	Logs      string `json:"logs" gorm:"column:logs"`

	AutoAddResult string `json:"auto_add_result" gorm:"column:auto_add_result"`
	CostMS        int64  `json:"cost_ms" gorm:"column:cost_ms"`

	CreatedAt string `json:"created_at" gorm:"column:created_at"`
	UpdatedAt string `json:"updated_at" gorm:"column:updated_at"`
}

func (PublishLogEntry) TableName() string { return "publish_logs" }

// PublishLogQuery 定义发种日志列表查询条件（分页 + 搜索 + 筛选）。
type PublishLogQuery struct {
	Page     int
	PageSize int

	Search string

	Status     string
	Trigger    string
	Scene      string
	TargetSite string
	SourceSite string
	TorrentID  string
}

// PublishLogRepository 负责发种日志的写入与分页查询。
// 参数/返回：依赖 Store 访问数据库；方法返回 error 表示失败原因。
// 失败场景：DB 未初始化、写库/读库失败等。
// 副作用：写入/读取 publish_logs 表。
type PublishLogRepository struct {
	store *Store
}

// NewPublishLogRepository 创建发种日志仓储实例。
// 参数/返回：store 为数据库连接容器；返回可复用的仓储对象。
// 失败场景：无直接失败场景（store 为 nil 时由调用方处理）。
// 副作用：无。
func NewPublishLogRepository(store *Store) *PublishLogRepository {
	return &PublishLogRepository{store: store}
}

// Insert 写入一条发种日志并返回自增 ID。
// 参数/返回：entry 为日志内容；返回 id 与 error。
// 失败场景：仓储/DB 未初始化、写库失败时返回 error。
// 副作用：INSERT publish_logs。
func (r *PublishLogRepository) Insert(entry *PublishLogEntry) (uint64, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return 0, errors.New("publish log repo is nil")
	}
	if entry == nil {
		return 0, errors.New("entry is nil")
	}
	if strings.TrimSpace(entry.Trigger) == "" {
		entry.Trigger = "manual"
	}
	if strings.TrimSpace(entry.Status) == "" {
		entry.Status = "unknown"
	}

	now := time.Now().Format(publishLogTimeLayout)
	if strings.TrimSpace(entry.CreatedAt) == "" {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now

	if err := r.store.DB.Create(entry).Error; err != nil {
		return 0, err
	}
	return entry.ID, nil
}

// List 分页查询发种日志。
// 参数/返回：query 为筛选与分页条件；返回 rows、total 与 error。
// 失败场景：查询失败时返回 error。
// 副作用：无（只读）。
func (r *PublishLogRepository) List(query PublishLogQuery) ([]PublishLogEntry, int64, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return nil, 0, errors.New("publish log repo is nil")
	}

	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	offset := (page - 1) * pageSize

	db := r.store.DB.Model(&PublishLogEntry{})
	if value := strings.TrimSpace(query.Status); value != "" {
		db = db.Where("status = ?", value)
	}
	if value := strings.TrimSpace(query.Trigger); value != "" {
		db = db.Where("publish_trigger = ?", value)
	}
	if value := strings.TrimSpace(query.Scene); value != "" {
		db = db.Where("scene = ?", value)
	}
	if value := strings.TrimSpace(query.TargetSite); value != "" {
		db = db.Where("target_site = ?", value)
	}
	if value := strings.TrimSpace(query.SourceSite); value != "" {
		db = db.Where("source_site = ?", value)
	}
	if value := strings.TrimSpace(query.TorrentID); value != "" {
		db = db.Where("torrent_id = ?", value)
	}
	if value := strings.TrimSpace(query.Search); value != "" {
		like := "%" + value + "%"
		if r.store.DBType == "postgresql" {
			db = db.Where(
				"(title ILIKE ? OR subtitle ILIKE ? OR torrent_id ILIKE ? OR target_site ILIKE ? OR source_site ILIKE ? OR task_id ILIKE ?)",
				like, like, like, like, like, like,
			)
		} else {
			db = db.Where(
				"(title LIKE ? OR subtitle LIKE ? OR torrent_id LIKE ? OR target_site LIKE ? OR source_site LIKE ? OR task_id LIKE ?)",
				like, like, like, like, like, like,
			)
		}
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	rows := make([]PublishLogEntry, 0)
	if err := db.Order("id DESC").Offset(offset).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
