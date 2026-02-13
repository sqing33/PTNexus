package fetch

import "sync"

// BatchFetchTask 表示批量抓取任务的进度快照。
type BatchFetchTask struct {
	Total     int              `json:"total"`
	Processed int              `json:"processed"`
	Success   int              `json:"success"`
	Failed    int              `json:"failed"`
	Skipped   int              `json:"skipped"`
	IsRunning bool             `json:"isRunning"`
	Results   []map[string]any `json:"results"`
}

// BatchFetchState 管理批量抓取任务的内存状态。
type BatchFetchState struct {
	mu    sync.RWMutex
	tasks map[string]*BatchFetchTask
}

// NewBatchFetchState 创建批量抓取状态管理器。
func NewBatchFetchState() *BatchFetchState {
	return &BatchFetchState{tasks: map[string]*BatchFetchTask{}}
}

// Start 注册并初始化一个批量抓取任务。
func (s *BatchFetchState) Start(taskID string, total int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.tasks[taskID] = &BatchFetchTask{
		Total:     total,
		Processed: 0,
		Success:   0,
		Failed:    0,
		Skipped:   0,
		IsRunning: true,
		Results:   []map[string]any{},
	}
	s.mu.Unlock()
}

// MarkResult 记录单项抓取结果并更新计数。
func (s *BatchFetchState) MarkResult(taskID string, success bool, result map[string]any) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	task := s.tasks[taskID]
	if task != nil {
		task.Processed++
		if success {
			task.Success++
		} else {
			task.Failed++
		}
		task.Results = append(task.Results, cloneAnyMap(result))
	}
	s.mu.Unlock()
	return task != nil
}

// Finish 标记批量抓取任务结束。
func (s *BatchFetchState) Finish(taskID string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	task := s.tasks[taskID]
	if task != nil {
		task.IsRunning = false
	}
	s.mu.Unlock()
	return task != nil
}

// Snapshot 返回任务状态副本。
func (s *BatchFetchState) Snapshot(taskID string) (BatchFetchTask, bool) {
	if s == nil {
		return BatchFetchTask{}, false
	}
	s.mu.RLock()
	task := s.tasks[taskID]
	s.mu.RUnlock()
	if task == nil {
		return BatchFetchTask{}, false
	}
	copied := *task
	copied.Results = make([]map[string]any, 0, len(task.Results))
	for _, item := range task.Results {
		copied.Results = append(copied.Results, cloneAnyMap(item))
	}
	return copied, true
}

func cloneAnyMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	copied := make(map[string]any, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}
