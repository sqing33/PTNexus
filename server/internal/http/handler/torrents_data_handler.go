package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service"
)

type TorrentDataHandler struct {
	service *service.TorrentDataService
}

func NewTorrentDataHandler(svc *service.TorrentDataService) *TorrentDataHandler {
	return &TorrentDataHandler{service: svc}
}

func (h *TorrentDataHandler) Data(c *gin.Context) {
	params := service.TorrentsDataParams{
		Page:                      intQuery(c, "page", 1),
		PageSize:                  intQuery(c, "pageSize", 50),
		PathFilters:               stringSliceQuery(c, "path_filters"),
		StateFilters:              stringSliceQuery(c, "state_filters"),
		DownloaderFilters:         stringSliceQuery(c, "downloader_filters"),
		SourceAvailabilityFilters: stringSliceQuery(c, "source_availability_filters"),
		ExistSiteNames:            stringSliceQuery(c, "existSiteNames"),
		NotExistSiteNames:         stringSliceQuery(c, "notExistSiteNames"),
		NameSearch:                c.Query("nameSearch"),
		SortProp:                  c.Query("sortProp"),
		SortOrder:                 c.Query("sortOrder"),
		ExcludeExisting:           boolQuery(c, "exclude_existing"),
		OnlyCompleted:             boolQuery(c, "only_completed"),
	}
	result, err := h.service.GetData(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "从数据库检索种子数据失败", "success": false})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TorrentDataHandler) RefreshData(c *gin.Context) {
	c.JSON(http.StatusOK, h.service.RefreshData())
}

func (h *TorrentDataHandler) CachedSites(c *gin.Context) {
	name := c.Query("name")
	size := int64Query(c, "size", 0)
	result, status := h.service.GetCachedSites(name, size)
	c.JSON(status, result)
}

func (h *TorrentDataHandler) UpdatePublishAt(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求体格式错误"})
		return
	}
	result, status := h.service.UpdatePublishAt(payload)
	c.JSON(status, result)
}

func (h *TorrentDataHandler) IYUUQuery(c *gin.Context) {
	payload := map[string]any{}
	_ = c.ShouldBindJSON(&payload)
	result, status := h.service.QueryIYUU(payload)
	c.JSON(status, result)
}

func (h *TorrentDataHandler) IYUUQueryBatch(c *gin.Context) {
	payload := struct {
		Torrents   []map[string]any `json:"torrents"`
		MaxGroups  int              `json:"max_groups"`
		ForceQuery *bool            `json:"force_query"`
	}{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	forceQuery := true
	if payload.ForceQuery != nil {
		forceQuery = *payload.ForceQuery
	}
	taskID, err := h.service.StartIYUUQueryBatch(payload.Torrents, payload.MaxGroups, forceQuery)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "task_id": taskID})
}

func (h *TorrentDataHandler) IYUUQueryBatchProgress(c *gin.Context) {
	taskID := strings.TrimSpace(c.Query("task_id"))
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 task_id 参数"})
		return
	}
	task, ok := h.service.GetIYUUQueryBatchTask(taskID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任务不存在或已过期"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "task": task})
}

func stringSliceQuery(c *gin.Context, key string) []string {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return []string{}
	}
	values := []string{}
	if err := json.Unmarshal([]byte(raw), &values); err == nil {
		clean := make([]string, 0, len(values))
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				clean = append(clean, trimmed)
			}
		}
		return clean
	}
	if strings.Contains(raw, ",") {
		parts := strings.Split(raw, ",")
		clean := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				clean = append(clean, trimmed)
			}
		}
		return clean
	}
	return []string{raw}
}

func intQuery(c *gin.Context, key string, fallback int) int {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func int64Query(c *gin.Context, key string, fallback int64) int64 {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func boolQuery(c *gin.Context, key string) bool {
	value := strings.ToLower(strings.TrimSpace(c.Query(key)))
	return value == "true" || value == "1" || value == "yes"
}
