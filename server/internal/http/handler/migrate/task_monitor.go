package migrate

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// TaskMonitor 返回全局任务监控面板所需的聚合任务快照。
// 参数/返回：无参数；成功时返回 success/tasks。
// 失败场景：后端聚合任务状态失败时返回对应错误码。
// 副作用：无（只读）。
func (h *Handler) TaskMonitor(c *gin.Context) {
	result, status := h.service.TaskMonitorSnapshot()
	c.JSON(status, result)
}

// TaskMonitorAction 对任务监控中的可控后台任务执行手动结束或终止。
// 参数/返回：kind/raw_id/action 来自请求体；返回标准 success/message。
// 失败场景：参数缺失、不支持的动作或任务不存在时返回对应错误码。
// 副作用：更新对应内存态任务或队列任务状态。
func (h *Handler) TaskMonitorAction(c *gin.Context) {
	var payload struct {
		Kind   string `json:"kind"`
		RawID  string `json:"raw_id"`
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(400, gin.H{"success": false, "message": "请求参数格式错误"})
		return
	}

	kind := strings.TrimSpace(payload.Kind)
	rawID := strings.TrimSpace(payload.RawID)
	action := strings.TrimSpace(payload.Action)
	if kind == "" || rawID == "" || action == "" {
		c.JSON(400, gin.H{"success": false, "message": "缺少任务类型、任务标识或操作类型"})
		return
	}
	if action != "finish" && action != "terminate" {
		c.JSON(400, gin.H{"success": false, "message": "不支持的任务操作"})
		return
	}

	result, status := h.service.TaskMonitorAction(kind, rawID, action)
	c.JSON(status, result)
}
