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
		"success": true,
		"items":   items,
		"total":   total,
		"page":    page,
		"page_size": pageSize,
	})
}
