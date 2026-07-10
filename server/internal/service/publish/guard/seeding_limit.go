package guard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/platform/logx"
)

const (
	seedingLimitGuardLogModule = "发布-预检查"

	seedingLimitPath    = "/seeding-limit/check"
	seedingLimitTimeout = 15 * time.Second

	minSeedsThreshold    = 5
	recentWindowSeconds  = int64(86400)
	ratioIgnoreThreshold = 1.2
)

var maxRecentAdditions = 15

func init() {
	if os.Getenv("VIP") == "true" {
		maxRecentAdditions = 999
	}
}

type seedingLimitResponse struct {
	Success     bool   `json:"success"`
	CanContinue bool   `json:"can_continue"`
	Message     string `json:"message"`
	Error       string `json:"error"`
}

// SeedingLimitStats 表示发布前限制的统计快照（用于队列/监控线程判断是否触发下一任务）。
// 注意：该统计仅走本地检查逻辑，不会调用远端预检查服务。
type SeedingLimitStats struct {
	CanContinue      bool     `json:"can_continue"`
	Message          string   `json:"message"`
	SubnetID         string   `json:"subnet_id"`
	Downloaders      []string `json:"downloaders"`
	MonitoredCount   int      `json:"monitored_count"`
	RecentCount      int      `json:"recent_count"`
	MaxRecentAllowed int      `json:"max_recent_allowed"`
}

type seedingLimitGroupStats struct {
	SubnetID        string
	DownloaderNames []string
	MonitoredCount  int
	RecentCount     int
}

type seedingLimitDownloader struct {
	ID       string
	Name     string
	Type     string
	Host     string
	Username string
	Password string
	Enabled  bool
	UseProxy bool
}

type qbTorrent struct {
	Hash        string  `json:"hash"`
	State       string  `json:"state"`
	NumComplete int     `json:"num_complete"`
	UpSpeed     int64   `json:"upspeed"`
	AddedOn     int64   `json:"added_on"`
	Ratio       float64 `json:"ratio"`
}

type transmissionTorrent struct {
	Status       int     `json:"status"`
	RateUpload   int64   `json:"rateUpload"`
	AddedDate    int64   `json:"addedDate"`
	UploadRatio  float64 `json:"uploadRatio"`
	TrackerStats []struct {
		SeederCount int `json:"seederCount"`
	} `json:"trackerStats"`
}

type transmissionResponse struct {
	Arguments struct {
		Torrents []transmissionTorrent `json:"torrents"`
	} `json:"arguments"`
	Result string `json:"result"`
}

// CheckDownloaderGate 检查下载器是否触发发布前限制。
// 参数/返回：downloaderID 为下载器 ID；返回 canContinue 与 message；检查异常时安全阻止发布。
// 失败场景：远端预检查不可用时回退本地检查；本地配置或下载器查询失败时返回 false。
// 副作用：可能访问远端预检查接口（当 GO_SERVICE_URL 配置存在），并可能直接连接下载器执行本地检查。
func CheckDownloaderGate(downloaderID string) (bool, string) {
	trimmedID := strings.TrimSpace(downloaderID)
	if trimmedID == "" {
		return true, ""
	}

	if canContinue, message, checked := checkByRemoteGuard(trimmedID); checked {
		return canContinue, message
	}

	return checkByLocalGuard(trimmedID)
}

// CheckDownloaderGateStats 返回指定 downloader_id 对应网段组的限制统计信息（仅本地检查）。
// 参数/返回：downloaderID 为下载器 ID；返回统计快照与 error。
// 失败场景：读取配置失败、解析 host 失败或下载器连接失败等会返回 error。
// 副作用：可能直连下载器拉取做种/暂停列表用于统计。
func CheckDownloaderGateStats(downloaderID string) (SeedingLimitStats, error) {
	stats := SeedingLimitStats{
		CanContinue:      true,
		Message:          "",
		SubnetID:         "",
		Downloaders:      []string{},
		MonitoredCount:   0,
		RecentCount:      0,
		MaxRecentAllowed: maxRecentAdditions,
	}

	trimmedID := strings.TrimSpace(downloaderID)
	if trimmedID == "" {
		return stats, nil
	}

	downloaders, err := loadDownloadersConfig()
	if err != nil {
		return SeedingLimitStats{}, err
	}

	target := findDownloaderByID(downloaders, trimmedID)
	if target == nil {
		stats.Message = "未找到下载器配置"
		return stats, nil
	}
	if target.UseProxy {
		stats.Message = "下载器开启代理，跳过预检查"
		return stats, nil
	}

	subnetID, err := resolveDownloaderSubnet(*target)
	if err != nil {
		return SeedingLimitStats{}, err
	}
	stats.SubnetID = subnetID

	group := make([]seedingLimitDownloader, 0, len(downloaders))
	for _, downloader := range downloaders {
		if !downloader.Enabled || downloader.UseProxy {
			continue
		}
		downloaderSubnet, parseErr := resolveDownloaderSubnet(downloader)
		if parseErr != nil {
			continue
		}
		if downloaderSubnet == subnetID {
			group = append(group, downloader)
		}
	}
	if len(group) == 0 {
		return stats, nil
	}

	groupStats, err := collectSeedingLimitGroupStats(subnetID, group)
	if err != nil {
		return SeedingLimitStats{}, err
	}
	canContinue, message := evaluateSeedingLimit(groupStats)
	stats.CanContinue = canContinue
	stats.Message = strings.TrimSpace(message)
	stats.Downloaders = append([]string{}, groupStats.DownloaderNames...)
	stats.MonitoredCount = groupStats.MonitoredCount
	stats.RecentCount = groupStats.RecentCount
	return stats, nil
}

func checkByRemoteGuard(downloaderID string) (bool, string, bool) {
	baseURL := strings.TrimSpace(os.Getenv("GO_SERVICE_URL"))
	if baseURL == "" {
		return true, "", false
	}

	endpoint := strings.TrimRight(baseURL, "/") + seedingLimitPath

	requestBody, _ := json.Marshal(map[string]string{"downloader_id": downloaderID})
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(requestBody))
	if err != nil {
		logx.Warnf(seedingLimitGuardLogModule, "构造预检查请求失败 downloader_id=%s err=%v", downloaderID, err)
		return true, "", false
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: seedingLimitTimeout}
	resp, err := client.Do(req)
	if err != nil {
		logx.Warnf(seedingLimitGuardLogModule, "远端预检查请求失败 downloader_id=%s err=%v，将回退本地检查", downloaderID, err)
		return true, "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 240))
		logx.Warnf(seedingLimitGuardLogModule, "远端预检查响应异常 downloader_id=%s status=%d body=%s，将回退本地检查", downloaderID, resp.StatusCode, strings.TrimSpace(string(body)))
		return true, "", false
	}

	payload := seedingLimitResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		logx.Warnf(seedingLimitGuardLogModule, "远端预检查响应解析失败 downloader_id=%s err=%v，将回退本地检查", downloaderID, err)
		return true, "", false
	}
	if !payload.Success {
		logx.Warnf(seedingLimitGuardLogModule, "远端预检查返回失败 downloader_id=%s error=%s，将回退本地检查", downloaderID, strings.TrimSpace(payload.Error))
		return true, "", false
	}

	return payload.CanContinue, strings.TrimSpace(payload.Message), true
}

func checkByLocalGuard(downloaderID string) (bool, string) {
	downloaders, err := loadDownloadersConfig()
	if err != nil {
		message := "加载下载器配置失败，已停止发布: " + err.Error()
		logx.Warnf(seedingLimitGuardLogModule, "%s downloader_id=%s", message, downloaderID)
		return false, message
	}

	target := findDownloaderByID(downloaders, downloaderID)
	if target == nil {
		message := "未找到下载器配置，已停止发布"
		logx.Warnf(seedingLimitGuardLogModule, "%s downloader_id=%s", message, downloaderID)
		return false, message
	}
	if target.UseProxy {
		return true, ""
	}

	targetSubnet, err := resolveDownloaderSubnet(*target)
	if err != nil {
		message := "解析下载器地址失败，已停止发布: " + err.Error()
		logx.Warnf(seedingLimitGuardLogModule, "%s downloader_id=%s host=%s", message, downloaderID, target.Host)
		return false, message
	}

	group := make([]seedingLimitDownloader, 0, len(downloaders))
	for _, downloader := range downloaders {
		if !downloader.Enabled || downloader.UseProxy {
			continue
		}
		subnetID, parseErr := resolveDownloaderSubnet(downloader)
		if parseErr != nil {
			continue
		}
		if subnetID == targetSubnet {
			group = append(group, downloader)
		}
	}

	if len(group) == 0 {
		return true, ""
	}

	return checkSeedingLimitForGroup(targetSubnet, group)
}

func loadDownloadersConfig() ([]seedingLimitDownloader, error) {
	paths := config.ResolveRuntimePaths()
	manager, err := config.NewManager(paths)
	if err != nil {
		return nil, err
	}

	root := manager.Get()
	rawDownloaders := toSliceAny(root["downloaders"])
	result := make([]seedingLimitDownloader, 0, len(rawDownloaders))
	for _, raw := range rawDownloaders {
		item := toMapAny(raw)
		if len(item) == 0 {
			continue
		}
		downloader := seedingLimitDownloader{
			ID:       strings.TrimSpace(toStringAny(item["id"])),
			Name:     strings.TrimSpace(toStringAny(item["name"])),
			Type:     strings.ToLower(strings.TrimSpace(toStringAny(item["type"]))),
			Host:     strings.TrimSpace(toStringAny(item["host"])),
			Username: toStringAny(item["username"]),
			Password: toStringAny(item["password"]),
			Enabled:  toBoolAny(item["enabled"], true),
			UseProxy: toBoolAny(item["use_proxy"], false),
		}
		if downloader.ID == "" || downloader.Type == "" || downloader.Host == "" {
			continue
		}
		result = append(result, downloader)
	}
	return result, nil
}

func findDownloaderByID(downloaders []seedingLimitDownloader, downloaderID string) *seedingLimitDownloader {
	for idx := range downloaders {
		if downloaders[idx].ID == downloaderID {
			return &downloaders[idx]
		}
	}
	return nil
}

func resolveDownloaderSubnet(downloader seedingLimitDownloader) (string, error) {
	normalizedHost := normalizeHostURL(downloader.Host)
	parsed, err := url.Parse(normalizedHost)
	if err != nil {
		return "", err
	}
	hostname := strings.TrimSpace(parsed.Hostname())
	if hostname == "" {
		return "", fmt.Errorf("host 为空")
	}

	ip := net.ParseIP(hostname)
	if ip == nil {
		return strings.ToLower(hostname), nil
	}
	v4 := ip.To4()
	if v4 == nil {
		return strings.ToLower(hostname), nil
	}
	return fmt.Sprintf("%d.%d.%d", v4[0], v4[1], v4[2]), nil
}

func checkSeedingLimitForGroup(subnetID string, downloaders []seedingLimitDownloader) (bool, string) {
	stats, err := collectSeedingLimitGroupStats(subnetID, downloaders)
	if err != nil {
		message := "下载器统计失败，已停止发布: " + err.Error()
		logx.Warnf(seedingLimitGuardLogModule, "%s subnet=%s.x", message, subnetID)
		return false, message
	}
	canContinue, message := evaluateSeedingLimit(stats)
	if canContinue {
		logx.Infof(seedingLimitGuardLogModule, "本地预检查通过 subnet=%s.x monitored=%d recent=%d", subnetID, stats.MonitoredCount, stats.RecentCount)
	}
	return canContinue, message
}

func collectSeedingLimitGroupStats(subnetID string, downloaders []seedingLimitDownloader) (seedingLimitGroupStats, error) {
	monitored := 0
	recent := 0
	names := make([]string, 0, len(downloaders))

	for _, downloader := range downloaders {
		uploadingCount, recentCount, err := checkDownloaderStatus(downloader)
		if err != nil {
			return seedingLimitGroupStats{}, fmt.Errorf("下载器 %s 查询失败: %w", downloader.ID, err)
		}
		monitored += uploadingCount
		recent += recentCount
		name := strings.TrimSpace(downloader.Name)
		if name == "" {
			name = downloader.ID
		}
		names = append(names, name)
	}

	return seedingLimitGroupStats{
		SubnetID:        subnetID,
		DownloaderNames: names,
		MonitoredCount:  monitored,
		RecentCount:     recent,
	}, nil
}

func evaluateSeedingLimit(stats seedingLimitGroupStats) (bool, string) {
	if stats.RecentCount >= maxRecentAdditions {
		message := fmt.Sprintf(
			"网段 %s.x 内的下载器组（%s）在过去24h内添加且活跃/暂停的（做种人数>%d或分享率>%.1f不计算）种子共 %d 个 (上限 %d)。已暂停发布。",
			stats.SubnetID,
			strings.Join(stats.DownloaderNames, ", "),
			minSeedsThreshold,
			ratioIgnoreThreshold,
			stats.RecentCount,
			maxRecentAdditions,
		)
		return false, message
	}
	return true, ""
}

func checkDownloaderStatus(downloader seedingLimitDownloader) (int, int, error) {
	switch strings.ToLower(strings.TrimSpace(downloader.Type)) {
	case "qbittorrent":
		return checkQBittorrentStatus(downloader)
	case "transmission":
		return checkTransmissionStatus(downloader)
	default:
		return 0, 0, fmt.Errorf("不支持的下载器类型: %s", downloader.Type)
	}
}

func checkQBittorrentStatus(downloader seedingLimitDownloader) (int, int, error) {
	baseURL := normalizeHostURL(downloader.Host)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: 10 * time.Second, Jar: jar}

	if err := qbLogin(client, baseURL, downloader.Username, downloader.Password); err != nil {
		logx.Warnf(seedingLimitGuardLogModule, "qB 登录失败 downloader_id=%s host=%s err=%v", downloader.ID, downloader.Host, err)
		return 0, 0, err
	}

	seedingTorrents, err := qbFetchTorrents(client, baseURL, "seeding")
	if err != nil {
		logx.Warnf(seedingLimitGuardLogModule, "qB 获取做种列表失败 downloader_id=%s host=%s err=%v", downloader.ID, downloader.Host, err)
		return 0, 0, err
	}
	pausedTorrents, err := qbFetchTorrents(client, baseURL, "paused")
	if err != nil {
		logx.Warnf(seedingLimitGuardLogModule, "qB 获取暂停列表失败 downloader_id=%s host=%s err=%v", downloader.ID, downloader.Host, err)
		return 0, 0, err
	}

	activeCount := 0
	pausedCount := 0
	recentCount := 0
	countedHashes := make(map[string]struct{})

	for _, torrent := range seedingTorrents {
		if torrent.NumComplete >= minSeedsThreshold {
			continue
		}
		if torrent.Ratio > ratioIgnoreThreshold {
			continue
		}
		if torrent.UpSpeed <= 0 {
			continue
		}
		activeCount++
		if isWithinRecentWindow(torrent.AddedOn) {
			recentCount++
		}
		if torrent.Hash != "" {
			countedHashes[torrent.Hash] = struct{}{}
		}
	}

	for _, torrent := range pausedTorrents {
		if torrent.Hash != "" {
			if _, exists := countedHashes[torrent.Hash]; exists {
				continue
			}
		}
		if !isQbPausedState(torrent.State) {
			continue
		}
		if torrent.NumComplete >= minSeedsThreshold {
			continue
		}
		if torrent.Ratio > ratioIgnoreThreshold {
			continue
		}
		pausedCount++
		if isWithinRecentWindow(torrent.AddedOn) {
			recentCount++
		}
	}

	return activeCount + pausedCount, recentCount, nil
}

func qbLogin(client *http.Client, baseURL, username, password string) error {
	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)

	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	loginResp := strings.TrimSpace(string(body))
	if loginResp != "Ok." && loginResp != "Ok" {
		return fmt.Errorf("登录响应异常: %s", loginResp)
	}
	return nil
}

func qbFetchTorrents(client *http.Client, baseURL, filter string) ([]qbTorrent, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/v2/torrents/info?filter="+url.QueryEscape(filter), nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	rows := make([]qbTorrent, 0)
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func checkTransmissionStatus(downloader seedingLimitDownloader) (int, int, error) {
	rpcURL := strings.TrimRight(normalizeHostURL(downloader.Host), "/") + "/transmission/rpc"
	payload := map[string]any{
		"method": "torrent-get",
		"arguments": map[string]any{
			"fields": []string{"status", "rateUpload", "addedDate", "trackerStats", "uploadRatio"},
		},
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return 0, 0, err
	}

	body, err := transmissionRPC(rpcURL, downloader.Username, downloader.Password, payloadBytes)
	if err != nil {
		logx.Warnf(seedingLimitGuardLogModule, "Transmission 调用失败 downloader_id=%s host=%s err=%v", downloader.ID, downloader.Host, err)
		return 0, 0, err
	}

	resp := transmissionResponse{}
	if err := json.Unmarshal(body, &resp); err != nil {
		logx.Warnf(seedingLimitGuardLogModule, "Transmission 响应解析失败 downloader_id=%s host=%s err=%v", downloader.ID, downloader.Host, err)
		return 0, 0, err
	}
	if resp.Result != "success" {
		return 0, 0, fmt.Errorf("Transmission 响应失败: %s", resp.Result)
	}

	activeCount := 0
	pausedCount := 0
	recentCount := 0

	for _, torrent := range resp.Arguments.Torrents {
		if transmissionSeeders(torrent) >= minSeedsThreshold {
			continue
		}
		if torrent.UploadRatio > ratioIgnoreThreshold {
			continue
		}
		isRecent := isWithinRecentWindow(torrent.AddedDate)
		if torrent.Status == 6 && torrent.RateUpload > 0 {
			activeCount++
			if isRecent {
				recentCount++
			}
		} else if torrent.Status == 0 {
			pausedCount++
			if isRecent {
				recentCount++
			}
		}
	}

	return activeCount + pausedCount, recentCount, nil
}

func transmissionRPC(rpcURL, username, password string, payload []byte) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	sessionID := ""

	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequest(http.MethodPost, rpcURL, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if sessionID != "" {
			req.Header.Set("X-Transmission-Session-Id", sessionID)
		}
		if strings.TrimSpace(username) != "" {
			req.SetBasicAuth(username, password)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusConflict {
			sessionID = strings.TrimSpace(resp.Header.Get("X-Transmission-Session-Id"))
			if sessionID == "" {
				return nil, fmt.Errorf("缺少 Session ID")
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return body, nil
	}

	return nil, fmt.Errorf("Transmission 会话协商失败")
}

func transmissionSeeders(t transmissionTorrent) int {
	maxSeeders := 0
	for _, tracker := range t.TrackerStats {
		if tracker.SeederCount > maxSeeders {
			maxSeeders = tracker.SeederCount
		}
	}
	return maxSeeders
}

func isWithinRecentWindow(addedUnix int64) bool {
	if addedUnix <= 0 {
		return false
	}
	threshold := time.Now().Unix() - recentWindowSeconds
	return addedUnix >= threshold
}

func isQbPausedState(state string) bool {
	switch strings.TrimSpace(state) {
	case "pausedDL", "pausedUP", "stoppedDL", "stoppedUP":
		return true
	default:
		return false
	}
}

func normalizeHostURL(host string) string {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(trimmed), "http://") && !strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		trimmed = "http://" + trimmed
	}
	return strings.TrimRight(trimmed, "/")
}

func toMapAny(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	if typed, ok := value.(map[string]interface{}); ok {
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = item
		}
		return result
	}
	return map[string]any{}
}

func toSliceAny(value any) []any {
	if value == nil {
		return []any{}
	}
	if typed, ok := value.([]any); ok {
		return typed
	}
	if typed, ok := value.([]interface{}); ok {
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	}
	return []any{}
}

func toStringAny(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprintf("%v", value)
	}
}

func toBoolAny(value any, fallback bool) bool {
	if value == nil {
		return fallback
	}
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
		lower := strings.ToLower(strings.TrimSpace(typed))
		if lower == "" {
			return fallback
		}
		return lower == "1" || lower == "true" || lower == "yes" || lower == "on"
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text == "" {
			return fallback
		}
		parsed, err := strconv.ParseBool(text)
		if err != nil {
			return fallback
		}
		return parsed
	}
}
