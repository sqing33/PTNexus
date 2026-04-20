package migrate

import "github.com/gin-gonic/gin"

// TaskMonitor 返回全局任务监控面板所需的聚合任务快照。
// 参数/返回：无参数；成功时返回 success/tasks。
// 失败场景：后端聚合任务状态失败时返回对应错误码。
// 副作用：无（只读）。
func (h *Handler) TaskMonitor(c *gin.Context) {
	result, status := h.service.TaskMonitorSnapshot()
	c.JSON(status, result)
}
