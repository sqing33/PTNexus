package migrationflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	processingtitle "github.com/pt-nexus/server/internal/service/processing/title"
)

const (
	publishLogModule               = "发布-日志"
	batchCrossSeedTriggerPrefix    = "批量转种-"
	batchCrossSeedDefaultSceneName = "multi_torrent"
)

// ExternalPublishLogInput 表示“外部流程”直接写入 publish_logs 的最小字段集合。
// 说明：用于无法走标准 Publish 工作流，但仍希望在“发种日志”页面中留痕的场景（例如：批量转种前置过滤、抓取失败等）。
type ExternalPublishLogInput struct {
	Trigger      string
	Scene        string
	QueueGroupID string

	TaskID    string
	TorrentID string

	SourceSite   string
	TargetSite   string
	DownloaderID string

	Title    string
	Subtitle string

	Status    string
	ResultURL string
	Logs      string

	AutoAddResult string
	CostMS        int64
}

// NextBatchCrossSeedTrigger 计算下一次“批量转种”的触发标识（批量转种-<序号>）。
// 参数/返回：无参数；返回 trigger 字符串、批次序号与 error。
// 失败场景：发种日志仓储未初始化或查询失败返回 error。
// 副作用：读取 publish_logs。
func (s *MigrateService) NextBatchCrossSeedTrigger() (string, int, error) {
	if s == nil || s.publishLogRepo == nil {
		return "", 0, errors.New("发种日志未初始化")
	}

	maxNumber, err := s.publishLogRepo.MaxNumericTriggerSuffix(batchCrossSeedTriggerPrefix)
	if err != nil {
		return "", 0, err
	}

	next := maxNumber + 1
	if next < 1 {
		next = 1
	}
	return fmt.Sprintf("%s%d", batchCrossSeedTriggerPrefix, next), next, nil
}

// InsertExternalPublishLog 直接插入一条发种日志记录（不经过 Publish 工作流）。
// 参数/返回：input 为日志内容；返回 error 表示写入失败原因。
// 失败场景：发种日志仓储未初始化、写库失败返回 error。
// 副作用：写入 publish_logs。
func (s *MigrateService) InsertExternalPublishLog(input ExternalPublishLogInput) error {
	if s == nil || s.publishLogRepo == nil {
		return errors.New("发种日志未初始化")
	}

	scene := strings.TrimSpace(input.Scene)
	if scene == "" {
		scene = batchCrossSeedDefaultSceneName
	}

	entry := repository.PublishLogEntry{
		Trigger:       strings.TrimSpace(input.Trigger),
		Scene:         scene,
		QueueGroupID:  strings.TrimSpace(input.QueueGroupID),
		TaskID:        strings.TrimSpace(input.TaskID),
		TorrentID:     strings.TrimSpace(input.TorrentID),
		SourceSite:    s.normalizePublishLogSourceSite(input.SourceSite),
		TargetSite:    strings.TrimSpace(input.TargetSite),
		DownloaderID:  strings.TrimSpace(input.DownloaderID),
		Title:         strings.TrimSpace(input.Title),
		Subtitle:      strings.TrimSpace(input.Subtitle),
		Status:        strings.TrimSpace(input.Status),
		ResultURL:     strings.TrimSpace(input.ResultURL),
		Logs:          strings.TrimSpace(input.Logs),
		AutoAddResult: strings.TrimSpace(input.AutoAddResult),
		CostMS:        input.CostMS,
	}

	_, err := s.publishLogRepo.Insert(&entry)
	return err
}

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
	sourceSite := s.normalizePublishLogSourceSite(processingshared.ToString(payload["sourceSite"], processingshared.ToString(payload["source_site"], "")))
	downloaderID := strings.TrimSpace(processingshared.ToString(payload["downloaderId"], processingshared.ToString(payload["downloader_id"], "")))

	uploadData, _ := payload["upload_data"].(map[string]any)
	if uploadData == nil {
		uploadData = map[string]any{}
	}

	torrentID := strings.TrimSpace(ctxTorrentID)
	if torrentID == "" {
		torrentID = strings.TrimSpace(processingshared.ToString(payload["torrent_id"], processingshared.ToString(payload["torrentId"], "")))
	}
	if torrentID == "" {
		torrentID = strings.TrimSpace(processingshared.ToString(uploadData["torrent_id"], processingshared.ToString(uploadData["torrentId"], "")))
	}
	title, subtitle := resolvePublishLogTitleFromUploadData(uploadData, torrentID)
	if title == "" {
		title = torrentID
	}

	queueTaskID := (*int64)(nil)
	if rawQueueID := processingshared.ToFloat(payload["queue_task_id"]); rawQueueID > 0 {
		value := int64(rawQueueID)
		queueTaskID = &value
	}
	queueGroupID := strings.TrimSpace(processingshared.ToString(payload["queue_group_id"], ""))

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
		QueueGroupID:  queueGroupID,
		TaskID:        ctxTaskID,
		TorrentID:     torrentID,
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
		if existing, ok, err := s.publishLogRepo.FindLatestByQueueTaskID(*queueTaskID); err == nil && ok && existing != nil {
			if strings.TrimSpace(entry.Title) == "" && strings.TrimSpace(existing.Title) != "" {
				entry.Title = existing.Title
			}
			if strings.TrimSpace(entry.Subtitle) == "" && strings.TrimSpace(existing.Subtitle) != "" {
				entry.Subtitle = existing.Subtitle
			}
		}
		if err := s.publishLogRepo.UpsertByQueueTaskID(&entry); err != nil {
			logx.Warnf(publishLogModule, "更新发种日志失败 queue_task_id=%d trigger=%s scene=%s target=%s err=%v", *queueTaskID, trigger, scene, targetSite, err)
		}
		return
	}

	if _, err := s.publishLogRepo.Insert(&entry); err != nil {
		logx.Warnf(publishLogModule, "写入发种日志失败 trigger=%s scene=%s target=%s err=%v", trigger, scene, targetSite, err)
	}
}

func resolvePublishLogTitleFromUploadData(uploadData map[string]any, fallbackTitle string) (string, string) {
	subtitle := strings.TrimSpace(processingshared.ToString(uploadData["subtitle"], ""))

	title := resolvePreviewTitleFromFinalParams(uploadData["final_publish_parameters"])
	baseTitle := strings.TrimSpace(firstNonEmptyString(
		processingshared.ToString(uploadData["title"], ""),
		processingshared.ToString(uploadData["original_main_title"], ""),
		processingshared.ToString(uploadData["name"], ""),
		fallbackTitle,
	))

	if title == "" {
		titleComponents := parseUploadTitleComponents(uploadData["title_components"])
		if len(titleComponents) > 0 {
			completed := processingtitle.CompleteTitleComponents(titleComponents, baseTitle)
			rebuilt := strings.TrimSpace(processingtitle.BuildPreviewTitleFromTitleComponents(completed, baseTitle))
			if rebuilt != "" && rebuilt != "-NOGROUP" {
				title = rebuilt
			}
		}
	}

	if title == "" {
		title = baseTitle
	}
	return strings.TrimSpace(title), subtitle
}

func resolvePreviewTitleFromFinalParams(raw any) string {
	parseMap := func(values map[string]any) string {
		return strings.TrimSpace(firstNonEmptyString(
			processingshared.ToString(values["主标题 (预览)"], ""),
			processingshared.ToString(values["final_main_title"], ""),
			processingshared.ToString(values["title"], ""),
		))
	}

	switch typed := raw.(type) {
	case map[string]any:
		return parseMap(typed)
	case map[string]string:
		return strings.TrimSpace(firstNonEmptyString(
			typed["主标题 (预览)"],
			typed["final_main_title"],
			typed["title"],
		))
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return ""
		}
		decoded := map[string]any{}
		if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
			return ""
		}
		return parseMap(decoded)
	default:
		return ""
	}
}

func parseUploadTitleComponents(raw any) []any {
	switch typed := raw.(type) {
	case []any:
		return typed
	case []map[string]any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		return items
	case []map[string]string:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			converted := map[string]any{}
			for key, value := range item {
				converted[key] = value
			}
			items = append(items, converted)
		}
		return items
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return []any{}
		}
		decoded := []any{}
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			return decoded
		}
		return []any{}
	default:
		return []any{}
	}
}

func (s *MigrateService) normalizePublishLogSourceSite(value string) string {
	sourceSite := strings.TrimSpace(value)
	if sourceSite == "" {
		return ""
	}
	if s == nil || s.repo == nil {
		return sourceSite
	}

	siteInfo, err := s.repo.GetSiteByName(sourceSite)
	if err != nil || siteInfo == nil {
		return sourceSite
	}

	nickname := strings.TrimSpace(processingshared.ToString(siteInfo["nickname"], ""))
	if nickname == "" {
		return sourceSite
	}
	return nickname
}
