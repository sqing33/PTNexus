package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service"
)

type TorrentTransferHandler struct {
	service *service.TorrentTransferService
}

func NewTorrentTransferHandler(svc *service.TorrentTransferService) *TorrentTransferHandler {
	return &TorrentTransferHandler{service: svc}
}

func (h *TorrentTransferHandler) Prepare(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	result, status := h.service.Prepare(payload)
	c.JSON(status, result)
}

func (h *TorrentTransferHandler) Execute(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	result, status := h.service.Execute(payload)
	c.JSON(status, result)
}

func (h *TorrentTransferHandler) Similar(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	result, status := h.service.Similar(payload)
	c.JSON(status, result)
}

func (h *TorrentTransferHandler) Pause(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	result, status := h.service.Pause(payload)
	c.JSON(status, result)
}

func (h *TorrentTransferHandler) Export(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	result, status := h.service.Export(payload)
	c.JSON(status, result)
}

func (h *TorrentTransferHandler) Add(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	result, status := h.service.Add(payload)
	c.JSON(status, result)
}
