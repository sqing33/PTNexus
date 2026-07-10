package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service"
)

type CrossSeedHandler struct {
	service *service.CrossSeedService
}

func NewCrossSeedHandler(svc *service.CrossSeedService) *CrossSeedHandler {
	return &CrossSeedHandler{service: svc}
}

func (h *CrossSeedHandler) UniquePaths(c *gin.Context) {
	result, err := h.service.UniquePaths(boolQuery(c, "video_only"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *CrossSeedHandler) Data(c *gin.Context) {
	includeUniquePaths := true
	if raw := strings.TrimSpace(c.Query("include_unique_paths")); raw != "" {
		includeUniquePaths = boolQuery(c, "include_unique_paths")
	}

	params := service.CrossSeedQueryParams{
		Page:               intQuery(c, "page", 1),
		PageSize:           intQuery(c, "page_size", 20),
		Search:             c.Query("search"),
		PathFilters:        stringSliceQuery(c, "path_filters"),
		IsDeleted:          c.Query("is_deleted"),
		ExcludeTargetSites: c.Query("exclude_target_sites"),
		ReviewStatus:       c.Query("review_status"),
		IncludeUniquePaths: includeUniquePaths,
		VideoOnly:          boolQuery(c, "video_only"),
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
