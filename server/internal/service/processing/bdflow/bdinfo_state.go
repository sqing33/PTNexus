package bdflow

import (
	"sync"
	"time"
)

// BDInfoState 管理 BDInfo 任务的内存状态。
type BDInfoState struct {
	mu    sync.RWMutex
	tasks map[string]*BDInfoTask
}

// NewBDInfoState 创建 BDInfo 状态管理器。
func NewBDInfoState() *BDInfoState {
	return &BDInfoState{tasks: map[string]*BDInfoTask{}}
}

// Register 注册新任务。
func (s *BDInfoState) Register(task *BDInfoTask) {
	if s == nil || task == nil {
		return
	}
	s.mu.Lock()
	s.tasks[task.TaskID] = task
	s.mu.Unlock()
}

// TaskStatus 返回任务状态快照。
func (s *BDInfoState) TaskStatus(taskID string) (BDInfoTask, bool) {
	if s == nil {
		return BDInfoTask{}, false
	}
	s.mu.RLock()
	task := s.tasks[taskID]
	s.mu.RUnlock()
	if task == nil {
		return BDInfoTask{}, false
	}
	return cloneTask(task), true
}

// UpdateProgress 更新任务进度信息。
func (s *BDInfoState) UpdateProgress(taskID string, progress float64, currentFile, elapsed, remaining string, updatedAt time.Time) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	task := s.tasks[taskID]
	if task != nil {
		task.ProgressPercent = progress
		task.CurrentFile = currentFile
		task.ElapsedTime = elapsed
		task.RemainingTime = remaining
		task.UpdatedAt = updatedAt
	}
	s.mu.Unlock()
	return task != nil
}

// UpdateDiscSize 更新任务的蓝光原盘体积（单位：字节）。
// 参数/返回：taskID 为任务标识；discSize 为原盘体积；updatedAt 为更新时间；返回是否更新成功。
// 失败场景：任务不存在时返回 false。
// 副作用：更新 BDInfoState 中任务的 DiscSize 字段。
func (s *BDInfoState) UpdateDiscSize(taskID string, discSize int64, updatedAt time.Time) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	task := s.tasks[taskID]
	if task != nil {
		task.DiscSize = discSize
		task.UpdatedAt = updatedAt
	}
	s.mu.Unlock()
	return task != nil
}

// SetProgress 设置任务运行状态与进度。
func (s *BDInfoState) SetProgress(taskID string, status string, progress float64, currentFile, elapsed, remaining string, updatedAt time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if task := s.tasks[taskID]; task != nil {
		task.Status = status
		task.ProgressPercent = progress
		task.CurrentFile = currentFile
		task.ElapsedTime = elapsed
		task.RemainingTime = remaining
		task.UpdatedAt = updatedAt
	}
	s.mu.Unlock()
}

// MarkFailed 标记任务失败。
func (s *BDInfoState) MarkFailed(taskID string, errMsg string, completedAt time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if task := s.tasks[taskID]; task != nil {
		task.Status = "failed"
		task.Error = errMsg
		task.UpdatedAt = completedAt
		task.CompletedAt = &completedAt
	}
	s.mu.Unlock()
}

// MarkCompleted 标记任务完成。
func (s *BDInfoState) MarkCompleted(taskID string, currentFile string, mediaInfo string, completedAt time.Time, elapsed string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if task := s.tasks[taskID]; task != nil {
		task.Status = "completed"
		task.ProgressPercent = 100
		task.CurrentFile = currentFile
		task.ElapsedTime = elapsed
		task.RemainingTime = "0s"
		task.MediaInfo = mediaInfo
		task.UpdatedAt = completedAt
		task.CompletedAt = &completedAt
	}
	s.mu.Unlock()
}

// ApplyCallbackCompletion 应用回调完成状态并返回 seed_id。
func (s *BDInfoState) ApplyCallbackCompletion(taskID string, success bool, mediaInfo string, errorMessage string, now time.Time) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	task := s.tasks[taskID]
	if task != nil {
		task.UpdatedAt = now
		task.CompletedAt = &now
		if success {
			task.Status = "completed"
			task.ProgressPercent = 100
			task.MediaInfo = mediaInfo
		} else {
			task.Status = "failed"
			task.Error = errorMessage
		}
	}
	s.mu.Unlock()
	if task == nil {
		return "", false
	}
	return task.SeedID, true
}

// CleanupBySeedID 清理仍在处理中的任务。
func (s *BDInfoState) CleanupBySeedID(seedID string, now time.Time) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, task := range s.tasks {
		if task == nil || task.SeedID != seedID || task.Status != "processing_bdinfo" {
			continue
		}
		task.Status = "failed"
		task.Error = "已手动清理"
		task.UpdatedAt = now
		task.CompletedAt = &now
		return true
	}
	return false
}

// Snapshot 返回任务列表和统计快照。
func (s *BDInfoState) Snapshot() ([]map[string]any, map[string]int) {
	if s == nil {
		return []map[string]any{}, map[string]int{"total": 0, "processing": 0, "completed": 0, "failed": 0}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]map[string]any, 0, len(s.tasks))
	stats := map[string]int{"total": 0, "processing": 0, "completed": 0, "failed": 0}
	for id, task := range s.tasks {
		if task == nil {
			continue
		}
		stats["total"]++
		switch task.Status {
		case "processing_bdinfo":
			stats["processing"]++
		case "completed":
			stats["completed"]++
		case "failed":
			stats["failed"]++
		}

		entry := map[string]any{
			"task_id":          id,
			"seed_id":          task.SeedID,
			"status":           task.Status,
			"progress_percent": task.ProgressPercent,
			"current_file":     task.CurrentFile,
			"elapsed_time":     task.ElapsedTime,
			"remaining_time":   task.RemainingTime,
			"disc_size":        task.DiscSize,
			"error":            task.Error,
			"started_at":       task.StartedAt.Format("2006-01-02 15:04:05"),
			"updated_at":       task.UpdatedAt.Format("2006-01-02 15:04:05"),
			"completed_at":     nil,
		}
		if task.CompletedAt != nil {
			entry["completed_at"] = task.CompletedAt.Format("2006-01-02 15:04:05")
		}
		tasks = append(tasks, entry)
	}
	return tasks, stats
}

func cloneTask(task *BDInfoTask) BDInfoTask {
	if task == nil {
		return BDInfoTask{}
	}
	copied := *task
	return copied
}
