package migrate

import (
	"net/http"

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
