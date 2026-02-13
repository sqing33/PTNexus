package workflow

import (
	"sync"
	"time"
)

// RunBatchPublishConcurrent 以 worker pool 方式并发执行批量发布循环。
// 参数/返回：targets 为目标站点列表；concurrency 为并发数；deps 为回调依赖；无返回值。
// 失败场景：回调内部错误由调用方处理，函数本身不中断进程。
// 副作用：会并发调用 PublishToSite，并触发 OnSiteStarted/OnSiteFinished 回调。
func RunBatchPublishConcurrent(targets []string, concurrency int, deps BatchRunnerDeps) {
	if concurrency <= 1 || len(targets) <= 1 {
		RunBatchPublish(targets, deps)
		return
	}

	workerCount := concurrency
	if workerCount > len(targets) {
		workerCount = len(targets)
	}
	if workerCount < 1 {
		workerCount = 1
	}

	jobs := make(chan string)
	var stopOnce sync.Once
	wg := sync.WaitGroup{}

	worker := func() {
		defer wg.Done()
		for siteName := range jobs {
			if deps.IsCancelled != nil && deps.IsCancelled() {
				stopOnce.Do(func() {
					if deps.OnBatchStopped != nil {
						deps.OnBatchStopped()
					}
				})
				continue
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

	wg.Add(workerCount)
	for idx := 0; idx < workerCount; idx++ {
		go worker()
	}

	go func() {
		defer close(jobs)
		for _, siteName := range targets {
			if deps.IsCancelled != nil && deps.IsCancelled() {
				stopOnce.Do(func() {
					if deps.OnBatchStopped != nil {
						deps.OnBatchStopped()
					}
				})
				return
			}
			jobs <- siteName
		}
	}()

	wg.Wait()
}
