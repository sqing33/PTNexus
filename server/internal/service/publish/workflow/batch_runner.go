package workflow

import "time"

// BatchRunnerDeps 定义批量发布执行器依赖回调。
type BatchRunnerDeps struct {
	IsCancelled    func() bool
	PublishToSite  func(siteName string) (map[string]any, int)
	OnSiteStarted  func(siteName string)
	OnSiteFinished func(siteName string, result map[string]any)
	OnBatchStopped func()
}

// RunBatchPublish 按目标站点顺序执行批量发布循环。
// 参数/返回：targets 为目标站点列表；依赖通过回调注入；无返回值。
// 失败场景：回调内部错误由调用方处理，函数本身不中断进程。
// 副作用：会调用外部发布回调并按节流间隔串行执行。
func RunBatchPublish(targets []string, deps BatchRunnerDeps) {
	for _, siteName := range targets {
		if deps.IsCancelled != nil && deps.IsCancelled() {
			if deps.OnBatchStopped != nil {
				deps.OnBatchStopped()
			}
			break
		}

		if deps.OnSiteStarted != nil {
			deps.OnSiteStarted(siteName)
		}

		result := map[string]any{}
		status := 500
		if deps.PublishToSite != nil {
			result, status = deps.PublishToSite(siteName)
		}
		if result == nil {
			result = map[string]any{}
		}
		if status != 200 {
			result["success"] = false
		}

		if deps.OnSiteFinished != nil {
			deps.OnSiteFinished(siteName, result)
		}
		time.Sleep(120 * time.Millisecond)
	}
}
