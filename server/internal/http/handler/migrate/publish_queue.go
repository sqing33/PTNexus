package migrate

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// PublishQueueEnqueue 接收“加入队列”请求，将当前已审核的发布内容写入发种队列。
// 参数/返回：请求体为发布 payload（需包含 task_id/upload_data/targetSites）；返回入队结果与状态码。
// 失败场景：参数缺失、上下文过期或入库失败会返回 4xx/5xx。
// 副作用：写入 publish_queue_tasks，并由后台线程异步触发发布。
func (h *Handler) PublishQueueEnqueue(c *gin.Context) {
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	result, status := h.service.EnqueuePublishQueue(payload)
	if status == 0 {
		status = http.StatusOK
	}
	c.JSON(status, result)
}

// PublishQueueEnqueueBatch 接收“一站多种”批量入队请求，一次性写入多个队列任务。
// 参数/返回：请求体需包含 target_site_name 与 seeds；返回批量入队统计与批次标识。
// 失败场景：参数缺失、队列未初始化、入队失败会返回 4xx/5xx。
// 副作用：写入 publish_queue_tasks 与 publish_logs。
func (h *Handler) PublishQueueEnqueueBatch(c *gin.Context) {
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	result, status := h.service.EnqueuePublishQueueBatch(payload)
	if status == 0 {
		status = http.StatusOK
	}
	c.JSON(status, result)
}

// PublishQueueDeleteTask 删除（取消）一条 queued 队列任务。
// 参数/返回：queue_task_id 从 URL 参数读取；返回删除结果与状态码。
// 失败场景：任务不存在返回 404，任务非 queued 返回 409，参数非法返回 400。
// 副作用：更新 publish_queue_tasks 与 publish_logs。
func (h *Handler) PublishQueueDeleteTask(c *gin.Context) {
	rawID := strings.TrimSpace(c.Param("queue_task_id"))
	if rawID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 queue_task_id 参数"})
		return
	}
	queueTaskID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || queueTaskID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "queue_task_id 参数非法"})
		return
	}

	result, status := h.service.DeleteQueuedPublishTask(queueTaskID)
	if status == 0 {
		status = http.StatusOK
	}
	c.JSON(status, result)
}
