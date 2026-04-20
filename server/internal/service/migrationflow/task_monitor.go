package migrationflow

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pt-nexus/server/internal/repository"
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
)

// TaskMonitorSnapshot 汇总当前可观测的后台任务状态。
// 参数/返回：无参数；返回适合前端任务面板消费的任务列表与状态码。
// 失败场景：数据库查询失败时返回 500。
// 副作用：读取内存态任务状态与数据库中的发布队列/日志。
func (s *MigrateService) TaskMonitorSnapshot() (map[string]any, int) {
	tasks := make([]map[string]any, 0)

	for _, task := range s.publishState.Snapshot() {
		tasks = append(tasks, buildBatchPublishTaskEntry(task))
	}

	for taskID, task := range s.batchFetchState.SnapshotAll() {
		tasks = append(tasks, buildBatchFetchTaskEntry(taskID, task))
	}

	bdinfoTasks, _ := s.bdinfoState.Snapshot()
	for _, task := range bdinfoTasks {
		tasks = append(tasks, buildBDInfoTaskEntry(task))
	}

	if s.queueRepo != nil {
		activeQueueTasks, err := s.queueRepo.ListActiveTasks(20)
		if err != nil {
			return map[string]any{"success": false, "message": "读取发布队列失败: " + err.Error()}, 500
		}
		for _, task := range activeQueueTasks {
			tasks = append(tasks, buildQueueTaskEntry(task))
		}
	}

	if s.publishLogRepo != nil {
		attentionLogs, err := s.publishLogRepo.ListRecentAttentionLogs(20)
		if err != nil {
			return map[string]any{"success": false, "message": "读取发种日志失败: " + err.Error()}, 500
		}
		for _, log := range attentionLogs {
			tasks = append(tasks, buildPublishLogTaskEntry(log))
		}
	}

	return map[string]any{
		"success": true,
		"tasks":   tasks,
	}, 200
}

func buildBatchPublishTaskEntry(task publishworkflow.BatchTask) map[string]any {
	status := "success"
	if task.IsRunning {
		status = "running"
	} else if task.Failed > 0 {
		status = "failed"
	}

	return map[string]any{
		"key":           "publish_batch:" + strings.TrimSpace(task.BatchID),
		"kind":          "publish_batch",
		"raw_id":        strings.TrimSpace(task.BatchID),
		"title":         "批量发布种子",
		"status":        status,
		"message":       buildBatchPublishMessage(task),
		"progress_text": fmt.Sprintf("%d/%d", task.Processed, task.Total),
		"started_at":    strings.TrimSpace(task.CreatedAt),
		"updated_at":    firstNonEmptyString(strings.TrimSpace(task.FinishedAt), strings.TrimSpace(task.CreatedAt)),
		"route_target": map[string]any{
			"path": "/publish-logs",
			"query": map[string]any{
				"scene": "multi_site",
			},
		},
	}
}

func buildBatchFetchTaskEntry(taskID string, task acquirefetch.BatchFetchTask) map[string]any {
	status := "success"
	if task.IsRunning {
		status = "running"
	} else if task.Failed > 0 {
		status = "failed"
	}

	return map[string]any{
		"key":           "batch_fetch:" + strings.TrimSpace(taskID),
		"kind":          "batch_fetch",
		"raw_id":        strings.TrimSpace(taskID),
		"title":         "批量获取种子数据",
		"status":        status,
		"message":       buildBatchFetchMessage(task),
		"progress_text": fmt.Sprintf("%d/%d", task.Processed, task.Total),
		"started_at":    "",
		"updated_at":    "",
	}
}

func buildBDInfoTaskEntry(task map[string]any) map[string]any {
	taskID := strings.TrimSpace(toStringLocal(task["task_id"]))
	taskStatus := strings.TrimSpace(toStringLocal(task["status"]))
	status := "running"
	if taskStatus == "completed" {
		status = "success"
	} else if taskStatus == "failed" {
		status = "failed"
	}

	return map[string]any{
		"key":           "bdinfo:" + taskID,
		"kind":          "bdinfo",
		"raw_id":        taskID,
		"title":         "BDInfo 任务",
		"status":        status,
		"message":       strings.TrimSpace(toStringLocal(task["current_file"])),
		"error":         strings.TrimSpace(toStringLocal(task["error"])),
		"progress_text": buildBDInfoProgressText(task),
		"started_at":    strings.TrimSpace(toStringLocal(task["started_at"])),
		"updated_at":    firstNonEmptyString(strings.TrimSpace(toStringLocal(task["updated_at"])), strings.TrimSpace(toStringLocal(task["started_at"]))),
	}
}

func buildQueueTaskEntry(task repository.PublishQueueTask) map[string]any {
	status := "running"
	if strings.TrimSpace(task.Status) == repository.PublishQueueStatusSuccess {
		status = "success"
	}

	return map[string]any{
		"key":           "publish_queue:" + strconv.FormatInt(task.ID, 10),
		"kind":          "publish_queue",
		"raw_id":        strconv.FormatInt(task.ID, 10),
		"title":         firstNonEmptyString(strings.TrimSpace(task.Title), "发布任务入队"),
		"status":        status,
		"message":       buildQueueTaskMessage(task),
		"error":         strings.TrimSpace(task.LastError),
		"progress_text": strings.TrimSpace(task.TargetSite),
		"started_at":    firstNonEmptyString(stringPtrValue(task.StartedAt), strings.TrimSpace(task.CreatedAt)),
		"updated_at":    strings.TrimSpace(task.UpdatedAt),
		"route_target": map[string]any{
			"path": "/publish-logs",
			"query": map[string]any{
				"scene":          strings.TrimSpace(task.Scene),
				"queue_group_id": strings.TrimSpace(task.GroupID),
			},
		},
	}
}

func buildPublishLogTaskEntry(log repository.PublishLogEntry) map[string]any {
	return map[string]any{
		"key":           "publish_log:" + strconv.FormatUint(log.ID, 10),
		"kind":          "publish_log",
		"raw_id":        strconv.FormatUint(log.ID, 10),
		"title":         firstNonEmptyString(strings.TrimSpace(log.Title), "发种失败"),
		"status":        "failed",
		"message":       firstNonEmptyString(strings.TrimSpace(log.TargetSite), strings.TrimSpace(log.SourceSite)),
		"error":         strings.TrimSpace(log.Logs),
		"progress_text": strings.TrimSpace(log.Status),
		"started_at":    strings.TrimSpace(log.CreatedAt),
		"updated_at":    strings.TrimSpace(log.UpdatedAt),
		"route_target": map[string]any{
			"path": "/publish-logs",
			"query": map[string]any{
				"scene":       strings.TrimSpace(log.Scene),
				"target_site": strings.TrimSpace(log.TargetSite),
				"search":      strings.TrimSpace(log.TorrentID),
			},
		},
	}
}

func buildBatchPublishMessage(task publishworkflow.BatchTask) string {
	if task.IsRunning {
		return "批量发布进行中"
	}
	if task.Failed > 0 {
		return "批量发布已结束，存在失败站点"
	}
	return "批量发布已完成"
}

func buildBatchFetchMessage(task acquirefetch.BatchFetchTask) string {
	if task.IsRunning {
		return "批量抓取进行中"
	}
	if task.Failed > 0 {
		return "批量抓取已结束，存在失败条目"
	}
	return "批量抓取已完成"
}

func buildBDInfoProgressText(task map[string]any) string {
	progress := strings.TrimSpace(toStringLocal(task["progress_percent"]))
	currentFile := strings.TrimSpace(toStringLocal(task["current_file"]))
	if progress == "" {
		return currentFile
	}
	if currentFile == "" {
		return progress + "%"
	}
	return progress + "% / " + currentFile
}

func buildQueueTaskMessage(task repository.PublishQueueTask) string {
	switch strings.TrimSpace(task.Status) {
	case repository.PublishQueueStatusQueued:
		return "等待发布"
	case repository.PublishQueueStatusRunning:
		return "发布中"
	case repository.PublishQueueStatusSuccess:
		return "已发布成功"
	default:
		return strings.TrimSpace(task.Status)
	}
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func toStringLocal(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", value)
	}
}
