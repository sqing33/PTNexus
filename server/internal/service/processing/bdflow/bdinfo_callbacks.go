package bdflow

import (
	"errors"
	"strconv"
	"strings"
	"time"

	processingshared "github.com/pt-nexus/server-go/internal/service/processing/shared"
)

var (
	// ErrMissingTaskID 表示回调中缺少 task_id 参数。
	ErrMissingTaskID = errors.New("缺少 task_id 参数")
	// ErrTaskNotFound 表示回调任务不存在。
	ErrTaskNotFound = errors.New("任务不存在")
)

// CompletePayloadResult 定义 BDInfo 完成回调解析结果。
type CompletePayloadResult struct {
	TaskID       string
	SeedID       string
	Success      bool
	MediaInfo    string
	ErrorMessage string
}

// ApplyProgressPayload 解析并应用进度回调。
// 参数/返回：payload 为回调正文；返回 taskID 与错误。
// 失败场景：缺少 task_id 或任务不存在时返回错误。
// 副作用：更新 BDInfoState 中任务进度。
func ApplyProgressPayload(state *BDInfoState, payload map[string]any, now time.Time) (string, error) {
	taskID := strings.TrimSpace(processingshared.ToString(payload["task_id"], ""))
	if taskID == "" {
		return "", ErrMissingTaskID
	}
	updated := false
	if state != nil {
		updated = state.UpdateProgress(
			taskID,
			processingshared.ToFloat(payload["progress_percent"]),
			processingshared.ToString(payload["current_file"], ""),
			processingshared.ToString(payload["elapsed_time"], ""),
			processingshared.ToString(payload["remaining_time"], ""),
			now,
		)
	}
	if !updated {
		return taskID, ErrTaskNotFound
	}

	// 兼容盒子代理：当进度回调携带 disc_size 时，一并写入内存状态，供前端实时展示“原盘体积”。
	if state != nil {
		discSize := toInt64FromAny(payload["disc_size"])
		if discSize > 0 {
			_ = state.UpdateDiscSize(taskID, discSize, now)
		}
	}
	return taskID, nil
}

// ApplyCompletePayload 解析并应用完成回调。
// 参数/返回：payload 为回调正文；返回完成结果与错误。
// 失败场景：缺少 task_id 或任务不存在时返回错误。
// 副作用：更新 BDInfoState 中任务完成状态。
func ApplyCompletePayload(state *BDInfoState, payload map[string]any, now time.Time) (CompletePayloadResult, error) {
	taskID := strings.TrimSpace(processingshared.ToString(payload["task_id"], ""))
	if taskID == "" {
		return CompletePayloadResult{}, ErrMissingTaskID
	}
	success := processingshared.ToBool(payload["success"])
	mediaInfo := processingshared.ToString(payload["bdinfo"], "")
	errorMessage := processingshared.ToString(payload["error_message"], "")

	seedID := ""
	ok := false
	if state != nil {
		seedID, ok = state.ApplyCallbackCompletion(taskID, success, mediaInfo, errorMessage, now)
	}
	if !ok {
		return CompletePayloadResult{TaskID: taskID}, ErrTaskNotFound
	}

	return CompletePayloadResult{
		TaskID:       taskID,
		SeedID:       seedID,
		Success:      success,
		MediaInfo:    mediaInfo,
		ErrorMessage: errorMessage,
	}, nil
}

func toInt64FromAny(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case uint64:
		if typed > uint64(^uint64(0)>>1) {
			return 0
		}
		return int64(typed)
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0
		}
		if parsed, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			return parsed
		}
		return 0
	case []byte:
		trimmed := strings.TrimSpace(string(typed))
		if trimmed == "" {
			return 0
		}
		if parsed, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			return parsed
		}
		return 0
	default:
		text := strings.TrimSpace(processingshared.ToString(value, ""))
		if text == "" {
			return 0
		}
		if parsed, err := strconv.ParseInt(text, 10, 64); err == nil {
			return parsed
		}
		return 0
	}
}
