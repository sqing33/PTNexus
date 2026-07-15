package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/repository"
)

type SitesHandler struct {
	repo *repository.SiteRepository
}

var specialPublisherSiteCodes = map[string]struct{}{
	"zhuque": {},
	"rousi":  {},
	"ptlgs":  {},
}

func NewSitesHandler(repo *repository.SiteRepository) *SitesHandler {
	return &SitesHandler{repo: repo}
}

func (h *SitesHandler) SitesList(c *gin.Context) {
	sourceSites, targetSites, err := h.repo.ListSourceAndTargetSites()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取站点列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"source_sites": sourceSites, "target_sites": targetSites})
}

func (h *SitesHandler) Sites(c *gin.Context) {
	filterBy := c.DefaultQuery("filter_by_torrents", "all")
	sites, err := h.repo.ListSites(filterBy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取站点列表失败"})
		return
	}
	c.JSON(http.StatusOK, sites)
}

func (h *SitesHandler) UpdateSite(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "必须提供站点ID。"})
		return
	}
	rawID, exists := payload["id"]
	if !exists || rawID == nil || isFalsyID(rawID) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "必须提供站点ID。"})
		return
	}
	updated, err := h.repo.UpdateSiteDetails(payload)
	if err != nil || !updated {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": fmt.Sprintf("未找到站点ID '%v' 或更新失败。", rawID)})
		return
	}
	nickname := pythonLikeString(payload["nickname"])
	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("站点 '%s' 的信息已成功更新。", nickname)})
}

func (h *SitesHandler) DeleteSite(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "必须提供站点ID。"})
		return
	}
	rawID, exists := payload["id"]
	if !exists || rawID == nil || isFalsyID(rawID) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "必须提供站点ID。"})
		return
	}
	siteID, err := parseInt64(rawID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": fmt.Sprintf("删除站点ID '%v' 失败。", rawID)})
		return
	}
	if siteID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "必须提供站点ID。"})
		return
	}
	deleted, err := h.repo.DeleteSite(siteID)
	if err == nil && deleted {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "站点已成功删除。"})
		return
	}
	c.JSON(http.StatusNotFound, gin.H{"success": false, "message": fmt.Sprintf("删除站点ID '%v' 失败。", rawID)})
}

func (h *SitesHandler) UpdateSiteCookie(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "必须提供站点昵称和 Cookie。"})
		return
	}
	nickname := strings.TrimSpace(toString(payload["nickname"], ""))
	rawCookie, exists := payload["cookie"]
	if !exists || rawCookie == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "必须提供站点昵称和 Cookie。"})
		return
	}
	cookie, ok := rawCookie.(string)
	if !ok {
		// Baseline expects a string; keep behavior simple here.
		cookie = fmt.Sprintf("%v", rawCookie)
	}
	cookie = strings.TrimSpace(cookie)
	if nickname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "必须提供站点昵称和 Cookie。"})
		return
	}
	updated, err := h.repo.UpdateSiteCookie(nickname, cookie)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "服务器内部错误。"})
		return
	}
	if !updated {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": fmt.Sprintf("未找到站点 '%s' 或更新失败。", nickname)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("站点 '%s' 的 Cookie 已成功更新。", nickname)})
}

func (h *SitesHandler) SitesStatus(c *gin.Context) {
	status, err := h.repo.SitesStatus()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取站点状态失败"})
		return
	}
	for _, siteStatus := range status {
		siteCode := strings.TrimSpace(toString(siteStatus["site"], ""))
		siteStatus["uses_public_publisher"] = h.usesPublicPublisher(siteCode)
	}
	c.JSON(http.StatusOK, status)
}

func (h *SitesHandler) usesPublicPublisher(siteCode string) bool {
	normalizedCode := strings.ToLower(strings.TrimSpace(siteCode))
	if normalizedCode == "" {
		return false
	}
	_, isSpecialPublisher := specialPublisherSiteCodes[normalizedCode]
	return !isSpecialPublisher
}

func (h *SitesHandler) UpdateSitesOrder(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少必要参数"})
		return
	}
	rawIDs, ok := payload["ids"].([]any)
	if !ok || len(rawIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "必须提供站点ID列表"})
		return
	}
	siteIDs := make([]int64, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id, err := parseInt64(raw)
		if err != nil || id <= 0 {
			continue
		}
		siteIDs = append(siteIDs, id)
	}
	if len(siteIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未找到有效的站点ID"})
		return
	}
	updated, err := h.repo.UpdateSitesSortOrder(siteIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新排序失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("已更新 %d 个站点的排序", updated)})
}

func (h *SitesHandler) SetNotExist(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少必要参数"})
		return
	}
	torrentName := strings.TrimSpace(toString(payload["torrent_name"], ""))
	siteName := strings.TrimSpace(toString(payload["site_name"], ""))
	if torrentName == "" || siteName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少必要参数"})
		return
	}
	updated, err := h.repo.SetTorrentSiteNotExist(torrentName, siteName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "设置站点状态失败"})
		return
	}
	if !updated {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到匹配的记录"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "站点状态已成功设置为不存在"})
}

func (h *SitesHandler) UpdateComment(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少必要参数"})
		return
	}
	torrentName := strings.TrimSpace(toString(payload["torrent_name"], ""))
	siteName := strings.TrimSpace(toString(payload["site_name"], ""))
	comment := strings.TrimSpace(toString(payload["comment"], ""))
	if torrentName == "" || siteName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少必要参数"})
		return
	}
	if comment == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "详情页链接或ID不能为空"})
		return
	}
	updated, err := h.repo.UpdateTorrentComment(torrentName, siteName, comment)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新详情页链接失败"})
		return
	}
	if !updated {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到匹配的记录"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "详情页链接已成功更新", "success": true})
}

func parseInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), nil
	case float32:
		return int64(typed), nil
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
	default:
		return 0, strconv.ErrSyntax
	}
}

func isFalsyID(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case bool:
		return !typed
	case float64:
		return typed == 0
	case float32:
		return typed == 0
	case int:
		return typed == 0
	case int64:
		return typed == 0
	case string:
		return strings.TrimSpace(typed) == ""
	default:
		return false
	}
}

func pythonLikeString(value any) string {
	if value == nil {
		return "None"
	}
	if typed, ok := value.(string); ok {
		return typed
	}
	return fmt.Sprintf("%v", value)
}

func toString(value any, fallback string) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return fallback
}
