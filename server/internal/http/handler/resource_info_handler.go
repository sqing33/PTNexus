package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/repository"
)

// ResourceInfoHandler 提供资源信息库的查询接口。
type ResourceInfoHandler struct {
	repo *repository.MigrateRepository
}

// NewResourceInfoHandler 创建资源信息 handler。
// 参数/返回：repo 为迁移仓储（含资源信息读写能力）。
func NewResourceInfoHandler(repo *repository.MigrateRepository) *ResourceInfoHandler {
	return &ResourceInfoHandler{repo: repo}
}

// resourceInfoUpdateRequest 定义资源信息编辑请求的可写字段。
type resourceInfoUpdateRequest struct {
	Title       string `json:"title"`
	Year        string `json:"year"`
	Country     string `json:"country"`
	DoubanID    string `json:"douban_id"`
	ImdbID      string `json:"imdb_id"`
	TmdbID      string `json:"tmdb_id"`
	PosterURL   string `json:"poster_url"`
	Summary     string `json:"summary"`
	Screenshots string `json:"screenshots"`
}

// Update 按主键 ID 更新资源信息的可编辑字段。
// 参数/返回：路径参数 id 为目标记录主键；请求体为可写字段；成功返回 success=true。
// 失败场景：ID 无效、参数解析失败或仓储写入失败时返回对应错误码。
// 副作用：更新 resource_info 表对应记录。
func (h *ResourceInfoHandler) Update(c *gin.Context) {
	if h.repo == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "仓储未初始化"})
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "无效的资源 ID"})
		return
	}
	var req resourceInfoUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	info := &repository.ResourceInfo{
		ID:          id,
		Title:       req.Title,
		Year:        req.Year,
		Country:     req.Country,
		DoubanID:    req.DoubanID,
		ImdbID:      req.ImdbID,
		TmdbID:      req.TmdbID,
		PosterURL:   req.PosterURL,
		Summary:     req.Summary,
		Screenshots: req.Screenshots,
	}
	if err := h.repo.UpdateResourceInfo(info); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// List 分页返回资源信息列表，支持 keyword 模糊搜索（标题/国家/三个 ID）。
// 参数/返回：查询参数 keyword/page/page_size；返回 items 与 total。
// 失败场景：仓储查询失败时返回 500。
// 副作用：仅读取 resource_info 表。
func (h *ResourceInfoHandler) List(c *gin.Context) {
	if h.repo == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "仓储未初始化"})
		return
	}
	page, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("page", "1")))
	pageSize, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("page_size", "20")))
	keyword := strings.TrimSpace(c.Query("keyword"))

	items, total, err := h.repo.ListResourceInfos(keyword, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	if items == nil {
		items = []repository.ResourceInfo{}
	}
	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}
