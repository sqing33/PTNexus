package migrate

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server-go/internal/platform/logx"
)

const (
	dbSeedInfoHandlerLogModule   = "迁移接口-种子查询"
	fetchStoreHandlerLogModule   = "迁移接口-源站抓取"
	updateDBSeedHandlerLogModule = "迁移接口-种子更新"
)

func (h *Handler) GetDBSeedInfo(c *gin.Context) {
	torrentID := strings.TrimSpace(c.Query("torrent_id"))
	siteName := strings.TrimSpace(c.Query("site_name"))
	taskID := strings.TrimSpace(c.Query("task_id"))
	requestID := c.GetString("request_id")
	logx.Infof(dbSeedInfoHandlerLogModule, "收到请求 请求ID=%s torrent_id=%s site_name=%s task_id=%s", requestID, torrentID, siteName, taskID)
	result, status := h.service.GetDBSeedInfo(torrentID, siteName, taskID)
	if success, ok := result["success"].(bool); ok && !success {
		logx.Warnf(dbSeedInfoHandlerLogModule, "请求结束 请求ID=%s 状态码=%d success=%t error_code=%s", requestID, status, success, handlerToString(result["error_code"], ""))
	} else if status >= http.StatusBadRequest {
		logx.Warnf(dbSeedInfoHandlerLogModule, "请求结束 请求ID=%s 状态码=%d", requestID, status)
	} else {
		logx.Infof(dbSeedInfoHandlerLogModule, "请求结束 请求ID=%s 状态码=%d", requestID, status)
	}
	c.JSON(status, result)
}

func (h *Handler) FetchAndStore(c *gin.Context) {
	requestID := c.GetString("request_id")
	payload, ok := bindMapPayload(c)
	if !ok {
		logx.Warnf(fetchStoreHandlerLogModule, "请求体绑定失败 请求ID=%s", requestID)
		return
	}
	sourceSite := strings.TrimSpace(handlerToString(payload["sourceSite"], ""))
	searchTerm := strings.TrimSpace(handlerToString(payload["searchTerm"], ""))
	taskID := strings.TrimSpace(handlerToString(payload["task_id"], ""))
	logx.Infof(fetchStoreHandlerLogModule, "收到请求 请求ID=%s source_site=%s search_term=%s task_id=%s", requestID, sourceSite, searchTerm, taskID)
	result, status := h.service.FetchAndStore(payload)
	if success, ok := result["success"].(bool); ok && !success {
		logx.Warnf(fetchStoreHandlerLogModule, "请求结束 请求ID=%s source_site=%s search_term=%s 状态码=%d success=%t error_code=%s", requestID, sourceSite, searchTerm, status, success, handlerToString(result["error_code"], ""))
	} else if status >= http.StatusBadRequest {
		logx.Warnf(fetchStoreHandlerLogModule, "请求结束 请求ID=%s source_site=%s search_term=%s 状态码=%d", requestID, sourceSite, searchTerm, status)
	} else {
		logx.Infof(fetchStoreHandlerLogModule, "请求结束 请求ID=%s source_site=%s search_term=%s 状态码=%d", requestID, sourceSite, searchTerm, status)
	}
	c.JSON(status, result)
}

func (h *Handler) UpdateDBSeedInfo(c *gin.Context) {
	requestID := c.GetString("request_id")
	payload, ok := bindMapPayload(c)
	if !ok {
		logx.Warnf(updateDBSeedHandlerLogModule, "请求体绑定失败 请求ID=%s", requestID)
		return
	}
	torrentID := strings.TrimSpace(handlerToString(payload["torrent_id"], ""))
	siteName := strings.TrimSpace(handlerToString(payload["site_name"], ""))
	logx.Infof(updateDBSeedHandlerLogModule, "收到请求 请求ID=%s torrent_id=%s site_name=%s", requestID, torrentID, siteName)
	result, status := h.service.UpdateDBSeedInfo(payload)
	if status >= http.StatusBadRequest {
		logx.Warnf(updateDBSeedHandlerLogModule, "请求结束 请求ID=%s torrent_id=%s site_name=%s 状态码=%d", requestID, torrentID, siteName, status)
	} else {
		logx.Infof(updateDBSeedHandlerLogModule, "请求结束 请求ID=%s torrent_id=%s site_name=%s 状态码=%d", requestID, torrentID, siteName, status)
	}
	c.JSON(status, result)
}

func (h *Handler) Publish(c *gin.Context) {
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	result, status := h.service.Publish(payload)
	c.JSON(status, result)
}

func (h *Handler) PublishBatchStart(c *gin.Context) {
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	result, status := h.service.StartPublishBatch(payload)
	c.JSON(status, result)
}

func (h *Handler) PublishBatchStatus(c *gin.Context) {
	batchID := strings.TrimSpace(c.Param("batch_id"))
	if batchID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 batch_id 参数"})
		return
	}
	result, status := h.service.PublishBatchStatus(batchID)
	c.JSON(status, result)
}

func (h *Handler) PublishBatchCancel(c *gin.Context) {
	batchID := strings.TrimSpace(c.Param("batch_id"))
	if batchID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 batch_id 参数"})
		return
	}
	result, status := h.service.PublishBatchCancel(batchID)
	c.JSON(status, result)
}
