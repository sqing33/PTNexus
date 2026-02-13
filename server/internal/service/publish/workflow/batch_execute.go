package workflow

import (
	"time"

	processingpersist "github.com/pt-nexus/server-go/internal/service/processing/persist"
)

// ManagedBatchInput 定义带状态管理的批量发布执行输入。
type ManagedBatchInput struct {
	BatchID     string
	Targets     []string
	Concurrency int
}

// ManagedBatchDeps 定义带状态管理的批量发布执行依赖。
type ManagedBatchDeps struct {
	State         *BatchState
	PublishToSite func(siteName string) (map[string]any, int)
}

// RunManagedBatchPublish 执行“发布循环 + 状态更新 + 事件广播”的完整批量流程。
// 参数/返回：input 提供批次标识与目标站点，deps 注入状态存储与发布回调；无返回值。
// 失败场景：状态容器为空时直接返回；单站发布错误由回调结果记录，不中断整个批次。
// 副作用：更新 BatchState、广播 SSE 事件，并在结束时关闭订阅通道。
func RunManagedBatchPublish(input ManagedBatchInput, deps ManagedBatchDeps) {
	if deps.State == nil {
		return
	}

	runnerDeps := BatchRunnerDeps{
		IsCancelled: func() bool {
			return deps.State.IsCancelled(input.BatchID)
		},
		PublishToSite: deps.PublishToSite,
		OnSiteStarted: func(siteName string) {
			deps.State.Emit(input.BatchID, map[string]any{"type": "site_started", "siteName": siteName})
		},
		OnSiteFinished: func(siteName string, result map[string]any) {
			deps.State.MarkSiteResult(input.BatchID, siteName, result, processingpersist.BoolFromAny(result["success"]))
			deps.State.Emit(input.BatchID, map[string]any{"type": "site_finished", "siteName": siteName, "result": result})
		},
		OnBatchStopped: func() {
			deps.State.Emit(input.BatchID, map[string]any{
				"type":    "batch_stopped",
				"reason":  "cancelled",
				"message": "批量发布任务已取消",
			})
		},
	}

	if input.Concurrency > 1 {
		RunBatchPublishConcurrent(input.Targets, input.Concurrency, runnerDeps)
	} else {
		RunBatchPublish(input.Targets, runnerDeps)
	}

	deps.State.Finish(input.BatchID, time.Now())
	deps.State.Emit(input.BatchID, map[string]any{"type": "batch_finished"})
	time.Sleep(80 * time.Millisecond)
	deps.State.CloseSubscribers(input.BatchID)
}
