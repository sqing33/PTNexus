package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server-go/internal/service"
)

type CrossSeedHandler struct {
	service *service.CrossSeedService
}

func NewCrossSeedHandler(svc *service.CrossSeedService) *CrossSeedHandler {
	return &CrossSeedHandler{service: svc}
}

func (h *CrossSeedHandler) UniquePaths(c *gin.Context) {
	result, err := h.service.UniquePaths()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *CrossSeedHandler) Data(c *gin.Context) {
	params := service.CrossSeedQueryParams{
		Page:               intQuery(c, "page", 1),
		PageSize:           intQuery(c, "page_size", 20),
		Search:             c.Query("search"),
		PathFilters:        stringSliceQuery(c, "path_filters"),
		IsDeleted:          c.Query("is_deleted"),
		ExcludeTargetSites: c.Query("exclude_target_sites"),
		ReviewStatus:       c.Query("review_status"),
	}
	result, err := h.service.QueryData(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *CrossSeedHandler) TestNoAuth(c *gin.Context) {
	// Python baseline uses str(time.time()).
	seconds := float64(time.Now().UnixNano()) / 1e9
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "无认证测试成功", "timestamp": strconv.FormatFloat(seconds, 'f', -1, 64)})
}

func (h *CrossSeedHandler) DeleteData(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求体格式错误"})
		return
	}
	result, status := h.service.DeleteCrossSeedData(payload)
	c.JSON(status, result)
}

func (h *CrossSeedHandler) AddBatchRecord(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求体格式错误"})
		return
	}
	result, status := h.service.AddBatchRecord(payload)
	c.JSON(status, result)
}

func (h *CrossSeedHandler) GetBatchRecords(c *gin.Context) {
	params := service.BatchRecordQueryParams{
		Page:      intQuery(c, "page", 1),
		PageSize:  intQuery(c, "page_size", 50),
		Status:    c.Query("status"),
		BatchID:   c.Query("batch_id"),
		Search:    c.Query("search"),
		StartTime: c.Query("start_time"),
		EndTime:   c.Query("end_time"),
	}
	result, err := h.service.QueryBatchRecords(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *CrossSeedHandler) ClearBatchRecords(c *gin.Context) {
	result, status := h.service.ClearBatchRecords("")
	c.JSON(status, result)
}

func (h *CrossSeedHandler) ClearBatchRecordsByID(c *gin.Context) {
	batchID := c.Param("batch_id")
	result, status := h.service.ClearBatchRecords(batchID)
	c.JSON(status, result)
}
