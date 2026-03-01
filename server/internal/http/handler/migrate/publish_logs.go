package migrate

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server-go/internal/repository"
)

// PublishLogs 分页查询发种日志（供前端“发种日志”页面使用）。
// 参数/返回：通过 querystring 提供 page/page_size/search/status/trigger/scene/target_site 等；返回列表数据与分页信息。
// 失败场景：日志仓储未初始化或查询失败时返回 5xx。
// 副作用：无（只读）。
func (h *Handler) PublishLogs(c *gin.Context) {
	page := parsePositiveInt(c.Query("page"), 1)
	pageSize := parsePositiveInt(c.Query("page_size"), 20)
	if pageSize > 200 {
		pageSize = 200
	}

	query := repository.PublishLogQuery{
		Page:       page,
		PageSize:   pageSize,
		Search:     strings.TrimSpace(c.Query("search")),
		Status:     strings.TrimSpace(c.Query("status")),
		Trigger:    strings.TrimSpace(c.Query("trigger")),
		Scene:      strings.TrimSpace(c.Query("scene")),
		TargetSite: strings.TrimSpace(c.Query("target_site")),
		SourceSite: strings.TrimSpace(c.Query("source_site")),
		TorrentID:  strings.TrimSpace(c.Query("torrent_id")),
	}

	result, status := h.service.ListPublishLogs(query)
	if status == 0 {
		status = http.StatusOK
	}
	c.JSON(status, result)
}

func parsePositiveInt(raw string, fallback int) int {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
