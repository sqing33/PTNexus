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

	"github.com/pt-nexus/server-go/internal/config"
	"github.com/pt-nexus/server-go/internal/platform/logx"
)

const (
	seedingLimitGuardLogModule = "发布-预检查"

	seedingLimitPath    = "/seeding-limit/check"
	seedingLimitTimeout = 15 * time.Second

	maxRecentAdditions  = 15
	minSeedsThreshold   = 5
	recentWindowSeconds = int64(864000)
)

type seedingLimitResponse struct {
	Success     bool   `json:"success"`
	CanContinue bool   `json:"can_continue"`
	Message     string `json:"message"`
	Error       string `json:"error"`
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
	Hash        string `json:"hash"`
	State       string `json:"state"`
	NumComplete int    `json:"num_complete"`
	UpSpeed     int64  `json:"upspeed"`
	AddedOn     int64  `json:"added_on"`
}

type transmissionTorrent struct {
	Status       int   `json:"status"`
	RateUpload   int64 `json:"rateUpload"`
	AddedDate    int64 `json:"addedDate"`
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
// 参数/返回：downloaderID 为下载器 ID；返回 canContinue 与 message；检查异常时默认放行（返回 true, ""）。
// 失败场景：远端预检查不可用或本地检查异常时记录日志并默认放行。
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
		logx.Warnf(seedingLimitGuardLogModule, "加载下载器配置失败 downloader_id=%s err=%v", downloaderID, err)
		return true, ""
	}

	target := findDownloaderByID(downloaders, downloaderID)
	if target == nil {
		logx.Warnf(seedingLimitGuardLogModule, "未找到下载器配置，跳过预检查 downloader_id=%s", downloaderID)
		return true, ""
	}
	if target.UseProxy {
		return true, ""
	}

	targetSubnet, err := resolveDownloaderSubnet(*target)
	if err != nil {
		logx.Warnf(seedingLimitGuardLogModule, "解析下载器地址失败，跳过预检查 downloader_id=%s host=%s err=%v", downloaderID, target.Host, err)
		return true, ""
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
	totalUploading := 0
	recentTotalCount := 0
	downloaderNames := make([]string, 0, len(downloaders))

	for _, downloader := range downloaders {
		uploadingCount, recentCount := checkDownloaderStatus(downloader)
		totalUploading += uploadingCount
		recentTotalCount += recentCount
		name := strings.TrimSpace(downloader.Name)
		if name == "" {
			name = downloader.ID
		}
		downloaderNames = append(downloaderNames, name)
	}

	if recentTotalCount > maxRecentAdditions {
		message := fmt.Sprintf(
			"网段 %s.x 内的下载器组（%s）在过去24h内添加且活跃/暂停的（做种人数>%d不计算）种子共 %d 个 (上限 %d)。已暂停发布。",
			subnetID,
			strings.Join(downloaderNames, ", "),
			minSeedsThreshold,
			recentTotalCount,
			maxRecentAdditions,
		)
		return false, message
	}

	logx.Infof(seedingLimitGuardLogModule, "本地预检查通过 subnet=%s.x monitored=%d recent=%d", subnetID, totalUploading, recentTotalCount)
	return true, ""
}

func checkDownloaderStatus(downloader seedingLimitDownloader) (int, int) {
	switch strings.ToLower(strings.TrimSpace(downloader.Type)) {
	case "qbittorrent":
		return checkQBittorrentStatus(downloader)
	case "transmission":
		return checkTransmissionStatus(downloader)
	default:
		return 0, 0
	}
}

func checkQBittorrentStatus(downloader seedingLimitDownloader) (int, int) {
	baseURL := normalizeHostURL(downloader.Host)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: 10 * time.Second, Jar: jar}

	if err := qbLogin(client, baseURL, downloader.Username, downloader.Password); err != nil {
		logx.Warnf(seedingLimitGuardLogModule, "qB 登录失败 downloader_id=%s host=%s err=%v", downloader.ID, downloader.Host, err)
		return 0, 0
	}

	seedingTorrents, err := qbFetchTorrents(client, baseURL, "seeding")
	if err != nil {
		logx.Warnf(seedingLimitGuardLogModule, "qB 获取做种列表失败 downloader_id=%s host=%s err=%v", downloader.ID, downloader.Host, err)
		return 0, 0
	}
	pausedTorrents, err := qbFetchTorrents(client, baseURL, "paused")
	if err != nil {
		logx.Warnf(seedingLimitGuardLogModule, "qB 获取暂停列表失败 downloader_id=%s host=%s err=%v", downloader.ID, downloader.Host, err)
		return 0, 0
	}

	activeCount := 0
	pausedCount := 0
	recentCount := 0
	countedHashes := make(map[string]struct{})

	for _, torrent := range seedingTorrents {
		if torrent.NumComplete >= minSeedsThreshold {
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
		pausedCount++
		if isWithinRecentWindow(torrent.AddedOn) {
			recentCount++
		}
	}

	return activeCount + pausedCount, recentCount
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

func checkTransmissionStatus(downloader seedingLimitDownloader) (int, int) {
	rpcURL := strings.TrimRight(normalizeHostURL(downloader.Host), "/") + "/transmission/rpc"
	payload := map[string]any{
		"method": "torrent-get",
		"arguments": map[string]any{
			"fields": []string{"status", "rateUpload", "addedDate", "trackerStats"},
		},
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return 0, 0
	}

	body, err := transmissionRPC(rpcURL, downloader.Username, downloader.Password, payloadBytes)
	if err != nil {
		logx.Warnf(seedingLimitGuardLogModule, "Transmission 调用失败 downloader_id=%s host=%s err=%v", downloader.ID, downloader.Host, err)
		return 0, 0
	}

	resp := transmissionResponse{}
	if err := json.Unmarshal(body, &resp); err != nil {
		logx.Warnf(seedingLimitGuardLogModule, "Transmission 响应解析失败 downloader_id=%s host=%s err=%v", downloader.ID, downloader.Host, err)
		return 0, 0
	}
	if resp.Result != "success" {
		return 0, 0
	}

	activeCount := 0
	pausedCount := 0
	recentCount := 0

	for _, torrent := range resp.Arguments.Torrents {
		if transmissionSeeders(torrent) >= minSeedsThreshold {
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

	return activeCount + pausedCount, recentCount
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
