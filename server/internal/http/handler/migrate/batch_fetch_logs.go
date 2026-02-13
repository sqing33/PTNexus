package migrate

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *Handler) BatchFetchSeedData(c *gin.Context) {
	payload := struct {
		TorrentNames   []string `json:"torrentNames"`
		SourcePriority []string `json:"sourcePriority"`
	}{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	result, status := h.service.StartBatchFetch(payload.TorrentNames, payload.SourcePriority)
	c.JSON(status, result)
}

func (h *Handler) BatchFetchProgress(c *gin.Context) {
	taskID := strings.TrimSpace(c.Query("task_id"))
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 task_id 参数"})
		return
	}
	result, status := h.service.BatchFetchProgress(taskID)
	c.JSON(status, result)
}

func (h *Handler) LogsStream(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 task_id 参数"})
		return
	}

	logCh := h.service.GetOrCreateLogStream(taskID)

	prepareSSE(c)
	if !writeSSEEvent(c, map[string]any{
		"type":        "connected",
		"task_id":     taskID,
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
		case entry, open := <-logCh:
			if !open {
				_ = writeSSEEvent(c, map[string]any{"type": "complete"})
				return
			}
			payload := map[string]any{"type": "log"}
			for key, value := range entry {
				payload[key] = value
			}
			if !writeSSEEvent(c, payload) {
				return
			}
		case <-heartbeatTicker.C:
			if !writeSSEEvent(c, map[string]any{"type": "heartbeat", "timestamp": time.Now().Unix()}) {
				return
			}
		}
	}
}
