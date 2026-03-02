package migrate

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/platform/logx"
)

const (
	bdinfoStatusHandlerLogModule = "迁移接口-BDInfo状态"
	bdinfoSSEHandlerLogModule    = "迁移接口-BDInfoSSE"
	bdinfoTaskHandlerLogModule   = "迁移接口-BDInfo任务"
)

func (h *Handler) BDInfoRecords(c *gin.Context) {
	statusFilter := strings.TrimSpace(c.Query("status_filter"))
	page := 1
	pageSize := 20
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if raw := strings.TrimSpace(c.Query("pageSize")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}
	requestID := c.GetString("request_id")
	logx.Infof(bdinfoStatusHandlerLogModule, "收到记录查询 请求ID=%s status_filter=%s page=%d page_size=%d", requestID, statusFilter, page, pageSize)
	result, status := h.service.BDInfoRecords(statusFilter, page, pageSize)
	if status >= http.StatusBadRequest {
		logx.Warnf(bdinfoStatusHandlerLogModule, "记录查询结束 请求ID=%s status_filter=%s 状态码=%d", requestID, statusFilter, status)
	} else {
		logx.Infof(bdinfoStatusHandlerLogModule, "记录查询结束 请求ID=%s status_filter=%s 状态码=%d", requestID, statusFilter, status)
	}
	c.JSON(status, result)
}

func (h *Handler) BDInfoStatus(c *gin.Context) {
	requestID := c.GetString("request_id")
	seedID := strings.TrimSpace(c.Param("seed_id"))
	if seedID == "" {
		logx.Warnf(bdinfoStatusHandlerLogModule, "状态查询参数缺失 请求ID=%s 缺少 seed_id", requestID)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 seed_id 参数"})
		return
	}
	result, status := h.service.BDInfoStatus(seedID)
	if status >= http.StatusBadRequest {
		logx.Warnf(bdinfoStatusHandlerLogModule, "状态查询结束 请求ID=%s seed_id=%s 状态码=%d", requestID, seedID, status)
	}
	c.JSON(status, result)
}

func (h *Handler) RefreshBDInfo(c *gin.Context) {
	requestID := c.GetString("request_id")
	seedID := strings.TrimSpace(c.Param("seed_id"))
	if seedID == "" {
		logx.Warnf(bdinfoTaskHandlerLogModule, "重试任务参数缺失 请求ID=%s 缺少 seed_id", requestID)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 seed_id 参数"})
		return
	}
	logx.Infof(bdinfoTaskHandlerLogModule, "收到重试任务 请求ID=%s seed_id=%s", requestID, seedID)
	result, status := h.service.RefreshBDInfo(seedID)
	if status >= http.StatusBadRequest {
		logx.Warnf(bdinfoTaskHandlerLogModule, "重试任务结束 请求ID=%s seed_id=%s 状态码=%d", requestID, seedID, status)
	} else {
		logx.Infof(bdinfoTaskHandlerLogModule, "重试任务结束 请求ID=%s seed_id=%s 状态码=%d", requestID, seedID, status)
	}
	c.JSON(status, result)
}

func (h *Handler) CleanupBDInfoProcess(c *gin.Context) {
	requestID := c.GetString("request_id")
	payload, ok := bindMapPayload(c)
	if !ok {
		logx.Warnf(bdinfoTaskHandlerLogModule, "清理任务请求体绑定失败 请求ID=%s", requestID)
		return
	}
	seedID := strings.TrimSpace(handlerToString(payload["seed_id"], ""))
	if seedID == "" {
		logx.Warnf(bdinfoTaskHandlerLogModule, "清理任务参数缺失 请求ID=%s 缺少 seed_id", requestID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 seed_id 参数"})
		return
	}
	logx.Infof(bdinfoTaskHandlerLogModule, "收到清理任务 请求ID=%s seed_id=%s", requestID, seedID)
	result, status := h.service.CleanupBDInfo(seedID)
	if status >= http.StatusBadRequest {
		logx.Warnf(bdinfoTaskHandlerLogModule, "清理任务结束 请求ID=%s seed_id=%s 状态码=%d", requestID, seedID, status)
	} else {
		logx.Infof(bdinfoTaskHandlerLogModule, "清理任务结束 请求ID=%s seed_id=%s 状态码=%d", requestID, seedID, status)
	}
	c.JSON(status, result)
}

func (h *Handler) RestartBDInfo(c *gin.Context) {
	requestID := c.GetString("request_id")
	payload, ok := bindMapPayload(c)
	if !ok {
		logx.Warnf(bdinfoTaskHandlerLogModule, "重启任务请求体绑定失败 请求ID=%s", requestID)
		return
	}
	seedID := strings.TrimSpace(handlerToString(payload["seed_id"], ""))
	if seedID == "" {
		logx.Warnf(bdinfoTaskHandlerLogModule, "重启任务参数缺失 请求ID=%s 缺少 seed_id", requestID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 seed_id 参数"})
		return
	}
	logx.Infof(bdinfoTaskHandlerLogModule, "收到重启任务 请求ID=%s seed_id=%s", requestID, seedID)
	result, status := h.service.RestartBDInfo(seedID)
	if status >= http.StatusBadRequest {
		logx.Warnf(bdinfoTaskHandlerLogModule, "重启任务结束 请求ID=%s seed_id=%s 状态码=%d", requestID, seedID, status)
	} else {
		logx.Infof(bdinfoTaskHandlerLogModule, "重启任务结束 请求ID=%s seed_id=%s 状态码=%d", requestID, seedID, status)
	}
	c.JSON(status, result)
}

func (h *Handler) BDInfoSSE(c *gin.Context) {
	requestID := c.GetString("request_id")
	seedID := strings.TrimSpace(c.Param("seed_id"))
	if seedID == "" {
		logx.Warnf(bdinfoSSEHandlerLogModule, "SSE 参数缺失 请求ID=%s 缺少 seed_id", requestID)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 seed_id 参数"})
		return
	}

	prepareSSE(c)
	connectionID := fmt.Sprintf("bdinfo-%d", time.Now().UnixNano())
	logx.Infof(bdinfoSSEHandlerLogModule, "SSE 连接建立 请求ID=%s seed_id=%s connection_id=%s", requestID, seedID, connectionID)
	if !writeSSEEvent(c, map[string]any{
		"type":          "connected",
		"connection_id": connectionID,
		"seed_id":       seedID,
	}) {
		logx.Warnf(bdinfoSSEHandlerLogModule, "SSE 首帧发送失败 请求ID=%s seed_id=%s connection_id=%s", requestID, seedID, connectionID)
		return
	}

	pollTicker := time.NewTicker(1200 * time.Millisecond)
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer pollTicker.Stop()
	defer heartbeatTicker.Stop()

	lastProgressSignature := ""
	lastTaskState := ""

	for {
		select {
		case <-c.Request.Context().Done():
			logx.Infof(bdinfoSSEHandlerLogModule, "SSE 连接关闭 请求ID=%s seed_id=%s connection_id=%s", requestID, seedID, connectionID)
			return
		case <-heartbeatTicker.C:
			if !writeSSEEvent(c, map[string]any{"type": "heartbeat", "timestamp": time.Now().Unix()}) {
				logx.Warnf(bdinfoSSEHandlerLogModule, "SSE 心跳发送失败 请求ID=%s seed_id=%s connection_id=%s", requestID, seedID, connectionID)
				return
			}
		case <-pollTicker.C:
			statusResp, statusCode := h.service.BDInfoStatus(seedID)
			if statusCode != http.StatusOK {
				errMessage := handlerToString(statusResp["error"], handlerToString(statusResp["message"], "获取BDInfo状态失败"))
				logx.Warnf(bdinfoSSEHandlerLogModule, "SSE 轮询失败 请求ID=%s seed_id=%s connection_id=%s 状态码=%d 错误=%s", requestID, seedID, connectionID, statusCode, errMessage)
				_ = writeSSEEvent(c, map[string]any{
					"type": "error",
					"data": map[string]any{"error": errMessage},
				})
				return
			}

			mediainfoStatus := strings.TrimSpace(handlerToString(statusResp["mediainfo_status"], ""))
			if mediainfoStatus == "completed" {
				logx.Infof(bdinfoSSEHandlerLogModule, "SSE 推送完成 请求ID=%s seed_id=%s connection_id=%s", requestID, seedID, connectionID)
				payload := map[string]any{"mediainfo": handlerToString(statusResp["mediainfo"], "")}
				if seedUpdates, ok := statusResp["seed_updates"].(map[string]any); ok && len(seedUpdates) > 0 {
					payload["seed_updates"] = seedUpdates
				}
				_ = writeSSEEvent(c, map[string]any{
					"type": "completion",
					"data": payload,
				})
				return
			}
			if mediainfoStatus == "failed" {
				logx.Warnf(bdinfoSSEHandlerLogModule, "SSE 推送失败 请求ID=%s seed_id=%s connection_id=%s 错误=%s", requestID, seedID, connectionID, handlerToString(statusResp["bdinfo_error"], "BDInfo 获取失败"))
				_ = writeSSEEvent(c, map[string]any{
					"type": "error",
					"data": map[string]any{"error": handlerToString(statusResp["bdinfo_error"], "BDInfo 获取失败")},
				})
				return
			}

			taskStatus, _ := statusResp["task_status"].(map[string]any)
			if len(taskStatus) == 0 {
				continue
			}

			taskState := strings.TrimSpace(handlerToString(taskStatus["status"], ""))
			if taskState != lastTaskState {
				lastTaskState = taskState
				logx.Infof(bdinfoSSEHandlerLogModule, "SSE 任务状态变更 请求ID=%s seed_id=%s connection_id=%s task_state=%s", requestID, seedID, connectionID, taskState)
			}
			switch taskState {
			case "processing_bdinfo":
				progressData := map[string]any{
					"progress_percent": handlerToFloat(taskStatus["progress_percent"]),
					"current_file":     handlerToString(taskStatus["current_file"], "处理中..."),
					"elapsed_time":     handlerToString(taskStatus["elapsed_time"], ""),
					"remaining_time":   handlerToString(taskStatus["remaining_time"], ""),
					"disc_size":        handlerToFloat(taskStatus["disc_size"]),
				}
				signature := fmt.Sprintf("%.2f|%s|%s|%s|%.0f", handlerToFloat(progressData["progress_percent"]), handlerToString(progressData["current_file"], ""), handlerToString(progressData["elapsed_time"], ""), handlerToString(progressData["remaining_time"], ""), handlerToFloat(progressData["disc_size"]))
				if signature != lastProgressSignature {
					lastProgressSignature = signature
					if !writeSSEEvent(c, map[string]any{"type": "progress_update", "data": progressData}) {
						logx.Warnf(bdinfoSSEHandlerLogModule, "SSE 进度发送失败 请求ID=%s seed_id=%s connection_id=%s", requestID, seedID, connectionID)
						return
					}
				}
			case "completed":
				logx.Infof(bdinfoSSEHandlerLogModule, "SSE 任务完成 请求ID=%s seed_id=%s connection_id=%s", requestID, seedID, connectionID)
				payload := map[string]any{"mediainfo": handlerToString(statusResp["mediainfo"], "")}
				if seedUpdates, ok := statusResp["seed_updates"].(map[string]any); ok && len(seedUpdates) > 0 {
					payload["seed_updates"] = seedUpdates
				}
				_ = writeSSEEvent(c, map[string]any{
					"type": "completion",
					"data": payload,
				})
				return
			case "failed":
				logx.Warnf(bdinfoSSEHandlerLogModule, "SSE 任务失败 请求ID=%s seed_id=%s connection_id=%s 错误=%s", requestID, seedID, connectionID, handlerToString(statusResp["bdinfo_error"], "BDInfo 获取失败"))
				_ = writeSSEEvent(c, map[string]any{
					"type": "error",
					"data": map[string]any{"error": handlerToString(statusResp["bdinfo_error"], "BDInfo 获取失败")},
				})
				return
			}
		}
	}
}

func (h *Handler) BDInfoProgress(c *gin.Context) {
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	result, status := h.service.BDInfoProgressCallback(payload)
	c.JSON(status, result)
}

func (h *Handler) BDInfoComplete(c *gin.Context) {
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	result, status := h.service.BDInfoCompleteCallback(payload)
	c.JSON(status, result)
}
func (h *Handler) BDInfoTasks(c *gin.Context) {
	result, status := h.service.BDInfoTasks()
	c.JSON(status, result)
}
