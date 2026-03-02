package migrationflow

import (
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
)

func (s *MigrateService) StartBatchFetch(torrentNames []string, sourcePriority []string) (map[string]any, int) {
	if len(torrentNames) == 0 {
		return map[string]any{"success": false, "message": "错误：种子名称列表不能为空"}, 400
	}
	if len(sourcePriority) == 0 {
		configured := s.cfg.Get()
		sourcePriority = processingshared.ToStringSlice(configured["source_priority"])
	}
	if len(sourcePriority) == 0 {
		return map[string]any{"success": false, "message": "错误：请先在设置中配置源站点优先级"}, 400
	}
	taskID := s.newID("fetch")
	s.batchFetchState.Start(taskID, len(torrentNames))
	go s.runBatchFetch(taskID, torrentNames, sourcePriority)
	return map[string]any{"success": true, "task_id": taskID, "message": "批量获取任务已启动"}, 200
}

func (s *MigrateService) runBatchFetch(taskID string, torrentNames []string, sourcePriority []string) {
	rows, err := s.repo.ListTorrentsByNames(torrentNames)
	if err != nil {
		for _, name := range torrentNames {
			s.batchFetchState.MarkResult(taskID, false, map[string]any{
				"name":   name,
				"status": "failed",
				"reason": "读取 torrents 失败: " + err.Error(),
			})
		}
		s.batchFetchState.Finish(taskID)
		return
	}

	acquirefetch.RunBatchFetch(torrentNames, sourcePriority, rows, acquirefetch.BatchFetchRunnerDeps{
		FetchAndStore: s.FetchAndStore,
		OnResult: func(success bool, result map[string]any) {
			s.batchFetchState.MarkResult(taskID, success, result)
		},
	})
	s.batchFetchState.Finish(taskID)
}

func (s *MigrateService) BatchFetchProgress(taskID string) (map[string]any, int) {
	task, ok := s.batchFetchState.Snapshot(taskID)
	if !ok {
		return map[string]any{"success": false, "message": "任务不存在或已过期"}, 404
	}
	return map[string]any{"success": true, "progress": task}, 200
}
