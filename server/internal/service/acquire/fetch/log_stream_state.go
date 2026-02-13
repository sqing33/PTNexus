package fetch

import (
	"sync"
	"time"
)

// LogStreamState 管理迁移抓取日志流（SSE）通道。
type LogStreamState struct {
	mu      sync.RWMutex
	streams map[string]chan map[string]any
}

// NewLogStreamState 创建日志流状态管理器。
func NewLogStreamState() *LogStreamState {
	return &LogStreamState{streams: map[string]chan map[string]any{}}
}

// Ensure 返回指定任务日志流；不存在时自动创建。
func (s *LogStreamState) Ensure(taskID string) chan map[string]any {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	stream, exists := s.streams[taskID]
	if !exists {
		stream = make(chan map[string]any, 256)
		s.streams[taskID] = stream
	}
	s.mu.Unlock()
	return stream
}

// Emit 发送日志事件到指定任务通道。
func (s *LogStreamState) Emit(taskID string, payload map[string]any) {
	if s == nil {
		return
	}
	stream := s.Ensure(taskID)
	if stream == nil {
		return
	}
	select {
	case stream <- payload:
	default:
	}
}

// EmitStep 发送标准步骤日志事件。
func (s *LogStreamState) EmitStep(taskID, step, message, status string, now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	s.Emit(taskID, map[string]any{
		"step":      step,
		"message":   message,
		"status":    status,
		"timestamp": now.Format("2006-01-02 15:04:05"),
	})
}

// Close 关闭并移除指定任务日志流。
func (s *LogStreamState) Close(taskID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	stream, exists := s.streams[taskID]
	if exists {
		delete(s.streams, taskID)
	}
	s.mu.Unlock()
	if exists {
		close(stream)
	}
}
