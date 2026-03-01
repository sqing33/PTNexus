package migrationflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pt-nexus/server-go/internal/platform/logx"
	"github.com/pt-nexus/server-go/internal/repository"
	acquirefetch "github.com/pt-nexus/server-go/internal/service/acquire/fetch"
	processingshared "github.com/pt-nexus/server-go/internal/service/processing/shared"
	publishguard "github.com/pt-nexus/server-go/internal/service/publish/guard"
	publishworkflow "github.com/pt-nexus/server-go/internal/service/publish/workflow"
)

const publishQueueLogModule = "发布-队列"

type publishQueueConfig struct {
	Enabled            bool
	MaxQueueSize       int64
	MaxRetries         int
	MaxRetryDelaySec   int
	MaxWorkers         int
	MonitorIntervalSec int
	RetryDelayBase     int
	TaskCleanupHours   int

	TriggerRecentCountBelow     int
	TriggerUploadSpeedBelowMBps float64
}

// InitPublishQueue 初始化发布队列依赖。
// 参数/返回：queueRepo 用于持久化队列任务；statsRepo 用于读取下载器速度（可为 nil）；无返回值。
// 失败场景：参数为空时仅记录日志并跳过初始化，避免启动阶段 panic。
// 副作用：写入服务内部依赖引用（不启动线程）。
func (s *MigrateService) InitPublishQueue(queueRepo *repository.PublishQueueRepository, statsRepo *repository.StatsRepository) {
	if s == nil {
		return
	}
	if queueRepo == nil {
		logx.Warnf(publishQueueLogModule, "初始化发布队列失败：queueRepo 为空")
		return
	}
	s.queueRepo = queueRepo
	s.statsRepo = statsRepo
}

// StartPublishQueueWorker 启动发布队列后台监控线程（重复调用仅生效一次）。
// 参数/返回：无参数无返回。
// 失败场景：队列仓储未初始化时会记录日志并直接返回，不会影响主服务启动。
// 副作用：启动 goroutine，周期性领取队列任务并触发发布流程。
func (s *MigrateService) StartPublishQueueWorker() {
	if s == nil {
		return
	}
	s.queueStartOnce.Do(func() {
		if s.queueRepo == nil || s.queueRepo.DB() == nil {
			logx.Warnf(publishQueueLogModule, "发布队列未启动：仓储未初始化")
			return
		}
		s.queueStopCh = make(chan struct{})
		s.queueDoneCh = make(chan struct{})
		go s.runPublishQueueWorker()
	})
}

func (s *MigrateService) runPublishQueueWorker() {
	defer close(s.queueDoneCh)

	cfg := s.resolvePublishQueueConfig()
	intervalSec := clampInt(cfg.MonitorIntervalSec, 5, 3600)
	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	logx.Infof(publishQueueLogModule, "发布队列线程已启动 interval=%ds enabled=%v workers=%d", intervalSec, cfg.Enabled, clampInt(cfg.MaxWorkers, 1, 20))

	lastCleanup := time.Now()
	for {
		select {
		case <-s.queueStopCh:
			logx.Infof(publishQueueLogModule, "发布队列线程已停止")
			return
		case <-ticker.C:
			cfg = s.resolvePublishQueueConfig()
			newInterval := clampInt(cfg.MonitorIntervalSec, 5, 3600)
			if newInterval != intervalSec {
				intervalSec = newInterval
				ticker.Reset(time.Duration(intervalSec) * time.Second)
				logx.Infof(publishQueueLogModule, "队列轮询间隔已更新 interval=%ds", intervalSec)
			}

			if !cfg.Enabled {
				continue
			}

			s.drainPublishQueueOnce(cfg)

			if cfg.TaskCleanupHours > 0 && time.Since(lastCleanup) >= 30*time.Minute {
				lastCleanup = time.Now()
				cutoff := time.Now().Add(-time.Duration(cfg.TaskCleanupHours) * time.Hour)
				if deleted, err := s.queueRepo.CleanupFinishedTasks(cutoff); err != nil {
					logx.Warnf(publishQueueLogModule, "清理旧队列任务失败 err=%v", err)
				} else if deleted > 0 {
					logx.Infof(publishQueueLogModule, "已清理旧队列任务 deleted=%d cutoff=%s", deleted, cutoff.Format(time.RFC3339))
				}
			}
		}
	}
}

// EnqueuePublishQueue 将“选择站点发布”步骤生成的 payload 写入发布队列，等待后台触发。
// 参数/返回：payload 与批量发布接口一致（支持 targetSites/targetSite），必须包含 task_id 与 upload_data；返回入队结果与 HTTP 状态码。
// 失败场景：参数缺失、上下文过期、队列已满或入库失败时返回对应错误。
// 副作用：写入 publish_queue_tasks 表。
func (s *MigrateService) EnqueuePublishQueue(payload map[string]any) (map[string]any, int) {
	if s == nil || s.queueRepo == nil || s.queueRepo.DB() == nil {
		return map[string]any{"success": false, "message": "队列服务未初始化"}, 500
	}
	if payload == nil {
		payload = map[string]any{}
	}

	cfg := s.resolvePublishQueueConfig()
	if !cfg.Enabled {
		return map[string]any{"success": false, "message": "队列功能已关闭"}, 400
	}

	taskID := strings.TrimSpace(processingshared.ToString(payload["task_id"], processingshared.ToString(payload["taskId"], "")))
	if taskID == "" {
		return map[string]any{"success": false, "message": "缺少 task_id 参数"}, 400
	}

	uploadData, ok := payload["upload_data"].(map[string]any)
	if !ok || uploadData == nil {
		return map[string]any{"success": false, "message": "缺少 upload_data 参数"}, 400
	}

	ctx, ok := s.contextState.Get(taskID)
	if !ok {
		return map[string]any{"success": false, "message": "任务上下文已过期，请重新打开编辑面板后再加入队列"}, 400
	}

	targetSites := processingshared.ToStringSlice(payload["targetSites"])
	if len(targetSites) == 0 {
		targetSites = processingshared.ToStringSlice(payload["target_sites"])
	}
	if len(targetSites) == 0 {
		if single := strings.TrimSpace(processingshared.ToString(payload["targetSite"], "")); single != "" {
			targetSites = []string{single}
		}
	}
	if len(targetSites) == 0 {
		return map[string]any{"success": false, "message": "缺少 targetSites 参数"}, 400
	}

	if cfg.MaxQueueSize > 0 {
		active, err := s.queueRepo.CountActiveTasks()
		if err != nil {
			logx.Warnf(publishQueueLogModule, "统计队列任务失败 err=%v", err)
		} else if active+int64(len(targetSites)) > cfg.MaxQueueSize {
			return map[string]any{"success": false, "message": fmt.Sprintf("队列已满（当前 %d / 上限 %d）", active, cfg.MaxQueueSize)}, 400
		}
	}

	groupID := s.newID("queue")
	now := time.Now()
	nowText := now.Format(repository.PublishQueueTimeLayout)

	sourceSite := strings.TrimSpace(processingshared.ToString(payload["sourceSite"], processingshared.ToString(payload["source_site"], "")))
	if sourceSite == "" {
		sourceSite = strings.TrimSpace(ctx.SourceNickname)
	}
	if sourceSite == "" {
		sourceSite = strings.TrimSpace(ctx.SiteName)
	}

	torrentID := strings.TrimSpace(ctx.TorrentID)
	if torrentID == "" {
		torrentID = strings.TrimSpace(processingshared.ToString(uploadData["torrent_id"], processingshared.ToString(payload["torrent_id"], "")))
	}

	title := strings.TrimSpace(processingshared.ToString(uploadData["title"], ""))
	if title == "" {
		title = strings.TrimSpace(ctx.Name)
	}
	subtitle := strings.TrimSpace(processingshared.ToString(uploadData["subtitle"], ""))

	downloaderID := strings.TrimSpace(processingshared.ToString(payload["downloaderId"], processingshared.ToString(payload["downloader_id"], "")))
	if downloaderID == "" {
		downloaderID = strings.TrimSpace(ctx.DownloaderID)
	}

	scene := strings.TrimSpace(processingshared.ToString(payload["publish_scene"], ""))
	trigger := "queue"

	normalizedPayload := map[string]any{}
	for key, value := range payload {
		normalizedPayload[key] = value
	}
	normalizedPayload["publish_trigger"] = "queue"
	normalizedPayload["queue_group_id"] = groupID

	uploadBytes, _ := json.Marshal(uploadData)
	ctxBytes, _ := json.Marshal(ctx)

	scheduledAt := (*time.Time)(nil)
	if rawScheduled := strings.TrimSpace(processingshared.ToString(payload["scheduled_at"], "")); rawScheduled != "" {
		if t, err := time.Parse(time.RFC3339, rawScheduled); err == nil {
			scheduledAt = &t
		} else if t, err := time.Parse("2006-01-02 15:04:05", rawScheduled); err == nil {
			scheduledAt = &t
		}
	}

	queueTasks := make([]repository.PublishQueueTask, 0, len(targetSites))
	for _, target := range targetSites {
		targetSite := strings.TrimSpace(target)
		if targetSite == "" {
			continue
		}

		taskPayload := map[string]any{}
		for key, value := range normalizedPayload {
			taskPayload[key] = value
		}
		taskPayload["targetSite"] = targetSite
		delete(taskPayload, "targetSites")
		delete(taskPayload, "target_sites")

		taskPayloadBytes, _ := json.Marshal(taskPayload)

		record := repository.PublishQueueTask{
			GroupID:        groupID,
			Status:         repository.PublishQueueStatusQueued,
			TaskID:         taskID,
			Trigger:        trigger,
			Scene:          scene,
			TorrentID:      torrentID,
			SourceSite:     sourceSite,
			TargetSite:     targetSite,
			DownloaderID:   downloaderID,
			Title:          title,
			Subtitle:       subtitle,
			PayloadJSON:    string(taskPayloadBytes),
			UploadDataJSON: string(uploadBytes),
			ContextJSON:    string(ctxBytes),
			AttemptCount:   0,
			NextRunAt:      nil,
			ScheduledAt:    nil,
			StartedAt:      nil,
			FinishedAt:     nil,
			LastError:      "",
			LastResult:     "",
			CreatedAt:      nowText,
			UpdatedAt:      nowText,
		}

		// 兼容 scheduled_at：同时设置 scheduled_at 与 next_run_at，避免不同 DB 的 NULL 排序差异。
		if scheduledAt != nil {
			value := scheduledAt.Format(repository.PublishQueueTimeLayout)
			record.ScheduledAt = &value
			record.NextRunAt = &value
		} else {
			value := nowText
			record.NextRunAt = &value
		}

		queueTasks = append(queueTasks, record)
	}

	if len(queueTasks) == 0 {
		return map[string]any{"success": false, "message": "没有可入队的站点"}, 400
	}

	createdTasks, err := s.queueRepo.EnqueueTasks(queueTasks)
	if err != nil {
		return map[string]any{"success": false, "message": "入队失败: " + err.Error()}, 500
	}

	if len(createdTasks) > 0 && s.publishLogRepo != nil {
		for _, task := range createdTasks {
			if task.ID <= 0 {
				continue
			}
			taskID := task.ID
			message := "等待发布"
			if task.ScheduledAt != nil && strings.TrimSpace(*task.ScheduledAt) != "" {
				message = "等待发布（计划时间：" + strings.TrimSpace(*task.ScheduledAt) + "）"
			}

			entry := repository.PublishLogEntry{
				Trigger:       "queue",
				Scene:         task.Scene,
				QueueTaskID:   &taskID,
				TaskID:        task.TaskID,
				TorrentID:     task.TorrentID,
				SourceSite:    task.SourceSite,
				TargetSite:    task.TargetSite,
				DownloaderID:  task.DownloaderID,
				Title:         task.Title,
				Subtitle:      task.Subtitle,
				Status:        "queued",
				ResultURL:     "",
				Logs:          message,
				AutoAddResult: "",
				CostMS:        0,
				CreatedAt:     task.CreatedAt,
				UpdatedAt:     task.UpdatedAt,
			}

			if _, err := s.publishLogRepo.Insert(&entry); err != nil {
				logx.Warnf(publishLogModule, "写入队列等待日志失败 queue_task_id=%d target=%s err=%v", taskID, task.TargetSite, err)
			}
		}
	}

	logx.Infof(publishQueueLogModule, "已入队 group_id=%s task_id=%s torrent_id=%s sites=%d", groupID, taskID, torrentID, len(queueTasks))
	return map[string]any{
		"success":  true,
		"message":  fmt.Sprintf("已加入队列（%d 个站点）", len(queueTasks)),
		"group_id": groupID,
		"count":    len(queueTasks),
	}, 200
}

func (s *MigrateService) drainPublishQueueOnce(cfg publishQueueConfig) {
	if s == nil || s.queueRepo == nil {
		return
	}

	workers := clampInt(cfg.MaxWorkers, 1, 20)
	now := time.Now()

	latestSpeeds := map[string]float64{}
	if cfg.TriggerUploadSpeedBelowMBps > 0 && s.statsRepo != nil {
		if rows, err := s.statsRepo.QueryLatestSpeeds(); err == nil {
			for _, row := range rows {
				id := strings.TrimSpace(row.DownloaderID)
				if id == "" {
					continue
				}
				latestSpeeds[id] = row.UploadSpeed
			}
		}
	}

	for i := 0; i < workers; i++ {
		task, ok, err := s.queueRepo.ClaimNextRunnableTask(now)
		if err != nil {
			logx.Warnf(publishQueueLogModule, "领取队列任务失败 err=%v", err)
			return
		}
		if !ok || task == nil {
			return
		}
		s.executePublishQueueTask(cfg, *task, latestSpeeds)
	}
}

func (s *MigrateService) executePublishQueueTask(cfg publishQueueConfig, taskRecord repository.PublishQueueTask, latestSpeeds map[string]float64) {
	if s == nil || s.queueRepo == nil {
		return
	}

	taskID := taskRecord.ID
	if taskID <= 0 {
		return
	}

	payload := map[string]any{}
	if err := json.Unmarshal([]byte(taskRecord.PayloadJSON), &payload); err != nil {
		reason := "payload_json 解析失败: " + err.Error()
		_ = s.queueRepo.UpdateTaskAfterFailure(taskID, taskRecord.AttemptCount+1, nil, reason, "")
		if s.publishLogRepo != nil {
			_ = s.publishLogRepo.UpdateStatusAndLogsByQueueTaskID(taskID, "failed", reason)
		}
		return
	}

	uploadData := map[string]any{}
	_ = json.Unmarshal([]byte(taskRecord.UploadDataJSON), &uploadData)
	ctx := publishworkflow.Context{}
	_ = json.Unmarshal([]byte(taskRecord.ContextJSON), &ctx)

	payload["publish_trigger"] = "queue"
	payload["queue_task_id"] = taskID
	payload["queue_group_id"] = taskRecord.GroupID
	payload["targetSite"] = strings.TrimSpace(processingshared.ToString(payload["targetSite"], taskRecord.TargetSite))
	payload["upload_data"] = uploadData

	downloaderID := s.resolveQueueTaskDownloaderID(taskRecord, payload, ctx)
	if strings.TrimSpace(downloaderID) != "" {
		stats, err := publishguard.CheckDownloaderGateStats(downloaderID)
		if err != nil {
			nextRunAt := time.Now().Add(time.Duration(clampInt(cfg.MonitorIntervalSec, 5, 3600)) * time.Second)
			reason := "预检查统计失败: " + err.Error()
			_ = s.queueRepo.UpdateTaskAfterRequeue(taskID, nextRunAt, reason, "")
			if s.publishLogRepo != nil {
				_ = s.publishLogRepo.UpdateStatusAndLogsByQueueTaskID(taskID, "queued", reason)
			}
			logx.Warnf(publishQueueLogModule, "队列任务等待预检查恢复 id=%d downloader_id=%s err=%v", taskID, downloaderID, err)
			return
		}
		if !stats.CanContinue {
			nextRunAt := time.Now().Add(time.Duration(clampInt(cfg.MonitorIntervalSec, 5, 3600)) * time.Second)
			reason := strings.TrimSpace(stats.Message)
			if reason == "" {
				reason = "已触发限制"
			}
			waitMessage := "发布前限制: " + reason
			_ = s.queueRepo.UpdateTaskAfterRequeue(taskID, nextRunAt, waitMessage, "")
			if s.publishLogRepo != nil {
				_ = s.publishLogRepo.UpdateStatusAndLogsByQueueTaskID(taskID, "queued", waitMessage)
			}
			logx.Infof(publishQueueLogModule, "队列任务等待限制解除 id=%d downloader_id=%s next_run_at=%s", taskID, downloaderID, nextRunAt.Format(time.RFC3339))
			return
		}

		recentOK := false
		if cfg.TriggerRecentCountBelow > 0 {
			recentOK = stats.RecentCount < cfg.TriggerRecentCountBelow
		}

		speedOK := false
		if cfg.TriggerUploadSpeedBelowMBps > 0 {
			thresholdBytes := cfg.TriggerUploadSpeedBelowMBps * 1024 * 1024
			currentSpeed := latestSpeeds[strings.TrimSpace(downloaderID)]
			speedOK = currentSpeed <= thresholdBytes
		}

		if cfg.TriggerRecentCountBelow <= 0 && cfg.TriggerUploadSpeedBelowMBps <= 0 {
			recentOK = true
		}

		if !recentOK && !speedOK {
			nextRunAt := time.Now().Add(time.Duration(clampInt(cfg.MonitorIntervalSec, 5, 3600)) * time.Second)
			reason := "等待触发条件"
			_ = s.queueRepo.UpdateTaskAfterRequeue(taskID, nextRunAt, reason, "")
			if s.publishLogRepo != nil {
				_ = s.publishLogRepo.UpdateStatusAndLogsByQueueTaskID(taskID, "queued", reason)
			}
			return
		}
	}

	targetSite := strings.TrimSpace(processingshared.ToString(payload["targetSite"], taskRecord.TargetSite))
	torrentID := strings.TrimSpace(processingshared.ToString(payload["torrent_id"], taskRecord.TorrentID))
	if torrentID == "" {
		torrentID = strings.TrimSpace(processingshared.ToString(uploadData["torrent_id"], ctx.TorrentID))
	}

	execTaskID := strings.TrimSpace(taskRecord.TaskID)
	if execTaskID == "" {
		execTaskID = fmt.Sprintf("queue-%d", taskID)
	}

	logx.Infof(publishQueueLogModule, "开始执行队列任务 id=%d torrent_id=%s target_site=%s attempt=%d", taskID, torrentID, targetSite, taskRecord.AttemptCount)

	startedAt := time.Now()
	result, status := publishworkflow.ExecutePublishWithContext(
		publishworkflow.PublishWithContextInput{
			TargetSite:  targetSite,
			TaskID:      execTaskID,
			Payload:     payload,
			UploadData:  uploadData,
			Context:     ctx,
			TorrentPath: "",
		},
		publishworkflow.PublishWithContextDeps{
			GetSiteByName: s.repo.GetSiteByName,
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
			AddToDownloader:         s.AddToDownloader,
			FindSiteNicknameByGroup: s.repo.FindSiteNicknameByGroup,
		},
	)
	cost := time.Since(startedAt)
	s.appendPublishLog(payload, execTaskID, torrentID, result, status, cost)

	encodedResult, _ := json.Marshal(result)
	resultText := strings.TrimSpace(string(encodedResult))
	logText := strings.TrimSpace(processingshared.ToString(result["logs"], processingshared.ToString(result["message"], "")))

	success := processingshared.ToBool(result["success"])
	isPreCheck := processingshared.ToBool(result["pre_check"])
	limitReached := processingshared.ToBool(result["limit_reached"])

	if success && status < 400 {
		if err := s.queueRepo.UpdateTaskAfterSuccess(taskID, resultText); err != nil {
			logx.Warnf(publishQueueLogModule, "标记任务成功失败 id=%d err=%v", taskID, err)
		}
		logx.Infof(publishQueueLogModule, "队列任务完成 id=%d success=true status=%d", taskID, status)
		return
	}

	if isPreCheck && limitReached && strings.Contains(logText, "发布前预检查触发限制") {
		nextRunAt := time.Now().Add(time.Duration(clampInt(cfg.MonitorIntervalSec, 5, 3600)) * time.Second)
		_ = s.queueRepo.UpdateTaskAfterRequeue(taskID, nextRunAt, logText, resultText)
		if s.publishLogRepo != nil {
			_ = s.publishLogRepo.UpdateStatusAndLogsByQueueTaskID(taskID, "queued", logText)
		}
		logx.Infof(publishQueueLogModule, "队列任务等待限制解除 id=%d next_run_at=%s", taskID, nextRunAt.Format(time.RFC3339))
		return
	}

	if isPreCheck && limitReached && strings.Contains(logText, "发布前标签限制") {
		_ = s.queueRepo.UpdateTaskAfterFailure(taskID, taskRecord.AttemptCount+1, nil, logText, resultText)
		if s.publishLogRepo != nil {
			_ = s.publishLogRepo.UpdateStatusAndLogsByQueueTaskID(taskID, "pre_check_limit", logText)
		}
		logx.Warnf(publishQueueLogModule, "队列任务失败（标签限制） id=%d", taskID)
		return
	}

	if status >= 400 && status < 500 {
		reason := fmt.Sprintf("请求失败 status=%d: %s", status, logText)
		_ = s.queueRepo.UpdateTaskAfterFailure(taskID, taskRecord.AttemptCount+1, nil, reason, resultText)
		if s.publishLogRepo != nil {
			_ = s.publishLogRepo.UpdateStatusAndLogsByQueueTaskID(taskID, "failed", reason)
		}
		logx.Warnf(publishQueueLogModule, "队列任务失败（不可重试） id=%d status=%d", taskID, status)
		return
	}

	attempt := taskRecord.AttemptCount + 1
	if cfg.MaxRetries >= 0 && attempt > cfg.MaxRetries {
		reason := fmt.Sprintf("超过最大重试次数(%d): %s", cfg.MaxRetries, logText)
		_ = s.queueRepo.UpdateTaskAfterFailure(taskID, attempt, nil, reason, resultText)
		if s.publishLogRepo != nil {
			_ = s.publishLogRepo.UpdateStatusAndLogsByQueueTaskID(taskID, "failed", reason)
		}
		logx.Warnf(publishQueueLogModule, "队列任务失败（达到最大重试） id=%d attempts=%d", taskID, attempt)
		return
	}

	delaySec := computeBackoffSeconds(cfg.RetryDelayBase, attempt, cfg.MaxRetryDelaySec)
	nextRunAt := time.Now().Add(time.Duration(delaySec) * time.Second)
	_ = s.queueRepo.UpdateTaskAfterFailure(taskID, attempt, &nextRunAt, logText, resultText)
	if s.publishLogRepo != nil {
		_ = s.publishLogRepo.UpdateStatusAndLogsByQueueTaskID(taskID, "queued", logText)
	}
	logx.Warnf(publishQueueLogModule, "队列任务失败，已重试入队 id=%d attempt=%d next_run_at=%s", taskID, attempt, nextRunAt.Format(time.RFC3339))
}

func (s *MigrateService) resolveQueueTaskDownloaderID(task repository.PublishQueueTask, payload map[string]any, ctx publishworkflow.Context) string {
	downloaderID := strings.TrimSpace(task.DownloaderID)
	if downloaderID == "" {
		downloaderID = strings.TrimSpace(processingshared.ToString(payload["downloaderId"], processingshared.ToString(payload["downloader_id"], "")))
	}
	if downloaderID == "" {
		downloaderID = strings.TrimSpace(ctx.DownloaderID)
	}

	root := map[string]any{}
	if s != nil && s.cfg != nil {
		root = s.cfg.Get()
	}
	if crossSeed, ok := root["cross_seed"].(map[string]any); ok && crossSeed != nil {
		if defaultID := strings.TrimSpace(processingshared.ToString(crossSeed["default_downloader"], "")); defaultID != "" {
			return defaultID
		}
	}

	return downloaderID
}

func (s *MigrateService) resolvePublishQueueConfig() publishQueueConfig {
	out := publishQueueConfig{
		Enabled:                     true,
		MaxQueueSize:                1000,
		MaxRetries:                  3,
		MaxRetryDelaySec:            60,
		MaxWorkers:                  1,
		MonitorIntervalSec:          30,
		RetryDelayBase:              2,
		TaskCleanupHours:            24,
		TriggerRecentCountBelow:     15,
		TriggerUploadSpeedBelowMBps: 0,
	}

	root := map[string]any{}
	if s != nil && s.cfg != nil {
		root = s.cfg.Get()
	}
	if root == nil {
		return out
	}
	raw, ok := root["downloader_queue"].(map[string]any)
	if !ok || raw == nil {
		return out
	}

	if value, exists := raw["enabled"]; exists {
		out.Enabled = processingshared.ToBool(value)
	}
	if value, exists := raw["max_queue_size"]; exists {
		if parsed := int64(processingshared.ToFloat(value)); parsed > 0 {
			out.MaxQueueSize = parsed
		}
	}
	if value, exists := raw["max_retries"]; exists {
		if parsed := int(processingshared.ToFloat(value)); parsed >= 0 {
			out.MaxRetries = parsed
		}
	}
	if value, exists := raw["max_retry_delay"]; exists {
		if parsed := int(processingshared.ToFloat(value)); parsed > 0 {
			out.MaxRetryDelaySec = parsed
		}
	}
	if value, exists := raw["max_workers"]; exists {
		if parsed := int(processingshared.ToFloat(value)); parsed > 0 {
			out.MaxWorkers = parsed
		}
	}
	if value, exists := raw["queue_monitor_interval"]; exists {
		if parsed := int(processingshared.ToFloat(value)); parsed > 0 {
			out.MonitorIntervalSec = parsed
		}
	}
	if value, exists := raw["retry_delay_base"]; exists {
		if parsed := int(processingshared.ToFloat(value)); parsed > 0 {
			out.RetryDelayBase = parsed
		}
	}
	if value, exists := raw["task_cleanup_hours"]; exists {
		if parsed := int(processingshared.ToFloat(value)); parsed > 0 {
			out.TaskCleanupHours = parsed
		}
	}
	if value, exists := raw["trigger_recent_count_below"]; exists {
		if parsed := int(processingshared.ToFloat(value)); parsed >= 0 {
			out.TriggerRecentCountBelow = parsed
		}
	}
	if value, exists := raw["trigger_upload_speed_below_mbps"]; exists {
		if parsed := processingshared.ToFloat(value); parsed >= 0 {
			out.TriggerUploadSpeedBelowMBps = parsed
		}
	}

	return out
}

func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func computeBackoffSeconds(base int, attempt int, maxDelay int) int {
	if attempt <= 0 {
		return clampInt(5, 1, maxDelay)
	}
	if base <= 1 {
		return clampInt(attempt*5, 1, maxDelay)
	}

	delay := 1
	for i := 0; i < attempt; i++ {
		delay *= base
		if maxDelay > 0 && delay >= maxDelay {
			return maxDelay
		}
	}
	if delay < 1 {
		delay = 1
	}
	if maxDelay > 0 && delay > maxDelay {
		delay = maxDelay
	}
	return delay
}
