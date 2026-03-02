package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

const downloaderInfoModule = "设置-下载器"

// DownloadersList 返回启用下载器的简要列表。
// 参数/返回：无请求体，返回仅包含 id/name 的数组。
// 失败场景：读取配置异常时返回空数组（由服务层兜底）。
// 副作用：无副作用，仅查询内存配置。
func (h *Handler) DownloadersList(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.DownloadersList(true))
}

// AllDownloaders 返回全部下载器（含启用状态）的简要列表。
// 参数/返回：无请求体，返回包含 id/name/enabled 的数组。
// 失败场景：读取配置异常时返回空数组（由服务层兜底）。
// 副作用：无副作用，仅查询内存配置。
func (h *Handler) AllDownloaders(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.DownloadersList(false))
}

// TestConnection 测试单个下载器的基础连通性（仅 host 可达性）。
// 参数/返回：读取 JSON 配置并返回 success/message。
// 失败场景：参数非法、host 不可达或返回码异常时返回 success=false。
// 副作用：会向目标 host 发起一次短超时 HTTP 请求。
func (h *Handler) TestConnection(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}

	name := handlerToString(payload["name"], "下载器")
	typ := strings.ToLower(strings.TrimSpace(handlerToString(payload["type"], "")))
	host := strings.TrimSpace(handlerToString(payload["host"], ""))

	// Python 侧：type 不合法时返回 400。
	if typ != "qbittorrent" && typ != "transmission" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的客户端类型。"})
		return
	}
	if host == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("'%s' 连接失败: 缺少 host 配置", name)})
		return
	}

	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}

	request, err := http.NewRequest(http.MethodGet, host, nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("'%s' 连接失败: %v", name, err)})
		return
	}
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("'%s' 连接失败: %v", name, err)})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("下载器 '%s' 连接测试成功", name)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("'%s' 连接失败: HTTP %d", name, resp.StatusCode)})
}

// DownloaderInfo 返回首页下载器状态与流量统计，语义对齐 Python 版本。
// 参数/返回：无请求体，返回每个下载器的状态、版本和今日/累计流量信息。
// 失败场景：DB 查询失败时流量字段回退为 0；连接探测失败时状态为“连接失败”并附错误信息。
// 副作用：会对启用下载器发起网络探测（直连或代理）以获取连接状态和版本号。
func (h *Handler) DownloaderInfo(c *gin.Context) {
	totals, err := h.torrents.TrafficTotals()
	if err != nil {
		totals = map[string]map[string]int64{}
		logx.Warnf(downloaderInfoModule, "读取累计流量失败 err=%v", err)
	}
	today, err := h.torrents.TrafficToday()
	if err != nil {
		today = map[string]map[string]int64{}
		logx.Warnf(downloaderInfoModule, "读取今日流量失败 err=%v", err)
	}

	result := h.settings.BuildDownloaderInfo(totals, today)
	configs := h.settings.DownloaderConfigs()
	configByID := map[string]map[string]any{}
	for _, cfg := range configs {
		id := handlerToString(cfg["id"], "")
		if id == "" {
			continue
		}
		configByID[id] = cfg
	}

	for _, item := range result {
		details := ensureDetailsMap(item["details"])
		item["details"] = details

		enabled := handlerToBool(item["enabled"], false)
		if !enabled {
			item["status"] = "未配置"
			continue
		}

		downloaderID := handlerToString(item["id"], "")
		downloaderName := handlerToString(item["name"], "下载器")
		cfg, ok := configByID[downloaderID]
		if !ok {
			item["status"] = "连接失败"
			details["错误信息"] = "未找到下载器配置"
			logx.Warnf(downloaderInfoModule, "下载器配置缺失 id=%s name=%s", downloaderID, downloaderName)
			continue
		}

		version, detectErr := resolveDownloaderVersion(cfg)
		if detectErr != nil {
			item["status"] = "连接失败"
			details["错误信息"] = detectErr.Error()
			logx.Warnf(downloaderInfoModule, "连接探测失败 id=%s name=%s err=%v", downloaderID, downloaderName, detectErr)
			continue
		}

		item["status"] = "已连接"
		details["版本"] = version
		delete(details, "错误信息")
	}

	c.JSON(http.StatusOK, result)
}

func ensureDetailsMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func resolveDownloaderVersion(cfg map[string]any) (string, error) {
	typ := strings.ToLower(strings.TrimSpace(handlerToString(cfg["type"], "")))
	if typ == "" {
		return "", errors.New("缺少下载器类型配置")
	}
	if typ == "qbittorrent" && handlerToBool(cfg["use_proxy"], false) {
		stats, err := getProxyDownloaderStats(cfg)
		if err != nil {
			return "", err
		}
		version := strings.TrimSpace(handlerToString(stats["version"], "未知"))
		if version == "" {
			version = "未知"
		}
		return version, nil
	}

	downloader := downloaderclient.Downloader{
		ID:       handlerToString(cfg["id"], ""),
		Name:     handlerToString(cfg["name"], ""),
		Type:     typ,
		Host:     strings.TrimSpace(handlerToString(cfg["host"], "")),
		Username: handlerToString(cfg["username"], ""),
		Password: handlerToString(cfg["password"], ""),
		Enabled:  true,
	}
	if downloader.Host == "" {
		return "", errors.New("缺少 host 配置")
	}
	return downloader.FetchVersion()
}

func getProxyDownloaderStats(clientConfig map[string]any) (map[string]any, error) {
	hostValue := strings.TrimSpace(handlerToString(clientConfig["host"], ""))
	if hostValue == "" {
		return nil, errors.New("缺少 host 配置")
	}

	parsedURL, err := parseHostURL(hostValue)
	if err != nil {
		return nil, fmt.Errorf("解析 host 失败: %w", err)
	}

	proxyIP := strings.TrimSpace(parsedURL.Hostname())
	if proxyIP == "" {
		proxyIP = extractHostFallback(hostValue)
	}
	if proxyIP == "" {
		return nil, errors.New("无法解析代理地址")
	}

	proxyPort := handlerToInt(clientConfig["proxy_port"], 9090)
	proxyBaseURL := fmt.Sprintf("http://%s:%d", proxyIP, proxyPort)

	originPort := parsedURL.Port()
	if strings.TrimSpace(originPort) == "" {
		originPort = "8080"
	}
	proxyDownloaderConfig := map[string]any{
		"id":       handlerToString(clientConfig["id"], ""),
		"type":     handlerToString(clientConfig["type"], ""),
		"host":     "http://127.0.0.1:" + originPort,
		"username": handlerToString(clientConfig["username"], ""),
		"password": handlerToString(clientConfig["password"], ""),
	}

	payload, err := json.Marshal([]map[string]any{proxyDownloaderConfig})
	if err != nil {
		return nil, fmt.Errorf("构造代理请求失败: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, proxyBaseURL+"/api/stats/server", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("创建代理请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("代理请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("代理返回异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	statsData := make([]map[string]any, 0)
	if err := json.Unmarshal(body, &statsData); err != nil {
		return nil, fmt.Errorf("解析代理响应失败: %w", err)
	}
	if len(statsData) == 0 {
		return nil, errors.New("代理返回空统计信息")
	}
	return statsData[0], nil
}

func parseHostURL(hostValue string) (*url.URL, error) {
	normalized := hostValue
	if !strings.HasPrefix(normalized, "http://") && !strings.HasPrefix(normalized, "https://") {
		normalized = "http://" + normalized
	}
	return url.Parse(normalized)
}

func extractHostFallback(hostValue string) string {
	value := strings.TrimSpace(hostValue)
	if value == "" {
		return ""
	}
	if index := strings.Index(value, "://"); index >= 0 {
		value = value[index+3:]
	}
	if index := strings.Index(value, "/"); index >= 0 {
		value = value[:index]
	}
	if index := strings.Index(value, ":"); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

func handlerToBool(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		lowered := strings.ToLower(strings.TrimSpace(typed))
		if lowered == "true" || lowered == "1" || lowered == "yes" {
			return true
		}
		if lowered == "false" || lowered == "0" || lowered == "no" {
			return false
		}
		return fallback
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return fallback
	}
}

func handlerToInt(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return fallback
		}
		return parsed
	default:
		return fallback
	}
}
