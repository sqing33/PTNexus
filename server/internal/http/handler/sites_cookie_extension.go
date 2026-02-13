package handler

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server-go/internal/http/middleware"
	"github.com/pt-nexus/server-go/internal/platform/logx"
)

const siteCookieSyncLogModule = "浏览器插件-Cookie同步"

type cookieSyncBatchRequest struct {
	Items     []cookieSyncBatchItem `json:"items"`
	SkipEmpty *bool                 `json:"skip_empty"`
}

type cookieSyncBatchItem struct {
	Site     string `json:"site"`
	Nickname string `json:"nickname"`
	Cookie   string `json:"cookie"`
}

// CookieSyncTargets 返回浏览器插件可同步的站点列表。
// 参数/返回：无请求参数；返回 site/nickname/base_url/domains 等同步所需字段。
// 失败场景：读取数据库站点列表失败时返回 500。
// 副作用：仅记录流程日志，不修改数据库。
func (h *SitesHandler) CookieSyncTargets(c *gin.Context) {
	requestID := middleware.GetRequestID(c)
	userID := middleware.GetUserID(c)
	logx.Infof(siteCookieSyncLogModule, "开始获取同步站点 request_id=%s user=%s", requestID, userID)

	sites, err := h.repo.ListSites("all")
	if err != nil {
		logx.Errorf(siteCookieSyncLogModule, "读取站点列表失败 request_id=%s user=%s err=%v", requestID, userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取站点列表失败"})
		return
	}

	result := make([]map[string]any, 0, len(sites))
	for _, site := range sites {
		siteCode := strings.TrimSpace(toString(site["site"], ""))
		nickname := strings.TrimSpace(toString(site["nickname"], ""))
		if siteCode == "" || nickname == "" {
			continue
		}
		baseURL := strings.TrimSpace(toString(site["base_url"], ""))
		specialTrackerDomain := strings.TrimSpace(toString(site["special_tracker_domain"], ""))
		domains := buildCookieSyncDomains(baseURL, specialTrackerDomain)

		result = append(result, map[string]any{
			"site":                   siteCode,
			"nickname":               nickname,
			"base_url":               baseURL,
			"special_tracker_domain": specialTrackerDomain,
			"domains":                domains,
			"cookie_required":        !strings.EqualFold(siteCode, "rousi"),
			"has_cookie":             hasCookieConfigured(site["has_cookie"]),
		})
	}

	logx.Infof(siteCookieSyncLogModule, "同步站点查询完成 request_id=%s user=%s total=%d", requestID, userID, len(result))
	c.JSON(http.StatusOK, gin.H{"success": true, "total": len(result), "sites": result})
}

// BatchUpdateSiteCookies 批量更新站点 Cookie，供浏览器插件一次性提交同步结果。
// 参数/返回：接收 items 列表（site/nickname/cookie），返回更新/跳过/失败统计与逐项结果。
// 失败场景：请求体非法或数据库查询失败时返回 4xx/5xx。
// 副作用：写入 sites 表 cookie 字段，并记录流程日志。
func (h *SitesHandler) BatchUpdateSiteCookies(c *gin.Context) {
	payload := cookieSyncBatchRequest{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	if len(payload.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "items 不能为空"})
		return
	}

	skipEmpty := true
	if payload.SkipEmpty != nil {
		skipEmpty = *payload.SkipEmpty
	}

	requestID := middleware.GetRequestID(c)
	userID := middleware.GetUserID(c)
	logx.Infof(siteCookieSyncLogModule, "开始批量更新Cookie request_id=%s user=%s items=%d skip_empty=%t", requestID, userID, len(payload.Items), skipEmpty)

	sites, err := h.repo.ListSites("all")
	if err != nil {
		logx.Errorf(siteCookieSyncLogModule, "读取站点列表失败 request_id=%s user=%s err=%v", requestID, userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取站点列表失败"})
		return
	}
	siteByCode, siteByNickname := buildCookieSyncSiteIndexes(sites)

	updatedCount := 0
	skippedCount := 0
	failedCount := 0
	results := make([]map[string]any, 0, len(payload.Items))

	for _, item := range payload.Items {
		siteCode := strings.TrimSpace(item.Site)
		nickname := strings.TrimSpace(item.Nickname)
		cookie := strings.TrimSpace(item.Cookie)

		target := map[string]any(nil)
		if siteCode != "" {
			target = siteByCode[strings.ToLower(siteCode)]
		}
		if target == nil && nickname != "" {
			target = siteByNickname[strings.ToLower(nickname)]
		}

		if target == nil {
			failedCount++
			results = append(results, map[string]any{
				"site":     siteCode,
				"nickname": nickname,
				"status":   "failed",
				"message":  "站点不存在",
			})
			continue
		}

		resolvedSite := strings.TrimSpace(toString(target["site"], siteCode))
		resolvedNickname := strings.TrimSpace(toString(target["nickname"], nickname))
		if resolvedSite == "" && resolvedNickname == "" {
			failedCount++
			results = append(results, map[string]any{
				"site":     siteCode,
				"nickname": nickname,
				"status":   "failed",
				"message":  "站点标识无效",
			})
			continue
		}

		if cookie == "" && skipEmpty {
			skippedCount++
			results = append(results, map[string]any{
				"site":     resolvedSite,
				"nickname": resolvedNickname,
				"status":   "skipped",
				"message":  "Cookie为空，已跳过",
			})
			continue
		}

		updated := false
		if resolvedSite != "" {
			updated, err = h.repo.UpdateSiteCookieBySite(resolvedSite, cookie)
		} else {
			updated, err = h.repo.UpdateSiteCookie(resolvedNickname, cookie)
		}
		if err != nil {
			failedCount++
			results = append(results, map[string]any{
				"site":     resolvedSite,
				"nickname": resolvedNickname,
				"status":   "failed",
				"message":  "写入数据库失败",
			})
			continue
		}
		if !updated {
			failedCount++
			results = append(results, map[string]any{
				"site":     resolvedSite,
				"nickname": resolvedNickname,
				"status":   "failed",
				"message":  "未命中站点或未更新",
			})
			continue
		}

		updatedCount++
		results = append(results, map[string]any{
			"site":     resolvedSite,
			"nickname": resolvedNickname,
			"status":   "updated",
			"message":  "更新成功",
		})
	}

	message := fmt.Sprintf("批量同步完成：共 %d 条，成功 %d 条，跳过 %d 条，失败 %d 条。", len(payload.Items), updatedCount, skippedCount, failedCount)
	if failedCount > 0 {
		logx.Warnf(siteCookieSyncLogModule, "批量更新完成(部分失败) request_id=%s user=%s total=%d updated=%d skipped=%d failed=%d", requestID, userID, len(payload.Items), updatedCount, skippedCount, failedCount)
	} else {
		logx.Infof(siteCookieSyncLogModule, "批量更新完成 request_id=%s user=%s total=%d updated=%d skipped=%d", requestID, userID, len(payload.Items), updatedCount, skippedCount)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"message":       message,
		"total":         len(payload.Items),
		"updated_count": updatedCount,
		"skipped_count": skippedCount,
		"failed_count":  failedCount,
		"results":       results,
	})
}

func buildCookieSyncSiteIndexes(sites []map[string]any) (map[string]map[string]any, map[string]map[string]any) {
	siteByCode := make(map[string]map[string]any, len(sites))
	siteByNickname := make(map[string]map[string]any, len(sites))
	for _, site := range sites {
		siteCode := strings.ToLower(strings.TrimSpace(toString(site["site"], "")))
		nickname := strings.ToLower(strings.TrimSpace(toString(site["nickname"], "")))
		if siteCode != "" {
			siteByCode[siteCode] = site
		}
		if nickname != "" {
			siteByNickname[nickname] = site
		}
	}
	return siteByCode, siteByNickname
}

func buildCookieSyncDomains(baseURL, specialTrackerDomain string) []string {
	set := map[string]struct{}{}
	addDomain := func(raw string) {
		normalized := normalizeCookieSyncDomain(raw)
		if normalized == "" {
			return
		}
		set[normalized] = struct{}{}
		// 自动添加根域名，确保能获取 .domain.com 域下的 cookie
		rootDomain := extractRootDomain(normalized)
		if rootDomain != "" && rootDomain != normalized {
			set[rootDomain] = struct{}{}
		}
	}

	addDomain(baseURL)
	separatorNormalized := strings.NewReplacer("，", ",", ";", ",", "|", ",").Replace(specialTrackerDomain)
	for _, token := range strings.Split(separatorNormalized, ",") {
		addDomain(token)
	}

	domains := make([]string, 0, len(set))
	for domain := range set {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
}

// extractRootDomain 从域名中提取根域名（如 www.qingwapt.com -> qingwapt.com）
func extractRootDomain(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return domain
	}
	// 返回最后两部分作为根域名
	return strings.Join(parts[len(parts)-2:], ".")
}

func normalizeCookieSyncDomain(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "https://" + strings.TrimPrefix(value, "//")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(parsed.Hostname())), ".")
	if host == "" {
		return ""
	}
	if net.ParseIP(host) != nil || host == "localhost" || strings.Contains(host, ".") {
		return host
	}
	return ""
}

func hasCookieConfigured(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case string:
		trimmed := strings.TrimSpace(strings.ToLower(typed))
		return trimmed == "1" || trimmed == "true"
	default:
		return false
	}
}
