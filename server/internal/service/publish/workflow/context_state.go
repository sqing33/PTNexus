package workflow

import "sync"

// Context 表示跨流程传递的迁移上下文。
type Context struct {
	TaskID              string
	TorrentID           string
	SiteName            string
	Hash                string
	Name                string
	SavePath            string
	DownloaderID        string
	SourceNickname      string
	SourceDetailURL     string
	OriginalTorrentPath string
	TorrentDir          string
}

// ContextState 管理迁移上下文缓存。
type ContextState struct {
	mu       sync.RWMutex
	contexts map[string]Context
}

// NewContextState 创建迁移上下文状态管理器。
func NewContextState() *ContextState {
	return &ContextState{contexts: map[string]Context{}}
}

// Set 写入上下文。
func (s *ContextState) Set(taskID string, ctx Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.contexts[taskID] = ctx
	s.mu.Unlock()
}

// Get 读取上下文。
func (s *ContextState) Get(taskID string) (Context, bool) {
	if s == nil {
		return Context{}, false
	}
	s.mu.RLock()
	ctx, ok := s.contexts[taskID]
	s.mu.RUnlock()
	return ctx, ok
}
