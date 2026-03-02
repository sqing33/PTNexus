package service

import (
	"fmt"
	"strings"
	"sync"

	migrationflow "github.com/pt-nexus/server/internal/service/migrationflow"
)

type GoProxyService struct {
	migrate *migrationflow.MigrateService

	mu            sync.RWMutex
	running       bool
	stopRequested bool
}

// NewGoProxyService 创建 GoProxyService，用于承载 Go 版批量增强能力。
// 参数/返回：migrate 提供抓取/发布编排能力；返回可用的服务实例。
// 失败场景：不适用。
// 副作用：无。
func NewGoProxyService(migrate *migrationflow.MigrateService) *GoProxyService {
	return &GoProxyService{migrate: migrate}
}

// StopBatchEnhance 请求停止当前批量转种任务。
// 参数/返回：无；返回标准接口响应与 HTTP 状态码。
// 失败场景：不适用（没有运行中的任务也返回 success）。
// 副作用：设置 stopRequested 标志，任务会在当前种子处理完成后停止。
func (s *GoProxyService) StopBatchEnhance() (map[string]any, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return map[string]any{"success": true, "message": "当前没有运行中的批量转种任务"}, 200
	}
	s.stopRequested = true
	return map[string]any{"success": true, "message": "已发送停止信号"}, 200
}

func goProxyToString(value any, fallback string) string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback
		}
		return typed
	case []byte:
		text := strings.TrimSpace(string(typed))
		if text == "" {
			return fallback
		}
		return text
	default:
		if value == nil {
			return fallback
		}
		text := fmt.Sprintf("%v", value)
		if strings.TrimSpace(text) == "" {
			return fallback
		}
		return text
	}
}
