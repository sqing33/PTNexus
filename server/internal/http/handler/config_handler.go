package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service"
)

type ConfigHandler struct {
	settings *service.SettingsService
}

func NewConfigHandler(settings *service.SettingsService) *ConfigHandler {
	return &ConfigHandler{settings: settings}
}

func (h *ConfigHandler) GetSourcePriority(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": h.settings.GetSourcePriority()})
}

func (h *ConfigHandler) SaveSourcePriority(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	rawPriority := []any{}
	if raw, exists := payload["source_priority"]; exists {
		parsed, ok := raw.([]any)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "源站点优先级必须是数组"})
			return
		}
		rawPriority = parsed
	}
	if err := h.settings.SaveSourcePriority(rawPriority); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "源站点优先级配置已保存"})
}

func (h *ConfigHandler) GetBatchFetchFilters(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": h.settings.GetBatchFetchFilters()})
}

func (h *ConfigHandler) SaveBatchFetchFilters(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	filters, ok := payload["batch_fetch_filters"].(map[string]any)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "批量获取筛选条件必须是对象"})
		return
	}
	if err := h.settings.SaveBatchFetchFilters(filters); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "批量获取筛选条件配置已保存"})
}

func (h *ConfigHandler) GetCrossSeedReviewFilter(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": h.settings.GetCrossSeedReviewFilter()})
}

func (h *ConfigHandler) SaveCrossSeedReviewFilter(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	filter := ""
	if raw, exists := payload["review_filter"]; exists {
		parsed, ok := raw.(string)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "检查状态筛选必须是字符串"})
			return
		}
		filter = parsed
	}
	if err := h.settings.SaveCrossSeedReviewFilter(filter); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "转种数据检查状态筛选配置已保存"})
}

func (h *ConfigHandler) GetTags(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": h.settings.GetTagsConfig()})
}

func (h *ConfigHandler) SaveTags(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	if err := h.settings.SaveTagsConfig(payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "标签配置已保存"})
}
