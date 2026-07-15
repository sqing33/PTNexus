package migrate

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/repository"
)

// PublishLogs 分页查询发种日志（供前端"发种日志"页面使用）。
// 参数/返回：通过 querystring 提供 page/page_size/search/status/trigger/scene/queue_group_id/target_site 等；返回列表数据与分页信息。
// 失败场景：日志仓储未初始化或查询失败时返回 5xx。
// 副作用：无（只读）。
func (h *Handler) PublishLogs(c *gin.Context) {
	page := parsePositiveInt(c.Query("page"), 1)
	pageSize := parsePositiveInt(c.Query("page_size"), 20)
	if pageSize > 200 {
		pageSize = 200
	}

	query := repository.PublishLogQuery{
		Page:         page,
		PageSize:     pageSize,
		Search:       strings.TrimSpace(c.Query("search")),
		Status:       strings.TrimSpace(c.Query("status")),
		Trigger:      strings.TrimSpace(c.Query("trigger")),
		Scene:        strings.TrimSpace(c.Query("scene")),
		QueueGroupID: strings.TrimSpace(c.Query("queue_group_id")),
		TargetSite:   strings.TrimSpace(c.Query("target_site")),
		SourceSite:   strings.TrimSpace(c.Query("source_site")),
		TorrentID:    strings.TrimSpace(c.Query("torrent_id")),
	}

	result, status := h.service.ListPublishLogs(query)
	if status == 0 {
		status = http.StatusOK
	}
	c.JSON(status, result)
}

// BatchDeletePublishLogs 批量删除发种日志（含关联队列任务取消）。
// 参数/返回：请求体为 {"ids": [1, 2, 3]}；返回删除结果与状态码。
// 失败场景：请求体解析失败或 ids 为空返回 400；服务层错误返回 5xx。
// 副作用：取消关联 queued 队列任务；删除 publish_logs 记录。
func (h *Handler) BatchDeletePublishLogs(c *gin.Context) {
	var body struct {
		IDs []uint64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供要删除的日志 ID"})
		return
	}

	result, status := h.service.BatchDeletePublishLogs(body.IDs)
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
