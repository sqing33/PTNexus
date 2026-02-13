package bdflow

import (
	"strings"
)

// EnrichBDInfoStatusResponseWithTaskProgress 将内存中的 BDInfo 任务进度合并到状态响应中。
// 参数/返回：response 为 QueryBDInfoStatus 返回的 response；taskID 为数据库记录中的任务 ID；state 为内存状态。
// 失败场景：response/state 为空、任务不存在时直接跳过。
// 副作用：会原地修改 response map。
func EnrichBDInfoStatusResponseWithTaskProgress(response map[string]any, taskID string, state *BDInfoState) {
	if response == nil || state == nil {
		return
	}
	trimmedTaskID := strings.TrimSpace(taskID)
	if trimmedTaskID == "" {
		return
	}

	task, ok := state.TaskStatus(trimmedTaskID)
	if !ok {
		return
	}

	response["task_status"] = map[string]any{
		"status":           task.Status,
		"progress_percent": task.ProgressPercent,
		"current_file":     task.CurrentFile,
		"elapsed_time":     task.ElapsedTime,
		"remaining_time":   task.RemainingTime,
		"disc_size":        task.DiscSize,
	}
	if task.Status == "processing_bdinfo" {
		response["progress_info"] = map[string]any{
			"progress_percent": task.ProgressPercent,
			"current_file":     task.CurrentFile,
			"elapsed_time":     task.ElapsedTime,
			"remaining_time":   task.RemainingTime,
			"disc_size":        task.DiscSize,
		}
	}
}

// EnrichBDInfoRecordsWithTaskProgress 将内存中的 BDInfo 任务进度合并到列表记录中。
// 参数/返回：records 为 QueryBDInfoRecords 返回的 records；state 为内存状态。
// 失败场景：records/state 为空时直接返回。
// 副作用：会原地修改 records 内每个 record map。
func EnrichBDInfoRecordsWithTaskProgress(records []map[string]any, state *BDInfoState) {
	if len(records) == 0 || state == nil {
		return
	}

	for _, record := range records {
		taskID := strings.TrimSpace(toStringAny(record["bdinfo_task_id"]))
		if taskID == "" {
			continue
		}
		task, ok := state.TaskStatus(taskID)
		if !ok || task.Status != "processing_bdinfo" {
			continue
		}
		record["progress_info"] = map[string]any{
			"progress_percent":     task.ProgressPercent,
			"elapsed_time":         task.ElapsedTime,
			"remaining_time":       task.RemainingTime,
			"disc_size":            task.DiscSize,
			"last_progress_update": task.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
	}
}
