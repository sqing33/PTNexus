package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/autoseed"
)

// AutoSeedHandler 处理自动发种规则、RSS 列表、整理、推送、发布和进度查询接口。
type AutoSeedHandler struct {
	service *autoseed.Service
}

// NewAutoSeedHandler 创建自动发种 HTTP 处理器。
func NewAutoSeedHandler(service *autoseed.Service) *AutoSeedHandler {
	return &AutoSeedHandler{service: service}
}

// ListRules 返回全部自动发种规则。
func (h *AutoSeedHandler) ListRules(c *gin.Context) {
	rows, err := h.service.ListRules()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

// SaveRule 创建或更新自动发种规则。
func (h *AutoSeedHandler) SaveRule(c *gin.Context) {
	var rule repository.AutoSeedRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求参数无效: " + err.Error()})
		return
	}
	if strings.TrimSpace(rule.Name) == "" {
		rule.Name = strings.TrimSpace(rule.SourceSite)
	}
	if strings.TrimSpace(rule.Name) == "" || strings.TrimSpace(rule.RSSURL) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "规则名称和 RSS 地址不能为空"})
		return
	}
	if idText := strings.TrimSpace(c.Param("id")); idText != "" {
		if id, err := strconv.ParseInt(idText, 10, 64); err == nil && id > 0 {
			rule.ID = id
		}
	}
	if err := h.service.SaveRule(&rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "规则已保存", "data": rule})
}

// DeleteRule 删除自动发种规则。
func (h *AutoSeedHandler) DeleteRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的规则 ID"})
		return
	}
	if err := h.service.DeleteRule(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "规则已删除"})
}

// TriggerRule 手动触发指定规则立即拉取 RSS。
func (h *AutoSeedHandler) TriggerRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的规则 ID"})
		return
	}
	h.service.TriggerRule(id)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已触发 RSS 拉取"})
}

// ListItems 分页查询自动发种列表。
func (h *AutoSeedHandler) ListItems(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	rows, total, err := h.service.ListItems(repository.AutoSeedListQuery{
		Page:         page,
		PageSize:     pageSize,
		SourceSite:   strings.TrimSpace(c.Query("source_site")),
		Status:       strings.TrimSpace(c.Query("status")),
		ResourceType: strings.TrimSpace(c.Query("resource_type")),
		DownloaderID: strings.TrimSpace(c.Query("downloader_id")),
		Search:       strings.TrimSpace(c.Query("search")),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows, "total": total})
}

// AddManualURL 将指定种子地址添加到自动发种列表并推送下载器。
func (h *AutoSeedHandler) AddManualURL(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求参数无效: " + err.Error()})
		return
	}
	if err := h.service.AddManualURL(
		autoSeedString(payload["torrent_url"]),
		autoSeedString(payload["downloader_id"]),
		autoSeedString(payload["source_site"]),
	); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "种子已加入自动发种列表"})
}

// OrganizeItem 保存自动发种记录的整理结果。
func (h *AutoSeedHandler) OrganizeItem(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的种子 ID"})
		return
	}
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求参数无效: " + err.Error()})
		return
	}
	if err := h.service.OrganizeItem(id, payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "整理信息已保存"})
}

// PublishItems 将一个或多个自动发种记录投递到发布队列。
func (h *AutoSeedHandler) PublishItems(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求参数无效: " + err.Error()})
		return
	}
	ids := autoSeedInt64Slice(payload["ids"])
	if len(ids) == 0 {
		if id := autoSeedInt64(payload["id"]); id > 0 {
			ids = []int64{id}
		}
	}
	targetSites := autoSeedStringSlice(payload["target_sites"])
	result, err := h.service.PublishItems(ids, targetSites)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// PushItems 灏嗕竴涓垨澶氫釜鏈帹閫佺殑鑷姩鍙戠璁板綍鎶撳彇璇︽儏骞舵帹閫佸埌涓嬭浇鍣ㄣ€?
func (h *AutoSeedHandler) PushItems(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "璇锋眰鍙傛暟鏃犳晥: " + err.Error()})
		return
	}
	ids := autoSeedInt64Slice(payload["ids"])
	if len(ids) == 0 {
		if id := autoSeedInt64(payload["id"]); id > 0 {
			ids = []int64{id}
		}
	}
	result, err := h.service.PushItems(ids)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error(), "data": result})
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteItems 删除自动发种记录，并按需删除下载器任务和文件。
func (h *AutoSeedHandler) DeleteItems(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求参数无效: " + err.Error()})
		return
	}
	ids := autoSeedInt64Slice(payload["ids"])
	deleted, err := h.service.DeleteItems(ids, autoSeedBool(payload["delete_files"], true))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "记录已删除", "deleted": deleted})
}

// Progress 查询下载器自动发种进度列表。
func (h *AutoSeedHandler) Progress(c *gin.Context) {
	rows, err := h.service.Progress(strings.TrimSpace(c.Query("downloader_id")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

func autoSeedString(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func autoSeedBool(value any, fallback bool) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}
	return fallback
}

func autoSeedInt64(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	}
	return 0
}

func autoSeedInt64Slice(value any) []int64 {
	raw, ok := value.([]any)
	if !ok {
		return []int64{}
	}
	result := make([]int64, 0, len(raw))
	for _, item := range raw {
		if parsed := autoSeedInt64(item); parsed > 0 {
			result = append(result, parsed)
		}
	}
	return result
}

func autoSeedStringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return []string{}
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if text := autoSeedString(item); text != "" {
			result = append(result, text)
		}
	}
	return result
}
