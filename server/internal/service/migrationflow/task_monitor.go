package migrationflow

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
)

const taskMonitorActionHint = "此操作会请求取消或标记监控状态，不保证强制终止底层进程"

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

// TaskMonitorAction 对任务监控中的可控后台任务执行手动结束或终止。
// 参数/返回：kind 为任务类型；rawID 为任务原始标识；action 为 finish/terminate；返回标准响应与状态码。
// 失败场景：任务不存在返回 404；不支持的动作或状态返回 409；参数错误返回 400。
// 副作用：更新对应内存态任务或队列任务状态。
func (s *MigrateService) TaskMonitorAction(kind string, rawID string, action string) (map[string]any, int) {
	kind = strings.TrimSpace(kind)
	rawID = strings.TrimSpace(rawID)
	action = strings.TrimSpace(action)
	if kind == "" || rawID == "" || action == "" {
		return map[string]any{"success": false, "message": "缺少任务类型、任务标识或操作类型"}, 400
	}

	switch kind {
	case "publish_batch":
		return s.applyPublishBatchTaskAction(rawID, action)
	case "batch_fetch":
		return s.applyBatchFetchTaskAction(rawID, action)
	case "bdinfo":
		return s.applyBDInfoTaskAction(rawID, action)
	case "publish_queue":
		return s.applyPublishQueueTaskAction(rawID, action)
	default:
		return map[string]any{"success": false, "message": "当前任务类型不支持手动操作"}, 409
	}
}

func (s *MigrateService) applyPublishBatchTaskAction(batchID string, action string) (map[string]any, int) {
	if _, ok := s.publishState.Status(batchID); !ok {
		return map[string]any{"success": false, "message": "任务不存在"}, 404
	}

	now := time.Now()
	switch action {
	case "finish":
		s.publishState.Finish(batchID, now)
		return map[string]any{"success": true, "message": "批量发布任务已标记完成"}, 200
	case "terminate":
		s.publishState.Cancel(batchID)
		s.publishState.Finish(batchID, now)
		return map[string]any{"success": true, "message": "批量发布任务已请求取消并结束监控"}, 200
	default:
		return map[string]any{"success": false, "message": "不支持的任务操作"}, 400
	}
}

func (s *MigrateService) applyBatchFetchTaskAction(taskID string, action string) (map[string]any, int) {
	if action != "finish" && action != "terminate" {
		return map[string]any{"success": false, "message": "不支持的任务操作"}, 400
	}
	if !s.batchFetchState.Finish(taskID) {
		return map[string]any{"success": false, "message": "任务不存在"}, 404
	}
	return map[string]any{"success": true, "message": "批量获取任务已标记结束"}, 200
}

func (s *MigrateService) applyBDInfoTaskAction(taskID string, action string) (map[string]any, int) {
	if action == "finish" {
		return map[string]any{"success": false, "message": "BDInfo 任务不支持手动标记完成"}, 409
	}
	if action != "terminate" {
		return map[string]any{"success": false, "message": "不支持的任务操作"}, 400
	}
	if !s.bdinfoState.CleanupByTaskID(taskID, time.Now(), "已手动终止") {
		if _, ok := s.bdinfoState.TaskStatus(taskID); ok {
			return map[string]any{"success": false, "message": "仅处理中的 BDInfo 任务支持终止"}, 409
		}
		return map[string]any{"success": false, "message": "任务不存在"}, 404
	}
	return map[string]any{"success": true, "message": "BDInfo 任务已标记为手动终止"}, 200
}

func (s *MigrateService) applyPublishQueueTaskAction(rawID string, action string) (map[string]any, int) {
	if action == "finish" {
		return map[string]any{"success": false, "message": "发布队列任务不支持手动标记完成"}, 409
	}
	if action != "terminate" {
		return map[string]any{"success": false, "message": "不支持的任务操作"}, 400
	}

	queueTaskID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || queueTaskID <= 0 {
		return map[string]any{"success": false, "message": "缺少有效的队列任务标识"}, 400
	}
	result, status := s.DeleteQueuedPublishTask(queueTaskID)
	if status == 200 {
		result["message"] = "发布队列任务已取消"
	}
	return result, status
}

func buildBatchPublishTaskEntry(task publishworkflow.BatchTask) map[string]any {
	status := "success"
	if task.IsRunning {
		status = "running"
	} else if task.Failed > 0 {
		status = "failed"
	}

	entry := map[string]any{
		"key":           "publish_batch:" + strings.TrimSpace(task.BatchID),
		"kind":          "publish_batch",
		"raw_id":        strings.TrimSpace(task.BatchID),
		"title":         "批量发布种子",
		"status":        status,
		"message":       buildBatchPublishMessage(task),
		"progress_text": fmt.Sprintf("%d/%d", task.Processed, task.Total),
		"started_at":    strings.TrimSpace(task.CreatedAt),
		"updated_at":    firstNonEmptyString(strings.TrimSpace(task.FinishedAt), strings.TrimSpace(task.CreatedAt)),
	}
	if task.IsRunning {
		entry["actions"] = []string{"finish", "terminate"}
		entry["action_hint"] = taskMonitorActionHint
	}
	return entry
}

func buildBatchFetchTaskEntry(taskID string, task acquirefetch.BatchFetchTask) map[string]any {
	status := "success"
	if task.IsRunning {
		status = "running"
	} else if task.Failed > 0 {
		status = "failed"
	}

	entry := map[string]any{
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
	if task.IsRunning {
		entry["actions"] = []string{"finish", "terminate"}
		entry["action_hint"] = taskMonitorActionHint
	}
	return entry
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

	entry := map[string]any{
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
	if status == "running" {
		entry["actions"] = []string{"terminate"}
		entry["action_hint"] = taskMonitorActionHint
	}
	return entry
}

func buildQueueTaskEntry(task repository.PublishQueueTask) map[string]any {
	status := "running"
	if strings.TrimSpace(task.Status) == repository.PublishQueueStatusSuccess {
		status = "success"
	}

	entry := map[string]any{
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
	if strings.TrimSpace(task.Status) == repository.PublishQueueStatusQueued {
		entry["actions"] = []string{"terminate"}
		entry["action_hint"] = taskMonitorActionHint
	}
	return entry
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
