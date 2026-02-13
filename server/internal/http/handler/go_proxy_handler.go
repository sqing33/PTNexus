package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server-go/internal/service"
)

type GoProxyHandler struct {
	service *service.GoProxyService
}

func NewGoProxyHandler(svc *service.GoProxyService) *GoProxyHandler {
	return &GoProxyHandler{service: svc}
}

func (h *GoProxyHandler) BatchEnhance(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求体格式错误"})
		return
	}
	result, status := h.service.BatchEnhance(payload)
	c.JSON(status, result)
}

func (h *GoProxyHandler) StopBatchEnhance(c *gin.Context) {
	result, status := h.service.StopBatchEnhance()
	c.JSON(status, result)
}

func (h *GoProxyHandler) Records(c *gin.Context) {
	params := service.BatchRecordQueryParams{
		Page:      intQuery(c, "page", 1),
		PageSize:  intQuery(c, "page_size", 200),
		Status:    c.Query("status"),
		BatchID:   c.Query("batch_id"),
		Search:    c.Query("search"),
		StartTime: c.Query("start_time"),
		EndTime:   c.Query("end_time"),
	}
	result, status := h.service.QueryRecords(params)
	c.JSON(status, result)
}

func (h *GoProxyHandler) ClearRecords(c *gin.Context) {
	result, status := h.service.ClearRecords()
	c.JSON(status, result)
}
