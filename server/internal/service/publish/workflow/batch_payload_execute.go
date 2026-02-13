package workflow

// ManagedBatchFromPayloadInput 定义按基础 payload 执行批量发布的输入。
type ManagedBatchFromPayloadInput struct {
	BatchID     string
	Targets     []string
	Concurrency int
	Payload     map[string]any
}

// ManagedBatchFromPayloadDeps 定义按 payload 执行批量发布的依赖。
type ManagedBatchFromPayloadDeps struct {
	State          *BatchState
	PublishPayload func(payload map[string]any) (map[string]any, int)
}

// RunManagedBatchPublishFromPayload 执行“按站点改写 targetSite + 发布 + 状态管理”的批量流程。
// 参数/返回：input 为批次信息与基础 payload；deps 注入状态容器与单站发布函数；无返回值。
// 失败场景：PublishPayload 为空时单站结果标记失败但不终止整个批次。
// 副作用：更新 BatchState、广播事件并关闭订阅。
func RunManagedBatchPublishFromPayload(input ManagedBatchFromPayloadInput, deps ManagedBatchFromPayloadDeps) {
	RunManagedBatchPublish(
		ManagedBatchInput{
			BatchID:     input.BatchID,
			Targets:     input.Targets,
			Concurrency: input.Concurrency,
		},
		ManagedBatchDeps{
			State: deps.State,
			PublishToSite: func(siteName string) (map[string]any, int) {
				if deps.PublishPayload == nil {
					return map[string]any{"success": false, "logs": "发布函数未初始化"}, 500
				}
				targetPayload := map[string]any{}
				for key, value := range input.Payload {
					targetPayload[key] = value
				}
				targetPayload["targetSite"] = siteName
				return deps.PublishPayload(targetPayload)
			},
		},
	)
}
