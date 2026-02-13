package workflow

import (
	"sync"
	"time"
)

// BatchTask 表示批量发布任务的状态快照。
type BatchTask struct {
	BatchID     string                    `json:"batch_id"`
	IsRunning   bool                      `json:"isRunning"`
	Total       int                       `json:"total"`
	Processed   int                       `json:"processed"`
	Success     int                       `json:"success"`
	Failed      int                       `json:"failed"`
	Concurrency int                       `json:"concurrency"`
	Cancelled   bool                      `json:"cancelled"`
	CreatedAt   string                    `json:"created_at"`
	FinishedAt  string                    `json:"finished_at"`
	Results     map[string]map[string]any `json:"results"`
}

// BatchState 管理批量发布任务与 SSE 订阅。
type BatchState struct {
	mu    sync.RWMutex
	tasks map[string]*BatchTask
	subs  map[string]map[chan map[string]any]struct{}
}

// NewBatchState 创建批量发布状态管理器。
func NewBatchState() *BatchState {
	return &BatchState{
		tasks: map[string]*BatchTask{},
		subs:  map[string]map[chan map[string]any]struct{}{},
	}
}

// Start 注册并初始化一个批量发布任务。
func (s *BatchState) Start(batchID string, total int, concurrency int, createdAt time.Time) {
	if s == nil {
		return
	}
	if concurrency < 1 {
		concurrency = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[batchID] = &BatchTask{
		BatchID:     batchID,
		IsRunning:   true,
		Total:       total,
		Processed:   0,
		Success:     0,
		Failed:      0,
		Concurrency: concurrency,
		Cancelled:   false,
		CreatedAt:   createdAt.Format("2006-01-02 15:04:05"),
		Results:     map[string]map[string]any{},
	}
	s.subs[batchID] = map[chan map[string]any]struct{}{}
}

// IsCancelled 返回任务是否已取消或不存在。
func (s *BatchState) IsCancelled(batchID string) bool {
	if s == nil {
		return true
	}
	s.mu.RLock()
	task := s.tasks[batchID]
	s.mu.RUnlock()
	return task == nil || task.Cancelled
}

// MarkSiteResult 记录单站点发布结果并更新计数器。
func (s *BatchState) MarkSiteResult(batchID string, siteName string, result map[string]any, success bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[batchID]
	if task == nil {
		return
	}
	task.Processed++
	if success {
		task.Success++
	} else {
		task.Failed++
	}
	task.Results[siteName] = cloneMap(result)
}

// Finish 标记任务结束。
func (s *BatchState) Finish(batchID string, finishedAt time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if task := s.tasks[batchID]; task != nil {
		task.IsRunning = false
		task.FinishedAt = finishedAt.Format("2006-01-02 15:04:05")
	}
}

// Status 返回任务状态副本。
func (s *BatchState) Status(batchID string) (BatchTask, bool) {
	if s == nil {
		return BatchTask{}, false
	}
	s.mu.RLock()
	task, ok := s.tasks[batchID]
	s.mu.RUnlock()
	if !ok || task == nil {
		return BatchTask{}, false
	}
	copied := *task
	copied.Results = map[string]map[string]any{}
	for site, result := range task.Results {
		copied.Results[site] = cloneMap(result)
	}
	return copied, true
}

// Cancel 标记任务取消。
func (s *BatchState) Cancel(batchID string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[batchID]
	if ok && task != nil {
		task.Cancelled = true
	}
	return ok && task != nil
}

// Subscribe 订阅批量发布事件。
func (s *BatchState) Subscribe(batchID string) (chan map[string]any, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tasks[batchID]; !exists {
		return nil, false
	}
	ch := make(chan map[string]any, 128)
	if s.subs[batchID] == nil {
		s.subs[batchID] = map[chan map[string]any]struct{}{}
	}
	s.subs[batchID][ch] = struct{}{}
	return ch, true
}

// Unsubscribe 取消订阅批量发布事件。
func (s *BatchState) Unsubscribe(batchID string, ch chan map[string]any) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	subs, ok := s.subs[batchID]
	if !ok {
		return
	}
	if _, exists := subs[ch]; exists {
		delete(subs, ch)
		close(ch)
	}
}

// Emit 向当前批次所有订阅者广播事件。
func (s *BatchState) Emit(batchID string, payload map[string]any) {
	if s == nil {
		return
	}
	s.mu.RLock()
	subs := s.subs[batchID]
	channels := make([]chan map[string]any, 0, len(subs))
	for ch := range subs {
		channels = append(channels, ch)
	}
	s.mu.RUnlock()
	for _, ch := range channels {
		select {
		case ch <- payload:
		default:
		}
	}
}

// CloseSubscribers 关闭指定批次的所有订阅通道。
func (s *BatchState) CloseSubscribers(batchID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	subs := s.subs[batchID]
	delete(s.subs, batchID)
	s.mu.Unlock()
	for ch := range subs {
		close(ch)
	}
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	copied := make(map[string]any, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}
