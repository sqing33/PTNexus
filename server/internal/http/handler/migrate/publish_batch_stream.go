package migrate

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *Handler) PublishBatchStream(c *gin.Context) {
	batchID := strings.TrimSpace(c.Param("batch_id"))
	if batchID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 batch_id 参数"})
		return
	}

	eventCh, ok := h.service.SubscribePublishEvents(batchID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任务不存在"})
		return
	}
	defer h.service.UnsubscribePublishEvents(batchID, eventCh)

	prepareSSE(c)
	if !writeSSEEvent(c, map[string]any{
		"type":        "connected",
		"batch_id":    batchID,
		"connectedAt": time.Now().Format("2006-01-02 15:04:05"),
	}) {
		return
	}

	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case event, open := <-eventCh:
			if !open {
				_ = writeSSEEvent(c, map[string]any{"type": "complete"})
				return
			}
			if _, exists := event["type"]; !exists {
				event["type"] = "message"
			}
			if !writeSSEEvent(c, event) {
				return
			}
		case <-heartbeatTicker.C:
			if !writeSSEEvent(c, map[string]any{"type": "heartbeat", "timestamp": time.Now().Unix()}) {
				return
			}
		}
	}
}
