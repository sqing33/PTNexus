package downloaderclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TorrentSnapshot 表示从下载器读取的一条标准化种子快照。
// 参数/返回：用于跨下载器统一字段，供上层服务写入 torrents/torrent_upload_stats。
// 失败场景：无直接失败场景，具体失败由 FetchTorrents 返回。
// 副作用：无副作用，仅承载数据。
type TorrentSnapshot struct {
	Hash        string
	Name        string
	SavePath    string
	ContentPath string
	Size        int64
	Progress    float64
	State       string
	Trackers    []string
	Comment     string
	Group       string
	Seeders     int64
	Uploaded    int64
}

// FetchTorrents 拉取下载器当前全部种子并归一化为统一结构。
// 参数/返回：根据下载器类型自动走 qBittorrent 或 Transmission，返回种子快照列表。
// 失败场景：连接失败、认证失败、RPC 异常、响应解析失败时返回错误。
// 副作用：会向下载器发起网络请求。
func (d Downloader) FetchTorrents() ([]TorrentSnapshot, error) {
	if d.Type == "qbittorrent" && d.UseProxy {
		return d.fetchQBTorrentsByProxy()
	}

	switch d.Type {
	case "qbittorrent":
		return d.fetchQBTorrents()
	case "transmission":
		return d.fetchTransmissionTorrents()
	default:
		return nil, fmt.Errorf("不支持的下载器类型: %s", d.Type)
	}
}

// fetchQBTorrentsByProxy 通过盒子代理拉取 qBittorrent 全量种子。
// 参数/返回：使用配置中的 host/use_proxy/proxy_port 生成代理请求，成功返回标准化快照列表。
// 失败场景：代理地址解析失败、请求失败、HTTP 返回异常或响应解析失败时返回错误。
// 副作用：会向代理服务发起 HTTP 请求。
func (d Downloader) fetchQBTorrentsByProxy() ([]TorrentSnapshot, error) {
	proxyPort := d.ProxyPort
	if proxyPort <= 0 {
		proxyPort = 9090
	}

	parsedURL, err := parseHostURL(d.Host)
	if err != nil {
		return nil, fmt.Errorf("解析 host 失败: %w", err)
	}
	proxyIP := strings.TrimSpace(parsedURL.Hostname())
	if proxyIP == "" {
		proxyIP = extractHostFallback(d.Host)
	}
	if proxyIP == "" {
		return nil, fmt.Errorf("无法解析代理地址: host=%s", d.Host)
	}

	originPort := strings.TrimSpace(parsedURL.Port())
	if originPort == "" {
		originPort = "8080"
	}
	requestPayload := map[string]any{
		"downloaders": []map[string]any{
			{
				"id":       d.ID,
				"type":     d.Type,
				"host":     "http://127.0.0.1:" + originPort,
				"username": d.Username,
				"password": d.Password,
			},
		},
		"include_comment":  true,
		"include_trackers": true,
	}
	payloadBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return nil, fmt.Errorf("构造代理请求失败: %w", err)
	}

	proxyURL := fmt.Sprintf("http://%s:%d/api/torrents/all", proxyIP, proxyPort)
	request, err := http.NewRequest(http.MethodPost, proxyURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("创建代理请求失败: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("代理请求失败: %w", err)
	}
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("代理返回异常: HTTP %d %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	rows := make([]map[string]any, 0)
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("解析代理响应失败: %w", err)
	}

	result := make([]TorrentSnapshot, 0, len(rows))
	for _, row := range rows {
		hash := strings.TrimSpace(toString(row["hash"], ""))
		name := strings.TrimSpace(toString(row["name"], ""))
		if hash == "" || name == "" {
			continue
		}

		trackers := extractProxyTrackers(row["trackers"])
		group := strings.TrimSpace(toString(row["category"], ""))
		if group == "" {
			group = firstCSVItem(toString(row["tags"], ""))
		}

		progress := normalizeProgressPercent(row["progress"])
		seeders := toInt64Any(row["seeders"])
		if seeders <= 0 {
			seeders = toInt64Any(row["num_seeds"])
		}

		result = append(result, TorrentSnapshot{
			Hash:        hash,
			Name:        name,
			SavePath:    strings.TrimSpace(toString(row["save_path"], "")),
			ContentPath: strings.TrimSpace(toString(row["content_path"], "")),
			Size:        toInt64Any(row["size"]),
			Progress:    progress,
			State:       formatTorrentState(toString(row["state"], "")),
			Trackers:    trackers,
			Comment:     strings.TrimSpace(toString(row["comment"], "")),
			Group:       group,
			Seeders:     seeders,
			Uploaded:    toInt64Any(row["uploaded"]),
		})
	}
	return result, nil
}

func (d Downloader) fetchQBTorrents() ([]TorrentSnapshot, error) {
	client, err := newQBClient(d)
	if err != nil {
		return nil, err
	}
	if err := client.Login(); err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("filter", "all")
	body, err := client.Get("torrents/info", params)
	if err != nil {
		return nil, err
	}

	rows := make([]map[string]any, 0)
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("qB 种子列表解析失败: %w", err)
	}

	result := make([]TorrentSnapshot, 0, len(rows))
	for _, row := range rows {
		hash := strings.TrimSpace(toString(row["hash"], ""))
		name := strings.TrimSpace(toString(row["name"], ""))
		if hash == "" || name == "" {
			continue
		}

		trackers := make([]string, 0, 2)
		if tracker := strings.TrimSpace(toString(row["tracker"], "")); tracker != "" {
			trackers = append(trackers, tracker)
		}

		group := strings.TrimSpace(toString(row["category"], ""))
		if group == "" {
			group = firstCSVItem(toString(row["tags"], ""))
		}

		progress := normalizeProgressPercent(row["progress"])
		seeders := toInt64Any(row["num_seeds"])
		if seeders <= 0 {
			seeders = toInt64Any(row["num_complete"])
		}

		result = append(result, TorrentSnapshot{
			Hash:        hash,
			Name:        name,
			SavePath:    strings.TrimSpace(toString(row["save_path"], "")),
			ContentPath: strings.TrimSpace(toString(row["content_path"], "")),
			Size:        toInt64Any(row["size"]),
			Progress:    progress,
			State:       formatTorrentState(toString(row["state"], "")),
			Trackers:    trackers,
			Comment:     strings.TrimSpace(toString(row["comment"], "")),
			Group:       group,
			Seeders:     seeders,
			Uploaded:    toInt64Any(row["uploaded"]),
		})
	}

	return result, nil
}

func (d Downloader) fetchTransmissionTorrents() ([]TorrentSnapshot, error) {
	client := newTransmissionClient(d)
	response, err := client.Call("torrent-get", map[string]any{
		"fields": []string{
			"hashString",
			"name",
			"downloadDir",
			"totalSize",
			"sizeWhenDone",
			"status",
			"comment",
			"trackers",
			"trackerStats",
			"percentDone",
			"uploadedEver",
			"peersGettingFromUs",
			"labels",
		},
	})
	if err != nil {
		return nil, err
	}

	args := toMap(response["arguments"])
	torrents := toSlice(args["torrents"])
	result := make([]TorrentSnapshot, 0, len(torrents))

	for _, raw := range torrents {
		row := toMap(raw)
		hash := strings.TrimSpace(toString(row["hashString"], ""))
		name := strings.TrimSpace(toString(row["name"], ""))
		if hash == "" || name == "" {
			continue
		}

		size := toInt64Any(row["sizeWhenDone"])
		if size <= 0 {
			size = toInt64Any(row["totalSize"])
		}

		trackers := extractTransmissionTrackers(row)
		group := firstLabel(row["labels"])
		statusText := transmissionStatusText(int(toInt64Any(row["status"])))

		result = append(result, TorrentSnapshot{
			Hash:     hash,
			Name:     name,
			SavePath: strings.TrimSpace(toString(row["downloadDir"], "")),
			Size:     size,
			Progress: normalizeProgressPercent(row["percentDone"]),
			State:    formatTorrentState(statusText),
			Trackers: trackers,
			Comment:  strings.TrimSpace(toString(row["comment"], "")),
			Group:    group,
			Seeders:  toInt64Any(row["peersGettingFromUs"]),
			Uploaded: toInt64Any(row["uploadedEver"]),
		})
	}

	return result, nil
}

func firstCSVItem(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	for _, part := range strings.Split(trimmed, ",") {
		item := strings.TrimSpace(part)
		if item != "" {
			return item
		}
	}
	return ""
}

func firstLabel(raw any) string {
	switch typed := raw.(type) {
	case []string:
		for _, item := range typed {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				return trimmed
			}
		}
	case []any:
		for _, item := range typed {
			trimmed := strings.TrimSpace(toString(item, ""))
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func extractTransmissionTrackers(row map[string]any) []string {
	result := make([]string, 0, 4)
	seen := map[string]struct{}{}

	appendTracker := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}

	for _, item := range toSlice(row["trackers"]) {
		tracker := toMap(item)
		appendTracker(toString(tracker["announce"], ""))
		appendTracker(toString(tracker["host"], ""))
	}
	for _, item := range toSlice(row["trackerStats"]) {
		tracker := toMap(item)
		appendTracker(toString(tracker["announce"], ""))
		appendTracker(toString(tracker["host"], ""))
	}

	return result
}

func extractProxyTrackers(raw any) []string {
	result := make([]string, 0, 4)
	seen := map[string]struct{}{}

	appendTracker := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}

	for _, item := range toSlice(raw) {
		tracker := toMap(item)
		appendTracker(toString(tracker["url"], ""))
		appendTracker(toString(tracker["announce"], ""))
		appendTracker(toString(tracker["host"], ""))
	}
	return result
}

func normalizeProgressPercent(value any) float64 {
	raw := toFloat64(value)
	if raw <= 0 {
		return 0
	}
	if raw <= 1 {
		raw *= 100
	}
	if raw < 0 {
		return 0
	}
	if raw > 100 {
		return 100
	}
	return raw
}

func toFloat64(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err == nil {
			return parsed
		}
		return 0
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0
		}
		parsed, err := json.Number(trimmed).Float64()
		if err == nil {
			return parsed
		}
		return 0
	default:
		return 0
	}
}

func transmissionStatusText(status int) string {
	switch status {
	case 0:
		return "stopped"
	case 1:
		return "check_wait"
	case 2:
		return "checking"
	case 3:
		return "queued"
	case 4:
		return "downloading"
	case 5:
		return "queued"
	case 6:
		return "seeding"
	default:
		return fmt.Sprintf("status_%d", status)
	}
}

func formatTorrentState(state string) string {
	stateLower := strings.ToLower(strings.TrimSpace(state))
	stateMap := map[string]string{
		"downloading":   "下载中",
		"forceddl":      "下载中",
		"uploading":     "做种中",
		"stalledup":     "做种中",
		"forcedup":      "做种中",
		"seed":          "做种中",
		"seeding":       "做种中",
		"paused":        "暂停",
		"stopped":       "暂停",
		"stalleddl":     "暂停",
		"check_wait":    "校验中",
		"checking":      "校验中",
		"check":         "校验中",
		"error":         "错误",
		"missingfiles":  "文件丢失",
		"moving":        "移动中",
		"allocating":    "分配空间",
		"queued":        "队列",
		"queuedup":      "队列",
		"queueddl":      "队列",
		"meta":          "下载中",
		"stalled":       "暂停",
		"upnp":          "暂停",
		"downloadingup": "下载中",
	}
	for key, value := range stateMap {
		if strings.Contains(stateLower, key) {
			return value
		}
	}
	if strings.TrimSpace(state) == "" {
		return "未知"
	}
	runes := []rune(state)
	if len(runes) == 0 {
		return "未知"
	}
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}
