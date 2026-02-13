package bdflow

import (
	"errors"
	"time"
)

// HandleBDInfoProgressCallback 处理 BDInfo 进度回调接口响应。
// 参数/返回：state 为内存任务状态，payload 为回调参数；返回接口响应体与状态码。
// 失败场景：缺少 task_id 返回 400；任务不存在返回 404。
// 副作用：更新内存任务进度。
func HandleBDInfoProgressCallback(state *BDInfoState, payload map[string]any, now time.Time) (map[string]any, int) {
	taskID, err := ApplyProgressPayload(state, payload, now)
	if err != nil {
		if errors.Is(err, ErrMissingTaskID) {
			return map[string]any{"success": false, "message": "缺少 task_id 参数"}, 400
		}
		return map[string]any{"success": false, "message": "任务不存在: " + taskID}, 404
	}
	return map[string]any{"success": true, "message": "进度更新成功"}, 200
}

// HandleBDInfoCompleteCallback 处理 BDInfo 完成回调接口响应，并回写数据库。
// 参数/返回：state 为内存任务状态，repo 为写库依赖，payload 为回调参数；返回接口响应体与状态码。
// 失败场景：缺少 task_id 返回 400；任务不存在返回 404。
// 副作用：更新内存任务状态并持久化完成结果到 seed_parameters。
func HandleBDInfoCompleteCallback(state *BDInfoState, repo BDInfoCallbackRepo, payload map[string]any, now time.Time) (map[string]any, int) {
	result, err := ApplyCompletePayload(state, payload, now)
	if err != nil {
		if errors.Is(err, ErrMissingTaskID) {
			return map[string]any{"success": false, "message": "缺少 task_id 参数"}, 400
		}
		return map[string]any{"success": false, "message": "任务不存在: " + result.TaskID}, 404
	}

	PersistBDInfoCompleteBySeedID(repo, result.SeedID, result.Success, result.MediaInfo, result.ErrorMessage, now)
	return map[string]any{"success": true, "message": "完成状态更新成功"}, 200
}
