package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server-go/internal/platform/logx"
)

const settingsUpdateModule = "设置-保存配置"

func (h *Handler) GetSettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.GetSettingsMasked())
}

func (h *Handler) UpdateSettings(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.settings.UpdateSettings(payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法保存配置到文件。"})
		return
	}

	// 删除/停用下载器配置后，旧 downloader_id 对应的历史种子会继续存在于 DB 中；
	// 若不及时隐藏，会导致前端仍可用 downloader_filters 筛选出“伪删除”数据。
	cleanup := map[string]any{
		"hidden_disabled_records": int64(0),
		"hidden_removed_records":  int64(0),
	}
	if h.torrentData != nil {
		configuredIDs := make([]string, 0)
		enabledIDs := make([]string, 0)
		for _, cfg := range h.settings.DownloaderConfigs() {
			id := strings.TrimSpace(handlerToString(cfg["id"], ""))
			if id == "" {
				continue
			}
			configuredIDs = append(configuredIDs, id)
			if handlerToBool(cfg["enabled"], true) {
				enabledIDs = append(enabledIDs, id)
			}
		}

		hiddenDisabled, err := h.torrentData.HideDisabledDownloaderData(enabledIDs, configuredIDs)
		if err != nil {
			logx.Warnf(settingsUpdateModule, "隐藏停用下载器数据失败 err=%v", err)
		} else if hiddenDisabled > 0 {
			logx.Infof(settingsUpdateModule, "已隐藏停用下载器种子 hidden=%d", hiddenDisabled)
			cleanup["hidden_disabled_records"] = hiddenDisabled
		}

		hiddenRemoved, err := h.torrentData.HideRemovedDownloaderData(configuredIDs)
		if err != nil {
			logx.Warnf(settingsUpdateModule, "隐藏已删除下载器数据失败 err=%v", err)
		} else if hiddenRemoved > 0 {
			logx.Infof(settingsUpdateModule, "已隐藏无效下载器种子 hidden=%d", hiddenRemoved)
			cleanup["hidden_removed_records"] = hiddenRemoved
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "配置已成功保存。", "cleanup": cleanup})
}

func (h *Handler) CookieCloudSync(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}

	ccURL := strings.TrimSpace(handlerToString(payload["url"], ""))
	ccKey := strings.TrimSpace(handlerToString(payload["key"], ""))
	e2ePassword := strings.TrimSpace(handlerToString(payload["e2e_password"], ""))
	if ccURL == "" || ccKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "CookieCloud URL 和 KEY 不能为空。"})
		return
	}

	sites, err := h.sites.ListSites("all")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "从数据库获取站点列表失败。"})
		return
	}

	targetURL := strings.TrimRight(ccURL, "/") + "/get/" + ccKey
	reqBody := map[string]any{}
	if e2ePassword != "" {
		reqBody["password"] = e2ePassword
	}
	encodedBody, _ := json.Marshal(reqBody)

	request, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(encodedBody))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "构建 CookieCloud 请求失败。"})
		return
	}
	request.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": fmt.Sprintf("请求 CookieCloud 时出错: %v", err)})
		return
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "读取 CookieCloud 响应失败。"})
		return
	}

	responseData := map[string]any{}
	if err := json.Unmarshal(bodyBytes, &responseData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "CookieCloud 返回了无效数据。"})
		return
	}

	rawCookieData, ok := responseData["cookie_data"].(map[string]any)
	if !ok {
		if _, encrypted := responseData["encrypted"]; encrypted {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "获取到加密数据。请确保端对端加密密码已填写并正确。"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "从 CookieCloud 返回的数据格式不正确或为空。"})
		return
	}

	matchedDomains := map[string]struct{}{}
	updatedCount := 0
	for _, site := range sites {
		nickname := strings.TrimSpace(handlerToString(site["nickname"], ""))
		siteCode := strings.TrimSpace(handlerToString(site["site"], ""))
		if nickname == "" {
			continue
		}
		if strings.EqualFold(siteCode, "qingwapt") {
			continue
		}

		identifiers := map[string]struct{}{}
		identifiers[strings.ToLower(nickname)] = struct{}{}
		if siteCode != "" {
			identifiers[strings.ToLower(siteCode)] = struct{}{}
		}
		if baseURL := strings.TrimSpace(handlerToString(site["base_url"], "")); baseURL != "" {
			host := normalizeCookieDomain(baseURL)
			if host != "" {
				identifiers[strings.ToLower(host)] = struct{}{}
			}
		}

		for domain, rawCookies := range rawCookieData {
			normalizedDomain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
			if _, match := identifiers[normalizedDomain]; !match {
				continue
			}
			cookieString := cookieValueToString(rawCookies)
			cookieString = strings.TrimSpace(cookieString)
			if cookieString == "" {
				continue
			}
			if updated, err := h.sites.UpdateSiteCookie(nickname, cookieString); err == nil && updated {
				updatedCount++
				matchedDomains[domain] = struct{}{}
			}
			break
		}
	}

	unmatchedCount := len(rawCookieData) - len(matchedDomains)
	message := fmt.Sprintf("成功更新了 %d 个站点的 Cookie。", updatedCount)
	if unmatchedCount > 0 {
		message += fmt.Sprintf("在 CookieCloud 中另有 %d 个未匹配的 Cookie。", unmatchedCount)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":         true,
		"message":         message,
		"updated_count":   updatedCount,
		"unmatched_count": unmatchedCount,
	})
}
func normalizeCookieDomain(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		value = "http://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), ".")
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	return strings.TrimPrefix(host, ".")
}

func cookieValueToString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	list, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]map[string]any); ok {
			parts := make([]string, 0, len(typed))
			for _, item := range typed {
				name := strings.TrimSpace(handlerToString(item["name"], ""))
				val := strings.TrimSpace(handlerToString(item["value"], ""))
				if name != "" {
					parts = append(parts, name+"="+val)
				}
			}
			return strings.Join(parts, "; ")
		}
		return ""
	}

	parts := make([]string, 0, len(list))
	for _, item := range list {
		cookie, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(handlerToString(cookie["name"], ""))
		val := strings.TrimSpace(handlerToString(cookie["value"], ""))
		if name != "" {
			parts = append(parts, name+"="+val)
		}
	}
	return strings.Join(parts, "; ")
}
