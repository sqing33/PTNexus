package migrationflow

import (
	"runtime"
	"strings"
	"time"

	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
	settingspkg "github.com/pt-nexus/server/internal/service/settings"
)

func (s *MigrateService) Publish(payload map[string]any) (map[string]any, int) {
	startedAt := time.Now()

	normalizedPayload := s.normalizePublishPayloadWithCrossSeedDefaults(payload)

	result, status := publishworkflow.ExecutePublishFromPayload(
		publishworkflow.PublishFromPayloadInput{
			Payload:     normalizedPayload,
			TorrentPath: "",
		},
		publishworkflow.PublishFromPayloadDeps{
			ContextState:            s.contextState,
			GetSiteByName:           s.repo.GetSiteByName,
			FindSiteNicknameByGroup: s.repo.FindSiteNicknameByGroup,
			ResolveTorrentPath: func(ctx publishworkflow.Context) string {
				return acquirefetch.ResolvePublishTorrentPath(s.repo, acquirefetch.ResolvePublishTorrentPathInput{
					OriginalTorrentPath: ctx.OriginalTorrentPath,
					TorrentDir:          ctx.TorrentDir,
					SiteName:            ctx.SiteName,
					TorrentID:           ctx.TorrentID,
					SourceNickname:      ctx.SourceNickname,
					SourceDetailURL:     ctx.SourceDetailURL,
				})
			},
			AddToDownloader: s.AddToDownloader,
		},
	)

	ctxTaskID := strings.TrimSpace(processingshared.ToString(normalizedPayload["task_id"], processingshared.ToString(normalizedPayload["taskId"], "")))
	ctxTorrentID := ""
	if ctxTaskID != "" && s.contextState != nil {
		if ctx, ok := s.contextState.Get(ctxTaskID); ok {
			ctxTorrentID = strings.TrimSpace(ctx.TorrentID)
		}
	}
	s.appendPublishLog(normalizedPayload, ctxTaskID, ctxTorrentID, result, status, time.Since(startedAt))
	return result, status
}

func (s *MigrateService) StartPublishBatch(payload map[string]any) (map[string]any, int) {
	targets := processingshared.ToStringSlice(payload["targetSites"])
	if len(targets) == 0 {
		targets = processingshared.ToStringSlice(payload["target_sites"])
	}
	if len(targets) == 0 {
		return map[string]any{"success": false, "message": "缺少 targetSites 参数"}, 400
	}

	// 计算批量发布并发（对齐 Python 规则：payload.concurrency 优先，否则走 cross_seed 配置）。
	rootConfig := map[string]any{}
	if s != nil && s.cfg != nil {
		rootConfig = s.cfg.Get()
	}
	crossSeed := map[string]any{}
	if rootConfig != nil {
		if item, ok := rootConfig["cross_seed"].(map[string]any); ok && item != nil {
			crossSeed = item
		}
	}

	concurrency := int(processingshared.ToFloat(payload["concurrency"]))
	if concurrency < 1 {
		mode := strings.TrimSpace(processingshared.ToString(payload["concurrency_mode"], processingshared.ToString(crossSeed["publish_batch_concurrency_mode"], "cpu")))
		manualValue := int(processingshared.ToFloat(crossSeed["publish_batch_concurrency_manual"]))
		if manualValue < 1 {
			manualValue = settingspkg.BatchPublishDefaultConcurrency
		}
		cpuThreads := runtime.NumCPU()
		if cpuThreads < 1 {
			cpuThreads = 1
		}

		switch mode {
		case "cpu":
			concurrency = cpuThreads * 2
		case "all":
			concurrency = len(targets)
		case "manual":
			concurrency = manualValue
		default:
			concurrency = settingspkg.BatchPublishDefaultConcurrency
		}
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > settingspkg.BatchPublishMaxConcurrency {
		concurrency = settingspkg.BatchPublishMaxConcurrency
	}
	if concurrency > len(targets) {
		concurrency = len(targets)
	}

	batchID := s.newID("batch")
	s.publishState.Start(batchID, len(targets), concurrency, time.Now())

	go s.runPublishBatch(batchID, payload, targets, concurrency)
	return map[string]any{"success": true, "batch_id": batchID, "concurrency": concurrency, "message": "批量发布任务已启动"}, 200
}

func (s *MigrateService) runPublishBatch(batchID string, payload map[string]any, targets []string, concurrency int) {
	publishworkflow.RunManagedBatchPublishFromPayload(
		publishworkflow.ManagedBatchFromPayloadInput{
			BatchID:     batchID,
			Targets:     targets,
			Concurrency: concurrency,
			Payload:     payload,
		},
		publishworkflow.ManagedBatchFromPayloadDeps{
			State:          s.publishState,
			PublishPayload: s.Publish,
		},
	)
}

func (s *MigrateService) PublishBatchStatus(batchID string) (map[string]any, int) {
	task, ok := s.publishState.Status(batchID)
	if !ok {
		return map[string]any{"success": false, "message": "任务不存在"}, 404
	}
	return map[string]any{"success": true, "task": task}, 200
}

func (s *MigrateService) PublishBatchCancel(batchID string) (map[string]any, int) {
	if !s.publishState.Cancel(batchID) {
		return map[string]any{"success": false, "message": "任务不存在"}, 404
	}
	return map[string]any{"success": true, "message": "取消信号已发送"}, 200
}

func (s *MigrateService) SubscribePublishEvents(batchID string) (chan map[string]any, bool) {
	return s.publishState.Subscribe(batchID)
}

func (s *MigrateService) UnsubscribePublishEvents(batchID string, ch chan map[string]any) {
	s.publishState.Unsubscribe(batchID, ch)
}
