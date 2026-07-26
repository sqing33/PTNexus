package migrate

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/platform/logx"
)

const (
	mediaValidateHandlerLogModule  = "迁移接口-媒体校验"
	mediaRefreshHandlerLogModule   = "迁移接口-媒体信息刷新"
	downloaderInfoHandlerLogModule = "迁移接口-下载器信息"
	parseTitleHandlerLogModule     = "迁移接口-标题解析"
)

func (h *Handler) AddToDownloader(c *gin.Context) {
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	result, status := h.service.AddToDownloader(payload)
	c.JSON(status, result)
}

func (h *Handler) GetDownloaderInfo(c *gin.Context) {
	requestID := c.GetString("request_id")
	payload, ok := bindMapPayload(c)
	if !ok {
		logx.Warnf(downloaderInfoHandlerLogModule, "请求体绑定失败 请求ID=%s", requestID)
		return
	}
	seedID := strings.TrimSpace(handlerToString(payload["seed_id"], ""))
	logx.Infof(downloaderInfoHandlerLogModule, "收到请求 请求ID=%s seed_id=%s", requestID, seedID)
	result, status := h.service.GetDownloaderInfo(payload)
	if status >= 400 {
		logx.Warnf(downloaderInfoHandlerLogModule, "请求结束 请求ID=%s seed_id=%s 状态码=%d", requestID, seedID, status)
	} else {
		logx.Infof(downloaderInfoHandlerLogModule, "请求结束 请求ID=%s seed_id=%s 状态码=%d", requestID, seedID, status)
	}
	c.JSON(status, result)
}

func (h *Handler) ParseTitle(c *gin.Context) {
	requestID := c.GetString("request_id")
	payload, ok := bindMapPayload(c)
	if !ok {
		logx.Warnf(parseTitleHandlerLogModule, "请求体绑定失败 请求ID=%s", requestID)
		return
	}
	title := handlerToString(payload["title"], "")
	mediainfo := handlerToString(payload["mediainfo"], "")
	logx.Infof(parseTitleHandlerLogModule, "收到请求 请求ID=%s title_len=%d mediainfo_len=%d", requestID, len(title), len(mediainfo))
	result, status := h.service.ParseTitle(title, mediainfo, requestID)
	if status >= 400 {
		logx.Warnf(parseTitleHandlerLogModule, "请求结束 请求ID=%s 状态码=%d", requestID, status)
	} else {
		logx.Infof(parseTitleHandlerLogModule, "请求结束 请求ID=%s 状态码=%d", requestID, status)
	}
	c.JSON(status, result)
}

func (h *Handler) MediaValidate(c *gin.Context) {
	requestID := c.GetString("request_id")
	payload, ok := bindMapPayload(c)
	if !ok {
		logx.Warnf(mediaValidateHandlerLogModule, "请求体绑定失败 请求ID=%s", requestID)
		return
	}
	// 统一把 request_id 透传到业务层，便于串联后续日志。
	if strings.TrimSpace(handlerToString(payload["request_id"], "")) == "" {
		payload["request_id"] = requestID
	}
	mediaType := strings.TrimSpace(handlerToString(payload["type"], ""))
	contentName := strings.TrimSpace(handlerToString(payload["content_name"], ""))
	logx.Infof(mediaValidateHandlerLogModule, "收到请求 请求ID=%s type=%s content_name=%s", requestID, mediaType, contentName)
	result, status := h.service.MediaValidate(payload)
	if status >= 400 {
		logx.Warnf(mediaValidateHandlerLogModule, "请求结束 请求ID=%s type=%s content_name=%s 状态码=%d", requestID, mediaType, contentName, status)
	} else {
		logx.Infof(mediaValidateHandlerLogModule, "请求结束 请求ID=%s type=%s content_name=%s 状态码=%d", requestID, mediaType, contentName, status)
	}
	c.JSON(status, result)
}

func (h *Handler) RefreshMediainfoAsync(c *gin.Context) {
	requestID := c.GetString("request_id")
	payload, ok := bindMapPayload(c)
	if !ok {
		logx.Warnf(mediaRefreshHandlerLogModule, "请求体绑定失败 请求ID=%s", requestID)
		return
	}
	seedID := strings.TrimSpace(handlerToString(payload["seed_id"], ""))
	savePath := strings.TrimSpace(handlerToString(payload["save_path"], handlerToString(payload["savePath"], "")))
	logx.Infof(mediaRefreshHandlerLogModule, "收到请求 请求ID=%s seed_id=%s save_path=%s", requestID, seedID, savePath)
	result, status := h.service.RefreshMediainfoAsync(payload)
	if status >= 400 {
		logx.Warnf(mediaRefreshHandlerLogModule, "请求结束 请求ID=%s seed_id=%s 状态码=%d", requestID, seedID, status)
	} else {
		logx.Infof(mediaRefreshHandlerLogModule, "请求结束 请求ID=%s seed_id=%s 状态码=%d", requestID, seedID, status)
	}
	c.JSON(status, result)
}
func (h *Handler) DownloadTorrentOnly(c *gin.Context) {
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	result, status := h.service.DownloadTorrentOnly(payload)
	c.JSON(status, result)
}

func (h *Handler) MigrateTorrent(c *gin.Context) {
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	result, status := h.service.MigrateTorrent(payload)
	c.JSON(status, result)
}

func (h *Handler) UpdatePreviewData(c *gin.Context) {
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	result, status := h.service.UpdatePreviewData(payload)
	c.JSON(status, result)
}

func (h *Handler) GetAggregatedTorrents(c *gin.Context) {
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	result, status := h.service.GetAggregatedTorrents(payload)
	c.JSON(status, result)
}
