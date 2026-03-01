package migrationflow

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/pt-nexus/server-go/internal/platform/logx"
	"github.com/pt-nexus/server-go/internal/repository"
	processingshared "github.com/pt-nexus/server-go/internal/service/processing/shared"
)

const publishLogModule = "发布-日志"

// InitPublishLogs 初始化发种日志依赖。
// 参数/返回：repo 用于写入与查询 publish_logs；无返回值。
// 失败场景：repo 为空时仅记录日志并跳过初始化，避免启动阶段 panic。
// 副作用：无。
func (s *MigrateService) InitPublishLogs(repo *repository.PublishLogRepository) {
	if s == nil {
		return
	}
	if repo == nil {
		logx.Warnf(publishLogModule, "初始化发种日志失败：repo 为空")
		return
	}
	s.publishLogRepo = repo
}

// ListPublishLogs 分页查询发种日志（供 UI “发种日志”页面使用）。
// 参数/返回：query 为筛选与分页条件；返回 success/data/total 等结构。
// 失败场景：日志仓储未初始化或查询失败时返回 5xx。
// 副作用：无（只读）。
func (s *MigrateService) ListPublishLogs(query repository.PublishLogQuery) (map[string]any, int) {
	if s == nil || s.publishLogRepo == nil {
		return map[string]any{"success": false, "message": "发种日志未初始化"}, 500
	}
	rows, total, err := s.publishLogRepo.List(query)
	if err != nil {
		return map[string]any{"success": false, "message": "查询发种日志失败: " + err.Error()}, 500
	}

	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = 20
	}

	return map[string]any{
		"success":    true,
		"data":       rows,
		"total":      total,
		"page":       query.Page,
		"page_size":  pageSize,
		"pageSize":   pageSize,
		"page_total": (total + int64(pageSize) - 1) / int64(pageSize),
	}, 200
}

func (s *MigrateService) appendPublishLog(payload map[string]any, ctxTaskID string, ctxTorrentID string, result map[string]any, statusCode int, cost time.Duration) {
	if s == nil || s.publishLogRepo == nil {
		return
	}

	trigger := strings.TrimSpace(processingshared.ToString(payload["publish_trigger"], "manual"))
	scene := strings.TrimSpace(processingshared.ToString(payload["publish_scene"], ""))

	targetSite := strings.TrimSpace(processingshared.ToString(payload["targetSite"], ""))
	sourceSite := strings.TrimSpace(processingshared.ToString(payload["sourceSite"], processingshared.ToString(payload["source_site"], "")))
	downloaderID := strings.TrimSpace(processingshared.ToString(payload["downloaderId"], processingshared.ToString(payload["downloader_id"], "")))

	uploadData, _ := payload["upload_data"].(map[string]any)
	if uploadData == nil {
		uploadData = map[string]any{}
	}
	title := strings.TrimSpace(processingshared.ToString(uploadData["title"], ""))
	subtitle := strings.TrimSpace(processingshared.ToString(uploadData["subtitle"], ""))

	queueTaskID := (*int64)(nil)
	if rawQueueID := processingshared.ToFloat(payload["queue_task_id"]); rawQueueID > 0 {
		value := int64(rawQueueID)
		queueTaskID = &value
	}

	logStatus := "failed"
	if result != nil {
		preCheck := processingshared.ToBool(result["pre_check"])
		limitReached := processingshared.ToBool(result["limit_reached"])
		if preCheck && limitReached {
			logStatus = "pre_check_limit"
		} else if statusCode == 200 && processingshared.ToBool(result["success"]) {
			autoEdited := false
			if processingshared.ToBool(result["auto_edit_executed"]) {
				if autoEditResult, ok := result["auto_edit_result"].(map[string]any); ok && autoEditResult != nil {
					autoEdited = processingshared.ToBool(autoEditResult["success"])
				}
			}

			if autoEdited {
				logStatus = "edited"
			} else if processingshared.ToBool(result["is_existing_torrent"]) {
				logStatus = "exists"
			} else {
				logStatus = "success"
			}
		}
	} else if statusCode == 200 {
		logStatus = "success"
	}

	resultURL := strings.TrimSpace(processingshared.ToString(result["url"], ""))
	logsText := strings.TrimSpace(processingshared.ToString(result["logs"], ""))

	autoAddJSON := ""
	if raw := result["auto_add_result"]; raw != nil {
		if encoded, err := json.Marshal(raw); err == nil {
			autoAddJSON = string(encoded)
		}
	}

	entry := repository.PublishLogEntry{
		Trigger:       trigger,
		Scene:         scene,
		QueueTaskID:   queueTaskID,
		TaskID:        ctxTaskID,
		TorrentID:     ctxTorrentID,
		SourceSite:    sourceSite,
		TargetSite:    targetSite,
		DownloaderID:  downloaderID,
		Title:         title,
		Subtitle:      subtitle,
		Status:        logStatus,
		ResultURL:     resultURL,
		Logs:          logsText,
		AutoAddResult: autoAddJSON,
		CostMS:        cost.Milliseconds(),
	}

	if queueTaskID != nil && *queueTaskID > 0 {
		if err := s.publishLogRepo.UpsertByQueueTaskID(&entry); err != nil {
			logx.Warnf(publishLogModule, "更新发种日志失败 queue_task_id=%d trigger=%s scene=%s target=%s err=%v", *queueTaskID, trigger, scene, targetSite, err)
		}
		return
	}

	if _, err := s.publishLogRepo.Insert(&entry); err != nil {
		logx.Warnf(publishLogModule, "写入发种日志失败 trigger=%s scene=%s target=%s err=%v", trigger, scene, targetSite, err)
	}
}
