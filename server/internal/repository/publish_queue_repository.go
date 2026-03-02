package repository

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// PublishQueueTimeLayout 为队列任务存储时间字段的统一格式（保证跨 DB 的可读性与可排序性）。
const PublishQueueTimeLayout = "2006-01-02 15:04:05"

const (
	PublishQueueStatusQueued    = "queued"
	PublishQueueStatusRunning   = "running"
	PublishQueueStatusSuccess   = "success"
	PublishQueueStatusFailed    = "failed"
	PublishQueueStatusCancelled = "cancelled"
)

var (
	// ErrPublishQueueTaskNotFound 表示队列任务不存在。
	ErrPublishQueueTaskNotFound = errors.New("publish queue task not found")
	// ErrPublishQueueTaskNotQueued 表示队列任务当前不是 queued 状态。
	ErrPublishQueueTaskNotQueued = errors.New("publish queue task is not queued")
)

// PublishQueueTask 表示一条待执行的发布队列任务记录（单目标站点粒度）。
type PublishQueueTask struct {
	ID int64 `json:"id" gorm:"column:id;primaryKey"`

	GroupID string `json:"group_id" gorm:"column:group_id"`
	Status  string `json:"status" gorm:"column:status"`

	TaskID     string `json:"task_id" gorm:"column:task_id"`
	Trigger    string `json:"trigger" gorm:"column:publish_trigger"`
	Scene      string `json:"scene" gorm:"column:scene"`
	TorrentID  string `json:"torrent_id" gorm:"column:torrent_id"`
	SourceSite string `json:"source_site" gorm:"column:source_site"`
	TargetSite string `json:"target_site" gorm:"column:target_site"`

	DownloaderID string `json:"downloader_id" gorm:"column:downloader_id"`
	Title        string `json:"title" gorm:"column:title"`
	Subtitle     string `json:"subtitle" gorm:"column:subtitle"`

	PayloadJSON    string `json:"payload_json" gorm:"column:payload_json"`
	UploadDataJSON string `json:"upload_data_json" gorm:"column:upload_data_json"`
	ContextJSON    string `json:"context_json" gorm:"column:context_json"`

	AttemptCount int `json:"attempt_count" gorm:"column:attempt_count"`

	ScheduledAt *string `json:"scheduled_at,omitempty" gorm:"column:scheduled_at"`
	NextRunAt   *string `json:"next_run_at,omitempty" gorm:"column:next_run_at"`
	StartedAt   *string `json:"started_at,omitempty" gorm:"column:started_at"`
	FinishedAt  *string `json:"finished_at,omitempty" gorm:"column:finished_at"`

	LastError  string `json:"last_error" gorm:"column:last_error"`
	LastResult string `json:"last_result" gorm:"column:last_result"`

	CreatedAt string `json:"created_at" gorm:"column:created_at"`
	UpdatedAt string `json:"updated_at" gorm:"column:updated_at"`
}

func (PublishQueueTask) TableName() string { return "publish_queue_tasks" }

// PublishQueueRepository 负责发布队列任务的入库、领取与状态更新。
// 参数/返回：依赖 Store 访问数据库；方法返回 error 表示失败原因。
// 失败场景：DB 未初始化、事务/更新失败等。
// 副作用：会写入/更新/删除 publish_queue_tasks 表。
type PublishQueueRepository struct {
	store *Store
}

// NewPublishQueueRepository 创建发布队列仓储实例。
// 参数/返回：store 为数据库连接容器；返回仓储对象。
// 失败场景：无直接失败场景。
// 副作用：无。
func NewPublishQueueRepository(store *Store) *PublishQueueRepository {
	return &PublishQueueRepository{store: store}
}

// DB 返回底层 gorm.DB，供 Service 复用事务与查询。
// 参数/返回：无入参；返回 DB 指针（仓储未就绪时返回 nil）。
// 失败场景：无。
// 副作用：无。
func (r *PublishQueueRepository) DB() *gorm.DB {
	if r == nil || r.store == nil {
		return nil
	}
	return r.store.DB
}

// EnqueueTasks 批量写入发布队列任务。
// 参数/返回：tasks 为待写入任务；返回写入后的任务切片（包含自增 ID）与 error。
// 失败场景：数据库不可用或写入失败返回 error。
// 副作用：写入 publish_queue_tasks。
func (r *PublishQueueRepository) EnqueueTasks(tasks []PublishQueueTask) ([]PublishQueueTask, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return nil, errors.New("publish queue repo is nil")
	}
	if len(tasks) == 0 {
		return nil, nil
	}

	now := time.Now().Format(PublishQueueTimeLayout)
	for idx := range tasks {
		if strings.TrimSpace(tasks[idx].Status) == "" {
			tasks[idx].Status = PublishQueueStatusQueued
		}
		if strings.TrimSpace(tasks[idx].Trigger) == "" {
			tasks[idx].Trigger = "queue"
		}
		if strings.TrimSpace(tasks[idx].CreatedAt) == "" {
			tasks[idx].CreatedAt = now
		}
		tasks[idx].UpdatedAt = now
	}

	if err := r.store.DB.Table("publish_queue_tasks").Create(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// CountActiveTasks 统计当前队列中“排队中/运行中”的任务数。
// 参数/返回：无入参；返回数量与 error。
// 失败场景：数据库查询失败返回 error。
// 副作用：读取 publish_queue_tasks。
func (r *PublishQueueRepository) CountActiveTasks() (int64, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return 0, errors.New("publish queue repo is nil")
	}
	var count int64
	if err := r.store.DB.Table("publish_queue_tasks").
		Where("status IN ?", []string{PublishQueueStatusQueued, PublishQueueStatusRunning}).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// ClaimNextRunnableTask 原子领取下一条可执行的 queued 任务，并标记为 running。
// 参数/返回：now 为当前时间；返回任务、是否命中与 error。
// 失败场景：事务/更新/查询失败返回 error。
// 副作用：更新 publish_queue_tasks.status/started_at/updated_at。
func (r *PublishQueueRepository) ClaimNextRunnableTask(now time.Time) (*PublishQueueTask, bool, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return nil, false, errors.New("publish queue repo is nil")
	}

	nowText := now.Format(PublishQueueTimeLayout)
	claimed := (*PublishQueueTask)(nil)

	err := r.store.DB.Transaction(func(tx *gorm.DB) error {
		row := struct {
			ID int64 `gorm:"column:id"`
		}{}
		if err := tx.Raw(
			`SELECT id
			 FROM publish_queue_tasks
			 WHERE status = ?
			   AND (scheduled_at IS NULL OR scheduled_at <= ?)
			   AND (next_run_at IS NULL OR next_run_at <= ?)
			 ORDER BY COALESCE(next_run_at, created_at) ASC, id ASC
			 LIMIT 1`,
			PublishQueueStatusQueued,
			nowText,
			nowText,
		).Scan(&row).Error; err != nil {
			return err
		}
		if row.ID == 0 {
			return nil
		}

		result := tx.Exec(
			`UPDATE publish_queue_tasks
			 SET status = ?, started_at = ?, updated_at = ?
			 WHERE id = ? AND status = ?`,
			PublishQueueStatusRunning,
			nowText,
			nowText,
			row.ID,
			PublishQueueStatusQueued,
		)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}

		task := PublishQueueTask{}
		if err := tx.Raw(`SELECT * FROM publish_queue_tasks WHERE id = ?`, row.ID).Scan(&task).Error; err != nil {
			return err
		}
		claimed = &task
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if claimed == nil || claimed.ID == 0 {
		return nil, false, nil
	}
	return claimed, true, nil
}

// UpdateTaskAfterRequeue 将 running 任务重置为 queued，并写入下次运行时间与原因（不增加 attempt_count）。
// 参数/返回：id 为任务主键；nextRunAt 为下次可运行时间；reason/result 为调试信息；返回 error。
// 失败场景：更新失败返回 error。
// 副作用：更新 publish_queue_tasks 状态与字段。
func (r *PublishQueueRepository) UpdateTaskAfterRequeue(id int64, nextRunAt time.Time, reason string, result string) error {
	if r == nil || r.store == nil || r.store.DB == nil {
		return errors.New("publish queue repo is nil")
	}
	if id <= 0 {
		return nil
	}

	nowText := time.Now().Format(PublishQueueTimeLayout)
	nextText := nextRunAt.Format(PublishQueueTimeLayout)
	return r.store.DB.Table("publish_queue_tasks").
		Where("id = ?", id).
		Updates(map[string]any{
			"status":      PublishQueueStatusQueued,
			"next_run_at": nextText,
			"started_at":  nil,
			"finished_at": nil,
			"last_error":  strings.TrimSpace(reason),
			"last_result": strings.TrimSpace(result),
			"updated_at":  nowText,
		}).Error
}

// UpdateTaskAfterFailure 记录失败并按需重试（写入 attempt_count 与 next_run_at）。
// 参数/返回：id 为任务主键；attemptCount 为最新次数；nextRunAt 为空表示不再重试；reason/result 为调试信息；返回 error。
// 失败场景：更新失败返回 error。
// 副作用：更新 publish_queue_tasks 状态与字段。
func (r *PublishQueueRepository) UpdateTaskAfterFailure(id int64, attemptCount int, nextRunAt *time.Time, reason string, result string) error {
	if r == nil || r.store == nil || r.store.DB == nil {
		return errors.New("publish queue repo is nil")
	}
	if id <= 0 {
		return nil
	}

	nowText := time.Now().Format(PublishQueueTimeLayout)
	status := PublishQueueStatusFailed
	nextText := (*string)(nil)
	if nextRunAt != nil {
		status = PublishQueueStatusQueued
		value := nextRunAt.Format(PublishQueueTimeLayout)
		nextText = &value
	}

	updates := map[string]any{
		"status":        status,
		"attempt_count": attemptCount,
		"next_run_at":   nextText,
		"last_error":    strings.TrimSpace(reason),
		"last_result":   strings.TrimSpace(result),
		"updated_at":    nowText,
	}
	if nextRunAt != nil {
		updates["started_at"] = nil
		updates["finished_at"] = nil
	} else {
		updates["finished_at"] = nowText
	}

	return r.store.DB.Table("publish_queue_tasks").
		Where("id = ?", id).
		Updates(updates).Error
}

// UpdateTaskAfterSuccess 将任务标记为成功并写入结果。
// 参数/返回：id 为任务主键；result 为调试信息；返回 error。
// 失败场景：更新失败返回 error。
// 副作用：更新 publish_queue_tasks 状态与字段。
func (r *PublishQueueRepository) UpdateTaskAfterSuccess(id int64, result string) error {
	if r == nil || r.store == nil || r.store.DB == nil {
		return errors.New("publish queue repo is nil")
	}
	if id <= 0 {
		return nil
	}

	nowText := time.Now().Format(PublishQueueTimeLayout)
	return r.store.DB.Table("publish_queue_tasks").
		Where("id = ?", id).
		Updates(map[string]any{
			"status":      PublishQueueStatusSuccess,
			"next_run_at": nil,
			"finished_at": nowText,
			"last_error":  "",
			"last_result": strings.TrimSpace(result),
			"updated_at":  nowText,
		}).Error
}

// FindTaskByID 按主键读取队列任务。
// 参数/返回：id 为任务主键；返回任务、是否命中与 error。
// 失败场景：数据库查询失败返回 error。
// 副作用：读取 publish_queue_tasks。
func (r *PublishQueueRepository) FindTaskByID(id int64) (*PublishQueueTask, bool, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return nil, false, errors.New("publish queue repo is nil")
	}
	if id <= 0 {
		return nil, false, nil
	}

	task := PublishQueueTask{}
	if err := r.store.DB.Table("publish_queue_tasks").Where("id = ?", id).Limit(1).Find(&task).Error; err != nil {
		return nil, false, err
	}
	if task.ID <= 0 {
		return nil, false, nil
	}
	return &task, true, nil
}

// CancelQueuedTask 将 queued 状态任务取消为 cancelled，供 UI 删除待发布项使用。
// 参数/返回：id 为任务主键；reason 为取消原因；返回 error。
// 失败场景：任务不存在或非 queued 状态会返回对应错误；更新失败返回 error。
// 副作用：更新 publish_queue_tasks 状态与时间字段。
func (r *PublishQueueRepository) CancelQueuedTask(id int64, reason string) error {
	if r == nil || r.store == nil || r.store.DB == nil {
		return errors.New("publish queue repo is nil")
	}
	if id <= 0 {
		return ErrPublishQueueTaskNotFound
	}

	nowText := time.Now().Format(PublishQueueTimeLayout)
	result := r.store.DB.Table("publish_queue_tasks").
		Where("id = ? AND status = ?", id, PublishQueueStatusQueued).
		Updates(map[string]any{
			"status":      PublishQueueStatusCancelled,
			"next_run_at": nil,
			"started_at":  nil,
			"finished_at": nowText,
			"last_error":  strings.TrimSpace(reason),
			"updated_at":  nowText,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}

	exists := int64(0)
	if err := r.store.DB.Table("publish_queue_tasks").Where("id = ?", id).Count(&exists).Error; err != nil {
		return err
	}
	if exists == 0 {
		return ErrPublishQueueTaskNotFound
	}
	return ErrPublishQueueTaskNotQueued
}

// CleanupFinishedTasks 清理已完成任务，避免队列表无限增长。
// 参数/返回：olderThan 为截止时间；返回删除条数与 error。
// 失败场景：数据库删除失败返回 error。
// 副作用：删除 publish_queue_tasks 表中 success/failed 的旧记录。
func (r *PublishQueueRepository) CleanupFinishedTasks(olderThan time.Time) (int64, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return 0, errors.New("publish queue repo is nil")
	}

	cutoffText := olderThan.Format(PublishQueueTimeLayout)
	result := r.store.DB.Table("publish_queue_tasks").
		Where(
			"status IN ? AND finished_at IS NOT NULL AND finished_at < ?",
			[]string{PublishQueueStatusSuccess, PublishQueueStatusFailed, PublishQueueStatusCancelled},
			cutoffText,
		).
		Delete(&PublishQueueTask{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
