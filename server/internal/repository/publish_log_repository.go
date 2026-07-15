package repository

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

const publishLogTimeLayout = "2006-01-02 15:04:05"

// PublishLogEntry 表示一条发种（发布）记录，通常按“单站点发布一次”为一条。
type PublishLogEntry struct {
	ID uint64 `json:"id" gorm:"column:id;primaryKey"`

	Trigger      string `json:"trigger" gorm:"column:publish_trigger"`
	Scene        string `json:"scene" gorm:"column:scene"`
	QueueTaskID  *int64 `json:"queue_task_id" gorm:"column:queue_task_id"`
	QueueGroupID string `json:"queue_group_id" gorm:"column:queue_group_id"`

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

	Status       string
	Trigger      string
	Scene        string
	QueueGroupID string
	TargetSite   string
	SourceSite   string
	TorrentID    string
}

// PublishLogRepository 负责发种日志的写入与分页查询。
// 参数/返回：依赖 Store 访问数据库；方法返回 error 表示失败原因。
// 失败场景：DB 未初始化、写库/读库失败等。
// 副作用：写入/读取 publish_logs 表。
type PublishLogRepository struct {
	store *Store
}

// FindLatestByQueueTaskID 按 queue_task_id 查询最新的一条日志记录。
// 参数/返回：queueTaskID 为队列任务自增 ID；返回日志记录、是否命中与 error。
// 失败场景：DB 未初始化或查询失败返回 error。
// 副作用：读取 publish_logs。
func (r *PublishLogRepository) FindLatestByQueueTaskID(queueTaskID int64) (*PublishLogEntry, bool, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return nil, false, errors.New("publish log repo is nil")
	}
	if queueTaskID <= 0 {
		return nil, false, nil
	}

	row := PublishLogEntry{}
	if err := r.store.DB.Model(&PublishLogEntry{}).
		Where("queue_task_id = ?", queueTaskID).
		Order("id DESC").
		Limit(1).
		Find(&row).Error; err != nil {
		return nil, false, err
	}
	if row.ID == 0 {
		return nil, false, nil
	}
	return &row, true, nil
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

// UpsertByQueueTaskID 按 queue_task_id 更新日志（若不存在则插入）。
// 参数/返回：entry 必须包含 QueueTaskID；返回 error 表示失败原因。
// 失败场景：DB 未初始化、查询/更新/插入失败时返回 error。
// 副作用：写入/更新 publish_logs。
func (r *PublishLogRepository) UpsertByQueueTaskID(entry *PublishLogEntry) error {
	if r == nil || r.store == nil || r.store.DB == nil {
		return errors.New("publish log repo is nil")
	}
	if entry == nil {
		return errors.New("entry is nil")
	}
	if entry.QueueTaskID == nil || *entry.QueueTaskID <= 0 {
		return errors.New("queue_task_id is required")
	}
	if strings.TrimSpace(entry.Trigger) == "" {
		entry.Trigger = "manual"
	}
	if strings.TrimSpace(entry.Status) == "" {
		entry.Status = "unknown"
	}

	now := time.Now().Format(publishLogTimeLayout)
	entry.UpdatedAt = now
	if strings.TrimSpace(entry.CreatedAt) == "" {
		entry.CreatedAt = now
	}

	existing, ok, err := r.FindLatestByQueueTaskID(*entry.QueueTaskID)
	if err != nil {
		return err
	}
	if !ok || existing == nil || existing.ID == 0 {
		_, err := r.Insert(entry)
		return err
	}

	updates := map[string]any{
		"publish_trigger": entry.Trigger,
		"scene":           entry.Scene,
		"queue_group_id":  entry.QueueGroupID,
		"task_id":         entry.TaskID,
		"torrent_id":      entry.TorrentID,
		"source_site":     entry.SourceSite,
		"target_site":     entry.TargetSite,
		"downloader_id":   entry.DownloaderID,
		"title":           entry.Title,
		"subtitle":        entry.Subtitle,
		"status":          entry.Status,
		"result_url":      entry.ResultURL,
		"logs":            entry.Logs,
		"auto_add_result": entry.AutoAddResult,
		"cost_ms":         entry.CostMS,
		"updated_at":      entry.UpdatedAt,
	}

	return r.store.DB.Model(&PublishLogEntry{}).Where("id = ?", existing.ID).Updates(updates).Error
}

// UpdateStatusAndLogsByQueueTaskID 更新队列日志的状态与文本（若不存在则插入最小记录）。
// 参数/返回：queueTaskID 为队列任务 ID；status/logs 为新状态与日志文本；返回 error。
// 失败场景：DB 未初始化、查询/更新/插入失败返回 error。
// 副作用：写入/更新 publish_logs。
func (r *PublishLogRepository) UpdateStatusAndLogsByQueueTaskID(queueTaskID int64, status string, logs string) error {
	if r == nil || r.store == nil || r.store.DB == nil {
		return errors.New("publish log repo is nil")
	}
	if queueTaskID <= 0 {
		return nil
	}

	trimmedStatus := strings.TrimSpace(status)
	if trimmedStatus == "" {
		trimmedStatus = "unknown"
	}

	now := time.Now().Format(publishLogTimeLayout)
	existing, ok, err := r.FindLatestByQueueTaskID(queueTaskID)
	if err != nil {
		return err
	}
	if !ok || existing == nil || existing.ID == 0 {
		entry := PublishLogEntry{
			Trigger:     "queue",
			QueueTaskID: &queueTaskID,
			Status:      trimmedStatus,
			Logs:        strings.TrimSpace(logs),
			CostMS:      0,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		_, err := r.Insert(&entry)
		return err
	}

	updates := map[string]any{
		"status":     trimmedStatus,
		"logs":       strings.TrimSpace(logs),
		"updated_at": now,
	}
	return r.store.DB.Model(&PublishLogEntry{}).Where("id = ?", existing.ID).Updates(updates).Error
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
	if value := strings.TrimSpace(query.QueueGroupID); value != "" {
		db = db.Where("queue_group_id = ?", value)
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
				"(title ILIKE ? OR subtitle ILIKE ? OR torrent_id ILIKE ? OR target_site ILIKE ? OR source_site ILIKE ? OR task_id ILIKE ? OR publish_trigger ILIKE ? OR scene ILIKE ? OR queue_group_id ILIKE ?)",
				like, like, like, like, like, like, like, like, like,
			)
		} else {
			db = db.Where(
				"(title LIKE ? OR subtitle LIKE ? OR torrent_id LIKE ? OR target_site LIKE ? OR source_site LIKE ? OR task_id LIKE ? OR publish_trigger LIKE ? OR scene LIKE ? OR queue_group_id LIKE ?)",
				like, like, like, like, like, like, like, like, like,
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

// DeleteByIDs 按主键列表批量删除发种日志记录。
// 参数/返回：ids 为日志主键列表；返回实际删除行数与 error。
// 失败场景：DB 未初始化或批量删除失败返回 error。
// 副作用：DELETE publish_logs。
func (r *PublishLogRepository) DeleteByIDs(ids []uint64) (int64, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return 0, errors.New("publish log repo is nil")
	}
	if len(ids) == 0 {
		return 0, nil
	}

	result := r.store.DB.Where("id IN ?", ids).Delete(&PublishLogEntry{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// FindByIDs 按主键列表批量查询发种日志记录（仅返回 ID/Status/QueueTaskID）。
// 参数/返回：ids 为日志主键列表；返回匹配的日志列表与 error。
// 失败场景：DB 未初始化或查询失败返回 error。
// 副作用：无（只读）。
func (r *PublishLogRepository) FindByIDs(ids []uint64) ([]PublishLogEntry, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return nil, errors.New("publish log repo is nil")
	}
	if len(ids) == 0 {
		return nil, nil
	}

	rows := make([]PublishLogEntry, 0)
	if err := r.store.DB.
		Select("id, status, queue_task_id").
		Where("id IN ?", ids).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// MaxNumericTriggerSuffix 计算 publish_trigger 以指定前缀开头的最大数字后缀。
// 参数/返回：prefix 为触发前缀（例如：批量转种-）；返回最大数字（不存在则为 0）与 error。
// 失败场景：DB 未初始化或查询失败返回 error；解析失败的触发会被忽略。
// 副作用：读取 publish_logs。
func (r *PublishLogRepository) MaxNumericTriggerSuffix(prefix string) (int, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return 0, errors.New("publish log repo is nil")
	}

	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return 0, nil
	}

	triggers := make([]string, 0)
	if err := r.store.DB.Model(&PublishLogEntry{}).
		Distinct("publish_trigger").
		Where("publish_trigger LIKE ?", prefix+"%").
		Pluck("publish_trigger", &triggers).Error; err != nil {
		return 0, err
	}

	maxNumber := 0
	for _, trigger := range triggers {
		value := strings.TrimSpace(trigger)
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		suffix := strings.TrimSpace(strings.TrimPrefix(value, prefix))
		if suffix == "" {
			continue
		}
		number, err := strconv.Atoi(suffix)
		if err != nil || number <= 0 {
			continue
		}
		if number > maxNumber {
			maxNumber = number
		}
	}

	return maxNumber, nil
}
