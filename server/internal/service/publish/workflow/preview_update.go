package workflow

import (
	"strings"

	processingpersist "github.com/pt-nexus/server-go/internal/service/processing/persist"
	processingshared "github.com/pt-nexus/server-go/internal/service/processing/shared"
)

// PreviewUpdateDeps 定义预览更新流程依赖。
type PreviewUpdateDeps struct {
	ContextState    *ContextState
	ReverseMappings map[string]any
	BuildSeedID     func(ctx Context) string
}

// UpdatePreviewData 处理转种面板预览更新请求。
// 参数/返回：payload 需包含 task_id 与 updated_data；deps 注入上下文状态与 seed_id 构造器；返回预览结果。
// 失败场景：task_id 无效、updated_data 缺失或上下文不存在时返回 400。
// 副作用：无数据库写入，仅构建预览数据。
func UpdatePreviewData(payload map[string]any, deps PreviewUpdateDeps) (map[string]any, int) {
	taskID := strings.TrimSpace(processingshared.ToString(payload["task_id"], ""))
	if taskID == "" {
		return map[string]any{"success": false, "message": "错误：无效或已过期的任务ID。"}, 400
	}

	updatedData, ok := payload["updated_data"].(map[string]any)
	if !ok || len(updatedData) == 0 {
		return map[string]any{"success": false, "message": "错误：缺少更新数据。"}, 400
	}

	if deps.ContextState == nil {
		return map[string]any{"success": false, "message": "错误：上下文状态不可用。"}, 500
	}
	ctx, exists := deps.ContextState.Get(taskID)
	if !exists {
		return map[string]any{"success": false, "message": "错误：无效或已过期的任务ID。"}, 400
	}

	seedID := ""
	if deps.BuildSeedID != nil {
		seedID = strings.TrimSpace(deps.BuildSeedID(ctx))
	}
	previewData := processingpersist.BuildUpdatedPreviewData(updatedData, seedID)
	return map[string]any{
		"success":          true,
		"message":          "预览数据更新成功",
		"data":             previewData,
		"reverse_mappings": deps.ReverseMappings,
	}, 200
}
