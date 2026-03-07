// proxy.go
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ======================= 结构体定义 (无变动) =======================

type DownloaderConfig struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
}
type NormalizedTorrent struct {
	Hash         string
	Name         string
	Size         int64
	Progress     float64
	State        string
	SavePath     string
	Comment      string
	Trackers     []map[string]string
	Uploaded     int64
	Downloaded   int64
	Ratio        float64
	DownloaderID string
}
type NormalizedInfo struct {
	Hash         string              `json:"hash"`
	Name         string              `json:"name"`
	Size         int64               `json:"size"`
	Progress     float64             `json:"progress"`
	State        string              `json:"state"`
	SavePath     string              `json:"save_path"`
	Comment      string              `json:"comment,omitempty"`
	Trackers     []map[string]string `json:"trackers"`
	Uploaded     int64               `json:"uploaded"`
	Downloaded   int64               `json:"downloaded"`
	Ratio        float64             `json:"ratio"`
	DownloaderID string              `json:"downloader_id"`
}
type TorrentsRequest struct {
	Downloaders     []DownloaderConfig `json:"downloaders"`
	IncludeComment  bool               `json:"include_comment,omitempty"`
	IncludeTrackers bool               `json:"include_trackers,omitempty"`
}
type ServerStats struct {
	DownloaderID  string `json:"downloader_id"`
	DownloadSpeed int64  `json:"download_speed"`
	UploadSpeed   int64  `json:"upload_speed"`
	TotalDownload int64  `json:"total_download"`
	TotalUpload   int64  `json:"total_upload"`
	Version       string `json:"version,omitempty"`
}
type FlexibleTracker struct {
	URL        string      `json:"url"`
	Status     int         `json:"status"`
	Tier       interface{} `json:"tier"`
	NumPeers   int         `json:"num_peers"`
	NumSeeds   int         `json:"num_seeds"`
	NumLeeches int         `json:"num_leeches"`
	Msg        string      `json:"msg"`
}
type qbHTTPClient struct {
	Client     *http.Client
	BaseURL    string
	IsLoggedIn bool
}
type ScreenshotRequest struct {
	RemotePath    string    `json:"remote_path"`
	ContentName   string    `json:"content_name,omitempty"`
	Mode          string    `json:"mode,omitempty"`
	PreviewCount  int       `json:"preview_count,omitempty"`
	SelectedTimes []float64 `json:"selected_times,omitempty"`
}
type ScreenshotPreviewCandidate struct {
	ID          string  `json:"id"`
	TimeSeconds float64 `json:"time_seconds"`
	TimeLabel   string  `json:"time_label"`
	PreviewData string  `json:"preview_data"`
	Recommended bool    `json:"recommended"`
}
type ScreenshotResponse struct {
	Success           bool                         `json:"success"`
	Message           string                       `json:"message"`
	BBCode            string                       `json:"bbcode,omitempty"`
	HasSubtitleStream bool                         `json:"has_subtitle_stream,omitempty"`
	PreviewCandidates []ScreenshotPreviewCandidate `json:"preview_candidates,omitempty"`
}
type MediaInfoRequest struct {
	RemotePath  string `json:"remote_path"`
	ContentName string `json:"content_name,omitempty"`
}
type MediaInfoResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	MediaInfo string `json:"mediainfo,omitempty"`
	IsBDMV    bool   `json:"is_bdmv,omitempty"` // 新增：标识是否为原盘
}
type FileCheckRequest struct {
	RemotePath string `json:"remote_path"`
}
type FileCheckResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Exists  bool   `json:"exists"`
	IsFile  bool   `json:"is_file,omitempty"`
	Size    int64  `json:"size,omitempty"`
}
type BatchFileCheckRequest struct {
	RemotePaths []string `json:"remote_paths"`
}
type FileCheckResult struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	IsFile bool   `json:"is_file"`
	Size   int64  `json:"size"`
}
type BatchFileCheckResponse struct {
	Success bool              `json:"success"`
	Message string            `json:"message"`
	Results []FileCheckResult `json:"results"`
}
type EpisodeCountRequest struct {
	RemotePath string `json:"remote_path"`
}
type EpisodeCountResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	EpisodeCount int    `json:"episode_count,omitempty"`
	SeasonNumber int    `json:"season_number,omitempty"`
}

type UploadLimitGroup struct {
	LimitMBps  int      `json:"limit_mbps"`
	TorrentIDs []string `json:"torrent_ids"`
}

type UploadLimitDownloader struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Host     string             `json:"host"`
	Username string             `json:"username"`
	Password string             `json:"password"`
	Actions  []UploadLimitGroup `json:"actions"`
}

type UploadLimitBatchRequest struct {
	Downloaders []UploadLimitDownloader `json:"downloaders"`
}

type UploadLimitResult struct {
	DownloaderID    string   `json:"downloader_id"`
	AppliedGroups   int      `json:"applied_groups"`
	AppliedTorrents int      `json:"applied_torrents"`
	Errors          []string `json:"errors"`
}

type UploadLimitBatchResponse struct {
	Success bool                `json:"success"`
	Results []UploadLimitResult `json:"results"`
}

type SubtitleEvent struct {
	StartTime float64
	EndTime   float64
}

var isoMountMutex sync.Mutex

// ======================= 辅助函数 (无变动) =======================

// normalizePath 规范化路径，将反斜杠转换为正斜杠
// 解决 Windows 客户端发送的路径在 Linux 服务器上无法识别的问题
func normalizePath(path string) string {
	// 将所有反斜杠替换为正斜杠
	normalized := strings.ReplaceAll(path, "\\", "/")
	// 清理路径中的多余斜杠（如 // -> /）
	for strings.Contains(normalized, "//") {
		normalized = strings.ReplaceAll(normalized, "//", "/")
	}
	return normalized
}

func isISOFileInput(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	return strings.EqualFold(filepath.Ext(trimmed), ".iso")
}

func withMountedISOIfNeeded(inputPath string, scene string, fn func(resolvedPath string) error) (retErr error) {
	normalizedPath := normalizePath(inputPath)
	if !isISOFileInput(normalizedPath) {
		return fn(normalizedPath)
	}

	fileInfo, err := os.Stat(normalizedPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("ISO文件不存在: %s", normalizedPath)
	}
	if err != nil {
		return fmt.Errorf("访问ISO文件失败: %v", err)
	}
	if fileInfo.IsDir() {
		return fmt.Errorf("ISO路径必须是文件: %s", normalizedPath)
	}

	isoMountMutex.Lock()
	defer isoMountMutex.Unlock()

	mountDir, err := os.MkdirTemp("", "ptnexus-iso-*")
	if err != nil {
		return fmt.Errorf("创建ISO挂载目录失败: %v", err)
	}

	mountStartedAt := time.Now()
	_, err = executeCommand("mount", "-o", "loop,ro,nosuid,nodev,noexec", normalizedPath, mountDir)
	if err != nil {
		_ = os.RemoveAll(mountDir)
		return fmt.Errorf("ISO挂载失败（需要 root/CAP_SYS_ADMIN）: %v", err)
	}
	log.Printf("%s: ISO挂载成功 iso=%s mount_dir=%s 耗时=%s", scene, normalizedPath, mountDir, time.Since(mountStartedAt).Round(time.Millisecond))

	defer func() {
		umountStartedAt := time.Now()
		_, umountErr := executeCommand("umount", mountDir)
		if umountErr != nil {
			cleanupErr := fmt.Errorf("ISO卸载失败（请检查占用句柄）: %v", umountErr)
			log.Printf("%s: ISO卸载失败 iso=%s mount_dir=%s err=%v", scene, normalizedPath, mountDir, umountErr)
			if retErr != nil {
				retErr = fmt.Errorf("%v；%v", retErr, cleanupErr)
			} else {
				retErr = cleanupErr
			}
		} else {
			log.Printf("%s: ISO卸载成功 iso=%s mount_dir=%s 耗时=%s", scene, normalizedPath, mountDir, time.Since(umountStartedAt).Round(time.Millisecond))
		}

		if err := os.RemoveAll(mountDir); err != nil {
			log.Printf("%s: 清理ISO挂载目录失败 mount_dir=%s err=%v", scene, mountDir, err)
			if retErr == nil {
				retErr = fmt.Errorf("清理ISO挂载目录失败: %v", err)
			}
		}
	}()

	return fn(mountDir)
}

func newQBHTTPClient(baseURL string) (*qbHTTPClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &qbHTTPClient{
		Client:  &http.Client{Jar: jar, Timeout: 180 * time.Second},
		BaseURL: baseURL,
	}, nil
}
func (c *qbHTTPClient) Login(username, password string) error {
	loginURL := fmt.Sprintf("%s/api/v2/auth/login", c.BaseURL)
	data := url.Values{}
	data.Set("username", username)
	data.Set("password", password)
	req, err := http.NewRequest("POST", loginURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", c.BaseURL)
	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("登录失败, 状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}
	if strings.TrimSpace(string(body)) != "Ok." {
		return fmt.Errorf("登录失败，响应不为 'Ok.': %s", string(body))
	}
	c.IsLoggedIn = true
	// log.Printf("为 %s 登录成功", c.BaseURL)
	return nil
}
func (c *qbHTTPClient) Get(endpoint string, params url.Values) ([]byte, error) {
	if !c.IsLoggedIn {
		return nil, fmt.Errorf("客户端未登录")
	}
	fullURL := fmt.Sprintf("%s/api/v2/%s", c.BaseURL, endpoint)
	if params != nil {
		fullURL += "?" + params.Encode()
	}
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", c.BaseURL)
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET请求 %s 失败, 状态码: %d", endpoint, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
func formatAndFilterTrackers(originalTrackers []map[string]string) []map[string]string {
	var formatted []map[string]string
	if originalTrackers == nil {
		return formatted
	}
	for _, tracker := range originalTrackers {
		if url, ok := tracker["url"]; ok && (strings.HasPrefix(url, "http") || strings.HasPrefix(url, "udp")) {
			formatted = append(formatted, map[string]string{"url": url})
		}
	}
	return formatted
}
func toNormalizedInfo(t NormalizedTorrent) NormalizedInfo {
	return NormalizedInfo{
		Hash: t.Hash, Name: t.Name, Size: t.Size, Progress: t.Progress, State: t.State,
		SavePath: t.SavePath, Comment: t.Comment, Trackers: formatAndFilterTrackers(t.Trackers),
		Uploaded: t.Uploaded, Downloaded: t.Downloaded, Ratio: t.Ratio, DownloaderID: t.DownloaderID,
	}
}
func formatTrackersForRaw(trackers []FlexibleTracker) []map[string]string {
	var result []map[string]string
	for _, tracker := range trackers {
		result = append(result, map[string]string{
			"url": tracker.URL, "status": fmt.Sprintf("%d", tracker.Status), "msg": tracker.Msg,
			"peers": fmt.Sprintf("%d", tracker.NumPeers), "seeds": fmt.Sprintf("%d", tracker.NumSeeds),
			"leeches": fmt.Sprintf("%d", tracker.NumLeeches),
		})
	}
	return result
}
func writeJSONResponse(w http.ResponseWriter, r *http.Request, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	jsonData, err := json.Marshal(data)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success":false, "message":"Failed to serialize response"}`))
		return
	}
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		w.WriteHeader(statusCode)
		gz.Write(jsonData)
	} else {
		w.WriteHeader(statusCode)
		w.Write(jsonData)
	}
}

// 辅助函数：格式化字节数为可读格式
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// ======================= 核心业务逻辑 (无变动) =======================

// 自定义的种子信息结构，使用正确的类型
type QBTorrentInfo struct {
	Hash       string  `json:"hash"`
	Name       string  `json:"name"`
	Size       int64   `json:"size"`
	Progress   float64 `json:"progress"`
	State      string  `json:"state"`
	SavePath   string  `json:"save_path"`
	Uploaded   int64   `json:"uploaded"`
	Downloaded int64   `json:"downloaded"`
	Ratio      float64 `json:"ratio"`
}

func fetchTorrentsForDownloader(wg *sync.WaitGroup, config DownloaderConfig, includeComment, includeTrackers bool, resultsChan chan<- []NormalizedTorrent, errChan chan<- error) {
	defer wg.Done()
	if config.Type != "qbittorrent" {
		resultsChan <- []NormalizedTorrent{}
		return
	}
	log.Printf("正在为下载器 '%s' 获取种子数据...", config.Host)

	// 使用自定义 HTTP 客户端替代第三方库，避免类型解析问题
	httpClient, err := newQBHTTPClient(config.Host)
	if err != nil {
		errChan <- fmt.Errorf("[%s] 创建HTTP客户端失败: %v", config.Host, err)
		return
	}
	if err := httpClient.Login(config.Username, config.Password); err != nil {
		errChan <- fmt.Errorf("[%s] 登录失败: %v", config.Host, err)
		return
	}

	// 直接调用 API 获取种子列表
	body, err := httpClient.Get("torrents/info", nil)
	if err != nil {
		errChan <- fmt.Errorf("[%s] 获取种子列表失败: %v", config.Host, err)
		return
	}

	// 调试：打印原始 JSON 响应的前 500 个字符
	if len(body) > 500 {
		log.Printf("[DEBUG] [%s] 原始响应前500字符: %s", config.Host, string(body[:500]))
	} else {
		log.Printf("[DEBUG] [%s] 原始响应: %s", config.Host, string(body))
	}

	var torrents []QBTorrentInfo
	if err := json.Unmarshal(body, &torrents); err != nil {
		errChan <- fmt.Errorf("[%s] 解析种子列表JSON失败: %v", config.Host, err)
		return
	}

	// 调试：打印前 3 个种子的详细信息
	for i, t := range torrents {
		if i >= 3 {
			break
		}
		log.Printf("[DEBUG] [%s] 种子[%d]: Name=%s, Size=%d, Progress=%.4f, Uploaded=%d, Downloaded=%d",
			config.Host, i, t.Name, t.Size, t.Progress, t.Uploaded, t.Downloaded)
	}

	normalizedList := make([]NormalizedTorrent, 0, len(torrents))
	var totalUploaded int64 = 0
	var totalDownloaded int64 = 0
	for _, t := range torrents {
		// 计算已下载量：Size * Progress
		downloaded := int64(float64(t.Size) * t.Progress)
		totalUploaded += t.Uploaded
		totalDownloaded += downloaded

		// 计算分享率，避免除零错误
		var ratio float64
		if downloaded > 0 {
			ratio = float64(t.Uploaded) / float64(downloaded)
		} else {
			ratio = 0
		}

		normalizedList = append(normalizedList, NormalizedTorrent{
			Hash: t.Hash, Name: t.Name, Size: t.Size, Progress: t.Progress, State: t.State,
			SavePath: t.SavePath, Uploaded: t.Uploaded, Downloaded: downloaded, Ratio: ratio,
			DownloaderID: config.ID,
		})
	}
	if includeComment || includeTrackers {
		httpClient, err := newQBHTTPClient(config.Host)
		if err != nil {
			errChan <- fmt.Errorf("[%s] 创建HTTP客户端失败: %v", config.Host, err)
			return
		}
		if err := httpClient.Login(config.Username, config.Password); err != nil {
			errChan <- fmt.Errorf("[%s] 自定义HTTP客户端登录失败: %v", config.Host, err)
			return
		}
		for i := range normalizedList {
			torrent := &normalizedList[i]
			params := url.Values{}
			params.Set("hash", torrent.Hash)
			if includeComment {
				body, err := httpClient.Get("torrents/properties", params)
				if err == nil {
					var props struct {
						Comment string `json:"comment"`
					}
					if json.Unmarshal(body, &props) == nil {
						torrent.Comment = props.Comment
					}
				}
			}
			if includeTrackers {
				body, err := httpClient.Get("torrents/trackers", params)
				if err == nil {
					var trackers []FlexibleTracker
					if json.Unmarshal(body, &trackers) == nil {
						torrent.Trackers = formatTrackersForRaw(trackers)
					}
				}
			}
		}
	}
	log.Printf("成功从 '%s' 获取到 %d 个种子", config.Host, len(normalizedList))
	log.Printf("下载器 '%s' 统计: 上传量: %.2f GB, 下载量: %.2f GB", config.Host, float64(totalUploaded)/1024/1024/1024, float64(totalDownloaded)/1024/1024/1024)
	resultsChan <- normalizedList
}
func fetchServerStatsForDownloader(wg *sync.WaitGroup, config DownloaderConfig, resultsChan chan<- ServerStats, errChan chan<- error) {
	defer wg.Done()
	if config.Type != "qbittorrent" {
		resultsChan <- ServerStats{DownloaderID: config.ID}
		return
	}
	// log.Printf("正在为下载器 '%s' 获取统计信息...", config.Host)
	httpClient, err := newQBHTTPClient(config.Host)
	if err != nil {
		errChan <- fmt.Errorf("[%s] 创建HTTP客户端失败: %v", config.Host, err)
		return
	}
	if err := httpClient.Login(config.Username, config.Password); err != nil {
		errChan <- fmt.Errorf("[%s] 自定义HTTP客户端登录失败: %v", config.Host, err)
		return
	}
	body, err := httpClient.Get("sync/maindata", nil)
	if err != nil {
		errChan <- fmt.Errorf("[%s] 获取统计信息失败: %v", config.Host, err)
		return
	}
	var mainData struct {
		ServerState struct {
			DlInfoSpeed int64 `json:"dl_info_speed"`
			UpInfoSpeed int64 `json:"up_info_speed"`
			AlltimeDL   int64 `json:"alltime_dl"`
			AlltimeUL   int64 `json:"alltime_ul"`
		} `json:"server_state"`
	}
	if err := json.Unmarshal(body, &mainData); err != nil {
		errChan <- fmt.Errorf("[%s] 解析统计信息JSON失败: %v", config.Host, err)
		return
	}

	// 获取版本信息
	version := ""
	versionBody, err := httpClient.Get("app/version", nil)
	if err == nil {
		version = strings.TrimSpace(string(versionBody))
	} else {
		log.Printf("警告: 获取 '%s' 版本信息失败: %v", config.Host, err)
	}

	// 检查上传量下载量是否为0，可能是某些版本qb的问题
	if mainData.ServerState.AlltimeUL == 0 && mainData.ServerState.AlltimeDL == 0 {
		log.Printf("警告: 下载器 '%s' 的上传量和下载量都为0，可能是该版本qBittorrent不支持获取这些统计信息", config.Host)
	} else if mainData.ServerState.AlltimeUL == 0 {
		log.Printf("警告: 下载器 '%s' 的上传量为0，可能是该版本qBittorrent不支持获取上传量统计信息", config.Host)
	} else if mainData.ServerState.AlltimeDL == 0 {
		log.Printf("警告: 下载器 '%s' 的下载量为0，可能是该版本qBittorrent不支持获取下载量统计信息", config.Host)
	}

	stats := ServerStats{
		DownloaderID: config.ID, DownloadSpeed: mainData.ServerState.DlInfoSpeed,
		UploadSpeed: mainData.ServerState.UpInfoSpeed, TotalDownload: mainData.ServerState.AlltimeDL,
		TotalUpload: mainData.ServerState.AlltimeUL, Version: version,
	}

	// 显示获取到的上传量和下载量
	// log.Printf("下载器 '%s' 服务器统计: 版本: %s, 总上传量: %.2f GB, 总下载量: %.2f GB, 当前上传速度: %s/s, 当前下载速度: %s/s",
	// 	config.Host, version,
	// 	float64(mainData.ServerState.AlltimeUL)/1024/1024/1024,
	// 	float64(mainData.ServerState.AlltimeDL)/1024/1024/1024,
	// 	formatBytes(mainData.ServerState.UpInfoSpeed),
	// 	formatBytes(mainData.ServerState.DlInfoSpeed))

	resultsChan <- stats
}

// ======================= 媒体处理辅助函数 (有变动) =======================

func executeCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("命令 '%s' 执行失败: %v, 错误输出: %s", name, err, stderr.String())
	}
	return stdout.String(), nil
}
func executeCommandWithTimeout(timeout time.Duration, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// 启动命令
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动命令 '%s' 失败: %v", name, err)
	}

	// 使用channel等待命令完成
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// 等待命令完成或超时
	select {
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("命令 '%s' 执行失败: %v, 错误输出: %s", name, err, stderr.String())
		}
		return stdout.String(), nil
	case <-time.After(timeout):
		// 超时，杀死进程
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("警告: 无法杀死超时的进程 '%s': %v", name, err)
		}
		return "", fmt.Errorf("命令 '%s' 执行超时 (%.0f秒)", name, timeout.Seconds())
	}
}

func executeCommandWithTimeoutAndStderr(timeout time.Duration, name string, args ...string) (string, string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// 启动命令
	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("启动命令 '%s' 失败: %v", name, err)
	}

	// 使用channel等待命令完成
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// 等待命令完成或超时
	select {
	case err := <-done:
		if err != nil {
			return "", stderr.String(), fmt.Errorf("命令 '%s' 执行失败: %v, 错误输出: %s", name, err, stderr.String())
		}
		return stdout.String(), stderr.String(), nil
	case <-time.After(timeout):
		// 超时，杀死进程
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("警告: 无法杀死超时的进程 '%s': %v", name, err)
		}
		return "", "", fmt.Errorf("命令 '%s' 执行超时 (%.0f秒)", name, timeout.Seconds())
	}
}
func buildReadIntervals(duration float64) string {
	probePoints := []float64{0.2, 0.4, 0.6, 0.8}
	probeDuration := 60.0
	var intervals []string
	for _, point := range probePoints {
		startTime := duration * point
		endTime := startTime + probeDuration
		if endTime > duration {
			endTime = duration
		}
		intervals = append(intervals, fmt.Sprintf("%.2f%%%.2f", startTime, endTime))
	}
	intervalArg := strings.Join(intervals, ",")
	log.Printf("   🚀 将只扫描以下时间段来寻找字幕: %s", intervalArg)
	return intervalArg
}
func getVideoDuration(videoPath string) (float64, error) {
	log.Printf("正在获取视频时长: %s", videoPath)
	output, err := executeCommand("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", videoPath)
	if err != nil {
		return 0, fmt.Errorf("ffprobe 获取时长失败: %v", err)
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(output), 64)
	if err != nil {
		return 0, fmt.Errorf("解析视频时长失败: %v", err)
	}
	log.Printf("视频时长: %.2f 秒", duration)
	return duration, nil
}

func findBestChineseSubtitleStream(videoPath string) (int, int, string, error) {
	log.Printf("正在智能分析中文字幕流: %s", filepath.Base(videoPath))
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-select_streams", "s",
		videoPath,
	}
	output, err := executeCommand("ffprobe", args...)
	if err != nil {
		return 0, -1, "", fmt.Errorf("ffprobe 字幕探测失败: %v", err)
	}

	var probeResult struct {
		Streams []struct {
			Index     int    `json:"index"`
			CodecName string `json:"codec_name"`
			Tags      struct {
				Language string `json:"language"`
				Title    string `json:"title"`
			} `json:"tags"`
		} `json:"streams"`
	}

	if err := json.Unmarshal([]byte(output), &probeResult); err != nil {
		return 0, -1, "", fmt.Errorf("解析字幕JSON失败: %v", err)
	}

	if len(probeResult.Streams) == 0 {
		return 0, -1, "", nil
	}

	type candidate struct {
		MpvSid      int
		GlobalIndex int
		Codec       string
		Score       int
		Title       string
		Lang        string
	}

	var candidates []candidate

	for i, stream := range probeResult.Streams {
		mpvSid := i + 1
		score := 0
		lang := strings.ToLower(stream.Tags.Language)
		title := strings.ToLower(stream.Tags.Title)

		if lang == "chi" || lang == "zho" || lang == "zh" {
			score += 10
		}

		if strings.Contains(title, "简") || strings.Contains(title, "chs") || strings.Contains(title, "sc") {
			score += 5
		} else if strings.Contains(title, "繁") || strings.Contains(title, "cht") || strings.Contains(title, "tc") {
			score += 3
		} else if strings.Contains(title, "中") || strings.Contains(title, "chinese") {
			score += 2
		}

		if strings.Contains(title, "双语") {
			score += 1
		}

		if score > 0 {
			candidates = append(candidates, candidate{
				MpvSid:      mpvSid,
				GlobalIndex: stream.Index,
				Codec:       stream.CodecName,
				Score:       score,
				Title:       stream.Tags.Title,
				Lang:        lang,
			})
		}
	}

	if len(candidates) > 0 {
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].Score != candidates[j].Score {
				return candidates[i].Score > candidates[j].Score
			}
			return candidates[i].MpvSid < candidates[j].MpvSid
		})
		best := candidates[0]
		log.Printf("   🎯 自动选中字幕: Track #%d (Global %d) [%s] %s (Score: %d)", best.MpvSid, best.GlobalIndex, best.Lang, best.Title, best.Score)
		return best.MpvSid, best.GlobalIndex, best.Codec, nil
	}

	log.Println("   ℹ️ 未检测到明确的中文字幕。")
	return 0, -1, "", nil
}

func findFirstSubtitleStream(videoPath string) (int, string, error) {
	log.Printf("正在为视频 '%s' 探测字幕流...", filepath.Base(videoPath))
	args := []string{"-v", "quiet", "-print_format", "json", "-show_entries", "stream=index,codec_name,codec_type,disposition", "-select_streams", "s", videoPath}
	output, err := executeCommand("ffprobe", args...)
	if err != nil {
		return -1, "", fmt.Errorf("ffprobe 探测字幕失败: %v", err)
	}
	var probeResult struct {
		Streams []struct {
			Index       int    `json:"index"`
			CodecName   string `json:"codec_name"`
			Disposition struct {
				Comment         int `json:"comment"`
				HearingImpaired int `json:"hearing_impaired"`
				VisualImpaired  int `json:"visual_impaired"`
			} `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal([]byte(output), &probeResult); err != nil {
		log.Printf("警告: 解析 ffprobe 的字幕 JSON 输出失败: %v。将不带字幕截图。", err)
		return -1, "", nil
	}
	if len(probeResult.Streams) == 0 {
		log.Printf("视频中未发现内嵌字幕流。")
		return -1, "", nil
	}
	type SubtitleChoice struct {
		Index     int
		CodecName string
	}
	var bestASS, bestSRT, bestPGS SubtitleChoice
	bestASS.Index, bestSRT.Index, bestPGS.Index = -1, -1, -1
	for _, stream := range probeResult.Streams {
		isNormal := stream.Disposition.Comment == 0 && stream.Disposition.HearingImpaired == 0 && stream.Disposition.VisualImpaired == 0
		if isNormal {
			switch stream.CodecName {
			case "ass":
				if bestASS.Index == -1 {
					bestASS = SubtitleChoice{Index: stream.Index, CodecName: stream.CodecName}
				}
			case "subrip":
				if bestSRT.Index == -1 {
					bestSRT = SubtitleChoice{Index: stream.Index, CodecName: stream.CodecName}
				}
			case "hdmv_pgs_subtitle":
				if bestPGS.Index == -1 {
					bestPGS = SubtitleChoice{Index: stream.Index, CodecName: stream.CodecName}
				}
			}
		}
	}
	if bestASS.Index != -1 {
		log.Printf("   ✅ 找到最优字幕流 (ASS)，流索引: %d, 格式: %s", bestASS.Index, bestASS.CodecName)
		return bestASS.Index, bestASS.CodecName, nil
	}
	if bestSRT.Index != -1 {
		log.Printf("   ✅ 找到可用字幕流 (SRT)，流索引: %d, 格式: %s", bestSRT.Index, bestSRT.CodecName)
		return bestSRT.Index, bestSRT.CodecName, nil
	}
	if bestPGS.Index != -1 {
		log.Printf("   ✅ 找到可用字幕流 (PGS)，流索引: %d, 格式: %s", bestPGS.Index, bestPGS.CodecName)
		return bestPGS.Index, bestPGS.CodecName, nil
	}
	firstStream := probeResult.Streams[0]
	log.Printf("   ⚠️ 未找到任何“正常”字幕流，将使用第一个字幕流 (索引: %d, 格式: %s)", firstStream.Index, firstStream.CodecName)
	return firstStream.Index, firstStream.CodecName, nil
}

func takeScreenshot(videoPath, outputPath string, timePoint float64, subtitleSID int) error {
	log.Printf("正在使用 mpv 截图 (时间点: %.2fs) -> %s", timePoint, outputPath)
	args := []string{
		"--no-audio",
		fmt.Sprintf("--start=%.2f", timePoint),
		"--frames=1",
		"--screenshot-high-bit-depth=yes",
		"--screenshot-png-compression=0",
		"--screenshot-tag-colorspace=yes",
	}
	if subtitleSID > 0 {
		args = append(args, fmt.Sprintf("--sid=%d", subtitleSID), "--sub-visibility=yes")
	} else {
		args = append(args, "--sid=no")
	}
	args = append(args,
		"--sub-font-provider=fontconfig",
		"--sub-font=Noto Sans CJK SC",
		"--sub-font-size=52",
		fmt.Sprintf("--o=%s", outputPath),
		videoPath,
	)
	_, err := executeCommandWithTimeout(600*time.Second, "mpv", args...)
	if err != nil {
		log.Printf("mpv 截图失败，最终执行的命令: mpv %s", strings.Join(args, " "))
		return fmt.Errorf("mpv 截图失败: %v", err)
	}
	log.Printf("   ✅ mpv 原始截图成功 (等待优化) -> %s", outputPath)
	return nil
}

const (
	screenshotPreviewDefaultCount = 12
	screenshotPreviewMinCount     = 5
	screenshotPreviewSelectCount  = 5
)

func normalizeScreenshotPreviewCount(count int) int {
	if count <= 0 {
		return screenshotPreviewDefaultCount
	}
	if count < screenshotPreviewMinCount {
		return screenshotPreviewMinCount
	}
	if count > 20 {
		return 20
	}
	return count
}

func detectVideoHDR(videoPath string) bool {
	output, err := executeCommand("ffprobe", "-v", "error", "-show_streams", "-select_streams", "v:0", videoPath)
	if err != nil {
		return false
	}
	text := strings.ToLower(output)
	return strings.Contains(text, "smpte2084") || strings.Contains(text, "bt2020")
}

func takePreviewScreenshot(videoPath, outputPath string, timePoint float64, isHDR bool) error {
	vfFilter := "scale='min(640,iw)':-2,format=yuv420p"
	if isHDR {
		vfFilter = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,scale='min(640,iw)':-2,format=yuv420p"
	}
	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", timePoint),
		"-i", videoPath,
		"-frames:v", "1",
		"-an",
		"-sn",
		"-vf", vfFilter,
		"-q:v", "14",
		outputPath,
	}
	_, err := executeCommandWithTimeout(180*time.Second, "ffmpeg", args...)
	if err != nil {
		return fmt.Errorf("ffmpeg 预览截图失败: %v", err)
	}
	if stat, statErr := os.Stat(outputPath); statErr != nil || stat.Size() == 0 {
		return fmt.Errorf("预览截图文件未生成")
	}
	return nil
}

func buildSmartScreenshotPointsForPreview(videoPath string, duration float64, count int) []float64 {
	if count <= 0 {
		return nil
	}
	_, subtitleIndex, subtitleCodec, err := findBestChineseSubtitleStream(videoPath)
	if err != nil {
		log.Printf("警告: 预览模式探测中文字幕流失败: %v", err)
	}
	if subtitleIndex < 0 {
		fallbackIndex, fallbackCodec, fallbackErr := findFirstSubtitleStream(videoPath)
		if fallbackErr == nil && fallbackIndex >= 0 {
			subtitleIndex = fallbackIndex
			subtitleCodec = fallbackCodec
		}
	}
	if subtitleIndex < 0 {
		return nil
	}

	var subtitleEvents []SubtitleEvent
	if subtitleCodec == "subrip" || subtitleCodec == "ass" {
		subtitleEvents, err = findSubtitleEvents(videoPath, subtitleIndex, duration)
	} else if subtitleCodec == "hdmv_pgs_subtitle" {
		subtitleEvents, err = findSubtitleEventsForPGS(videoPath, subtitleIndex, duration)
	}
	if err != nil || len(subtitleEvents) == 0 {
		return nil
	}

	goldenStartTime := duration * 0.30
	goldenEndTime := duration * 0.80
	goldenEvents := make([]SubtitleEvent, 0, len(subtitleEvents))
	for _, event := range subtitleEvents {
		if event.StartTime >= goldenStartTime && event.EndTime <= goldenEndTime {
			goldenEvents = append(goldenEvents, event)
		}
	}
	targetEvents := goldenEvents
	if len(targetEvents) < count {
		targetEvents = subtitleEvents
	}
	if len(targetEvents) == 0 {
		return nil
	}
	sort.Slice(targetEvents, func(i, j int) bool {
		return targetEvents[i].StartTime < targetEvents[j].StartTime
	})
	chosenEvents := selectWellDistributedEvents(targetEvents, count)
	points := make([]float64, 0, len(chosenEvents))
	rand.Seed(time.Now().UnixNano())
	for _, event := range chosenEvents {
		durationOfEvent := event.EndTime - event.StartTime
		if durationOfEvent <= 0 {
			continue
		}
		randomOffset := durationOfEvent*0.1 + rand.Float64()*(durationOfEvent*0.8)
		points = append(points, event.StartTime+randomOffset)
	}
	return points
}

func buildUniformPreviewPoints(duration float64, count int) []float64 {
	if duration <= 0 || count <= 0 {
		return nil
	}
	start := duration * 0.12
	end := duration * 0.88
	if end <= start {
		start = duration * 0.10
		end = duration * 0.90
	}
	segment := (end - start) / float64(count)
	if segment <= 0 {
		segment = duration / float64(count+1)
	}
	points := make([]float64, 0, count)
	for i := 0; i < count; i++ {
		point := start + segment*(float64(i)+0.5)
		if point < 1 {
			point = 1
		}
		if point > duration-1 {
			point = math.Max(1, duration-1)
		}
		points = append(points, point)
	}
	return points
}

func mergePreviewPoints(primary, secondary []float64, limit int, duration float64) []float64 {
	points := make([]float64, 0, limit)
	minSpacing := math.Max(12, duration/240)
	appendPoint := func(candidate float64) {
		if candidate <= 0 {
			return
		}
		if candidate > duration-1 {
			candidate = math.Max(1, duration-1)
		}
		for _, existing := range points {
			if math.Abs(existing-candidate) < minSpacing {
				return
			}
		}
		points = append(points, candidate)
	}
	for _, candidate := range primary {
		if len(points) >= limit {
			break
		}
		appendPoint(candidate)
	}
	for _, candidate := range secondary {
		if len(points) >= limit {
			break
		}
		appendPoint(candidate)
	}
	sort.Float64s(points)
	if len(points) > limit {
		return points[:limit]
	}
	return points
}

func sanitizeSelectedScreenshotTimes(selected []float64, duration float64) []float64 {
	result := make([]float64, 0, len(selected))
	for _, item := range selected {
		if item <= 0 || item >= duration {
			continue
		}
		duplicated := false
		for _, existing := range result {
			if math.Abs(existing-item) < 0.8 {
				duplicated = true
				break
			}
		}
		if duplicated {
			continue
		}
		result = append(result, item)
	}
	sort.Float64s(result)
	if len(result) > screenshotPreviewSelectCount {
		result = result[:screenshotPreviewSelectCount]
	}
	return result
}

func formatSecondClockValue(second float64) string {
	total := int(second)
	if total < 0 {
		total = 0
	}
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func markRecommendedPreviewCandidates(candidates []ScreenshotPreviewCandidate, want int) {
	if len(candidates) == 0 || want <= 0 {
		return
	}
	if want > len(candidates) {
		want = len(candidates)
	}
	if want == 1 {
		candidates[len(candidates)/2].Recommended = true
		return
	}
	step := float64(len(candidates)-1) / float64(want-1)
	seen := map[int]struct{}{}
	for i := 0; i < want; i++ {
		idx := int(math.Round(float64(i) * step))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(candidates) {
			idx = len(candidates) - 1
		}
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		candidates[idx].Recommended = true
	}
}

func generatePreviewCandidates(videoPath string, duration float64, previewCount int) ([]ScreenshotPreviewCandidate, error) {
	previewCount = normalizeScreenshotPreviewCount(previewCount)
	smartCount := previewCount
	if smartCount > screenshotPreviewSelectCount {
		smartCount = screenshotPreviewSelectCount
	}
	smartPoints := buildSmartScreenshotPointsForPreview(videoPath, duration, smartCount)
	points := mergePreviewPoints(smartPoints, buildUniformPreviewPoints(duration, previewCount), previewCount, duration)
	if len(points) == 0 {
		return nil, fmt.Errorf("未找到可用的候选截图时间点")
	}
	isHDR := detectVideoHDR(videoPath)
	tempDir, err := os.MkdirTemp("", "screenshots-preview-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	candidates := make([]ScreenshotPreviewCandidate, 0, len(points))
	for i, point := range points {
		filePath := filepath.Join(tempDir, fmt.Sprintf("preview_%02d.jpg", i+1))
		if err := takePreviewScreenshot(videoPath, filePath, point, isHDR); err != nil {
			log.Printf("警告: 生成候选截图失败 point=%.2f err=%v", point, err)
			continue
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			log.Printf("警告: 读取候选截图失败 path=%s err=%v", filePath, err)
			continue
		}
		candidates = append(candidates, ScreenshotPreviewCandidate{
			ID:          fmt.Sprintf("candidate-%02d", len(candidates)+1),
			TimeSeconds: point,
			TimeLabel:   formatSecondClockValue(point),
			PreviewData: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(content),
		})
	}
	if len(candidates) < screenshotPreviewMinCount {
		return nil, fmt.Errorf("可用候选截图不足，仅生成 %d 张", len(candidates))
	}
	return candidates, nil
}

// [核心修改] 增加二次压缩逻辑
func convertPngToOptimizedPng(sourcePath, destPath string) error {
	const maxUploadSize = 10 * 1024 * 1024 // 10 MB

	// 步骤 1: 检测图片是否为 HDR
	checkCmd := exec.Command("ffprobe", "-v", "error", "-show_streams", sourcePath)
	output, err := checkCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffprobe 检测失败: %v", err)
	}
	isHDR := strings.Contains(string(output), "smpte2084") || strings.Contains(string(output), "bt2020")

	// 步骤 2: 构建 FFmpeg 命令 (初次压缩)
	var vfFilter string
	if isHDR {
		log.Printf("   🎨 检测到 HDR 图片，正在应用 zscale 色调映射...")
		vfFilter = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=pc,format=rgb24"
	} else {
		log.Printf("   🎨 检测到 SDR 图片，应用标准 RGB24 转换...")
		vfFilter = "format=rgb24"
	}

	log.Printf("正在优化 PNG (第一轮): %s -> %s (HDR: %v)", sourcePath, destPath, isHDR)
	args := []string{
		"-y", "-v", "error", "-i", sourcePath, "-frames:v", "1",
		"-vf", vfFilter,
		"-compression_level", "4", // 正常压缩级别
		"-pred", "mixed",
		destPath,
	}
	start := time.Now()
	_, stderrStr, err := executeCommandWithTimeoutAndStderr(600*time.Second, "ffmpeg", args...)
	if err != nil {
		return fmt.Errorf("ffmpeg 初次优化失败: %v, 错误: %s", err, stderrStr)
	}

	// 检查文件大小
	destInfo, err := os.Stat(destPath)
	if err != nil {
		return fmt.Errorf("无法获取优化后文件的大小: %v", err)
	}
	initialSize := destInfo.Size()
	log.Printf("   ✅ 第一轮优化完成 (耗时: %.2fs) | 大小: %.2f MB", time.Since(start).Seconds(), float64(initialSize)/1024/1024)

	// [新增逻辑] 如果文件大小超过限制，进行二次强力压缩
	if initialSize > maxUploadSize {
		log.Printf("   ⚠️ 图片大小 (%.2f MB) 超出 10MB 限制，正在进行二次强力压缩...", float64(initialSize)/1024/1024)

		tempRecompressPath := destPath + ".recompressed.png"
		recompressArgs := []string{
			"-y", "-v", "error", "-i", destPath, // 输入是已优化的图片
			"-compression_level", "100", // 使用最大压缩级别
			tempRecompressPath,
		}
		recompressStart := time.Now()
		_, recompressStderrStr, err := executeCommandWithTimeoutAndStderr(600*time.Second, "ffmpeg", recompressArgs...)
		if err != nil {
			return fmt.Errorf("ffmpeg 二次压缩失败: %v, 错误: %s", err, recompressStderrStr)
		}

		// 替换原文件
		if err := os.Rename(tempRecompressPath, destPath); err != nil {
			return fmt.Errorf("替换二次压缩文件失败: %v", err)
		}

		// 报告最终结果
		finalInfo, _ := os.Stat(destPath)
		finalSize := finalInfo.Size()
		log.Printf("   ✅ 二次强力压缩完成 (耗时: %.2fs) | 最终大小: %.2f MB", time.Since(recompressStart).Seconds(), float64(finalSize)/1024/1024)

		if finalSize > maxUploadSize {
			log.Printf("   ‼️ 警告: 即使经过强力压缩，文件大小 (%.2f MB) 仍然可能超过图床限制。", float64(finalSize)/1024/1024)
		}
	}

	return nil
}

func uploadToPixhost(imagePath string) (string, error) {
	const maxRetries = 3
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.Printf("准备上传图片到 Pixhost (第 %d/%d 次尝试): %s", attempt, maxRetries, imagePath)
		file, err := os.Open(imagePath)
		if err != nil {
			return "", err
		}
		defer file.Close()
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("img", filepath.Base(imagePath))
		if err != nil {
			return "", err
		}
		if _, err = io.Copy(part, file); err != nil {
			return "", err
		}
		if err = writer.WriteField("content_type", "0"); err != nil {
			return "", err
		}
		if err = writer.Close(); err != nil {
			return "", err
		}
		req, _ := http.NewRequest("POST", "https://api.pixhost.to/images", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
		client := &http.Client{Timeout: 180 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("上传请求失败: %w", err)
			log.Printf("   -> 第 %d 次尝试失败: %v", attempt, lastErr)
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
			}
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var result struct {
				ShowURL string `json:"show_url"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				lastErr = fmt.Errorf("解析成功响应失败: %w", err)
				log.Printf("   -> 第 %d 次尝试失败: %v", attempt, lastErr)
			} else {
				log.Printf("   ✅ Pixhost 上传成功, URL: %s", result.ShowURL)
				return result.ShowURL, nil
			}
		} else {
			respBody, _ := io.ReadAll(resp.Body)
			lastErr = fmt.Errorf("Pixhost 返回非 200 状态码: %d, 响应: %s", resp.StatusCode, string(respBody))
			log.Printf("   -> 第 %d 次尝试失败: %v", attempt, lastErr)
		}
		if attempt < maxRetries {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
	}
	return "", fmt.Errorf("经过 %d 次尝试后上传失败: %w", maxRetries, lastErr)
}
func findSubtitleEvents(videoPath string, subtitleStreamIndex int, duration float64) ([]SubtitleEvent, error) {
	log.Printf("正在为视频 '%s' (字幕流索引 %d) 智能提取字幕时间点 (快速扫描模式)...", filepath.Base(videoPath), subtitleStreamIndex)
	if subtitleStreamIndex < 0 {
		return nil, fmt.Errorf("无效的字幕流索引")
	}
	readIntervalsArg := buildReadIntervals(duration)
	args := []string{"-v", "quiet", "-read_intervals", readIntervalsArg, "-print_format", "json", "-show_packets", "-select_streams", fmt.Sprintf("%d", subtitleStreamIndex), videoPath}
	output, err := executeCommand("ffprobe", args...)
	if err != nil {
		return nil, fmt.Errorf("ffprobe 提取字幕数据包失败: %v", err)
	}
	jsonStartIndex := strings.Index(output, "{")
	if jsonStartIndex == -1 {
		return nil, fmt.Errorf("ffprobe 输出中未找到有效的JSON内容")
	}
	jsonOutput := output[jsonStartIndex:]
	var probeResult struct {
		Packets []struct {
			PtsTime      string `json:"pts_time"`
			DurationTime string `json:"duration_time"`
		} `json:"packets"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &probeResult); err != nil {
		return nil, fmt.Errorf("解析 ffprobe 的字幕JSON输出失败: %v", err)
	}
	var events []SubtitleEvent
	for _, packet := range probeResult.Packets {
		start, err1 := strconv.ParseFloat(packet.PtsTime, 64)
		duration, err2 := strconv.ParseFloat(packet.DurationTime, 64)
		if err1 == nil && err2 == nil && duration > 0.1 {
			end := start + duration
			events = append(events, SubtitleEvent{StartTime: start, EndTime: end})
		}
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("未能在指定区间内提取到任何有效的时间事件")
	}
	log.Printf("   ✅ 成功从指定区间提取到 %d 条字幕事件。", len(events))
	return events, nil
}
func findSubtitleEventsForPGS(videoPath string, subtitleStreamIndex int, duration float64) ([]SubtitleEvent, error) {
	log.Printf("正在为视频 '%s' (PGS字幕流索引 %d) 智能提取显示时间段 (快速扫描模式)...", filepath.Base(videoPath), subtitleStreamIndex)
	if subtitleStreamIndex < 0 {
		return nil, fmt.Errorf("无效的字幕流索引")
	}
	readIntervalsArg := buildReadIntervals(duration)
	args := []string{"-v", "quiet", "-read_intervals", readIntervalsArg, "-print_format", "json", "-show_packets", "-select_streams", fmt.Sprintf("%d", subtitleStreamIndex), videoPath}
	output, err := executeCommand("ffprobe", args...)
	if err != nil {
		return nil, fmt.Errorf("ffprobe 提取PGS数据包失败: %v", err)
	}
	jsonStartIndex := strings.Index(output, "{")
	if jsonStartIndex == -1 {
		return nil, fmt.Errorf("ffprobe 输出中未找到有效的JSON内容")
	}
	jsonOutput := output[jsonStartIndex:]
	var probeResult struct {
		Packets []struct {
			PtsTime string `json:"pts_time"`
		} `json:"packets"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &probeResult); err != nil {
		return nil, fmt.Errorf("解析 ffprobe 的PGS JSON输出失败: %v", err)
	}
	if len(probeResult.Packets) < 2 {
		return nil, fmt.Errorf("PGS字幕数据包数量过少，无法配对")
	}
	var events []SubtitleEvent
	for i := 0; i < len(probeResult.Packets)-1; i += 2 {
		start, err1 := strconv.ParseFloat(probeResult.Packets[i].PtsTime, 64)
		end, err2 := strconv.ParseFloat(probeResult.Packets[i+1].PtsTime, 64)
		if err1 == nil && err2 == nil && end > start && (end-start) > 0.1 {
			events = append(events, SubtitleEvent{StartTime: start, EndTime: end})
		}
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("未能从PGS字幕流的指定区间中提取到任何有效的显示时间段")
	}
	log.Printf("   ✅ 成功从指定区间提取到 %d 个PGS字幕显示时间段。", len(events))
	return events, nil
}

var seasonEpisodePattern = regexp.MustCompile(`(?i)S\d{1,2}E\d{1,3}`)
var seasonOnlyPattern = regexp.MustCompile(`(?i)S\d{1,2}`)
var multiEpisodePattern = regexp.MustCompile(`(?i)S\d{1,2}E\d{1,3}\s*(?:[-~]\s*(?:S?\d{1,2})?E?\d{1,3}|E\d{1,3})`)

func extractSeasonEpisode(text string) string {
	if text == "" {
		return ""
	}
	if match := seasonEpisodePattern.FindString(text); match != "" {
		return strings.ToUpper(match)
	}
	if match := seasonOnlyPattern.FindString(text); match != "" {
		return strings.ToUpper(match)
	}
	return ""
}

func parseSeasonEpisodeNumbers(seasonEpisode string) (int, int, bool) {
	re := regexp.MustCompile(`(?i)^S(\d{1,2})(?:E(\d{1,3}))?$`)
	match := re.FindStringSubmatch(strings.TrimSpace(seasonEpisode))
	if match == nil {
		return 0, 0, false
	}
	season, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, 0, false
	}
	if match[2] == "" {
		return season, 0, false
	}
	episode, err := strconv.Atoi(match[2])
	if err != nil {
		return 0, 0, false
	}
	return season, episode, true
}

func findTargetVideoFile(path string, contentName string) (string, error) {
	log.Printf("正在路径 '%s' 中查找目标视频文件...", path)
	videoExtensions := map[string]bool{".mkv": true, ".mp4": true, ".ts": true, ".avi": true, ".wmv": true, ".mov": true, ".flv": true, ".m2ts": true}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("提供的路径不存在: %s", path)
	}
	if err != nil {
		return "", fmt.Errorf("无法获取路径信息: %v", err)
	}
	if !info.IsDir() {
		if videoExtensions[strings.ToLower(filepath.Ext(path))] {
			log.Printf("输入路径直接指向文件: %s", path)
			return path, nil
		}
		return "", fmt.Errorf("输入路径是一个文件，但不是支持的视频格式: %s", path)
	}
	type videoFileInfo struct {
		path string
		size int64
	}
	var videoFiles []videoFileInfo
	err = filepath.Walk(path, func(filePath string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			log.Printf("警告: 访问文件 '%s' 时出错: %v", filePath, err)
			return nil
		}
		if fileInfo.IsDir() {
			return nil
		}
		if videoExtensions[strings.ToLower(filepath.Ext(filePath))] {
			videoFiles = append(videoFiles, videoFileInfo{path: filePath, size: fileInfo.Size()})
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("遍历目录失败: %v", err)
	}
	if len(videoFiles) == 0 {
		return "", fmt.Errorf("在目录 '%s' 中未找到任何视频文件", path)
	}

	if len(videoFiles) == 1 {
		log.Printf("✅ 找到唯一视频文件: %s", videoFiles[0].path)
		return videoFiles[0].path, nil
	}

	seasonEpisode := ""
	if contentName != "" {
		seasonEpisode = extractSeasonEpisode(contentName)
	}
	if seasonEpisode == "" {
		seasonEpisode = extractSeasonEpisode(filepath.Base(path))
	}

	if seasonEpisode != "" {
		targetSeason, targetEpisode, hasEpisode := parseSeasonEpisodeNumbers(seasonEpisode)
		if targetSeason > 0 {
			if !hasEpisode {
				targetEpisode = 1
			}
			type episodeCandidate struct {
				episode int
				isMulti bool
				path    string
			}
			var episodeMatches []episodeCandidate
			var seasonCandidates []episodeCandidate
			for _, vf := range videoFiles {
				baseName := filepath.Base(vf.path)
				candidate := extractSeasonEpisode(baseName)
				if candidate == "" {
					continue
				}
				candSeason, candEpisode, candHasEpisode := parseSeasonEpisodeNumbers(candidate)
				if candSeason != targetSeason || !candHasEpisode {
					continue
				}
				isMulti := multiEpisodePattern.MatchString(baseName)
				seasonCandidates = append(seasonCandidates, episodeCandidate{
					episode: candEpisode,
					isMulti: isMulti,
					path:    vf.path,
				})
				if candEpisode == targetEpisode {
					episodeMatches = append(episodeMatches, episodeCandidate{
						episode: candEpisode,
						isMulti: isMulti,
						path:    vf.path,
					})
				}
			}
			if len(episodeMatches) > 0 {
				selected := episodeMatches[0].path
				for _, cand := range episodeMatches {
					if !cand.isMulti {
						if selected == "" || cand.path < selected {
							selected = cand.path
						}
					}
				}
				if selected == "" {
					selected = episodeMatches[0].path
					for _, cand := range episodeMatches {
						if cand.path < selected {
							selected = cand.path
						}
					}
				}
				log.Printf("✅ 根据季集信息选择视频文件: %s", selected)
				return selected, nil
			}
			if len(seasonCandidates) > 0 {
				minEpisode := seasonCandidates[0].episode
				for _, cand := range seasonCandidates {
					if cand.episode < minEpisode {
						minEpisode = cand.episode
					}
				}
				selected := ""
				for _, cand := range seasonCandidates {
					if cand.episode != minEpisode {
						continue
					}
					if !cand.isMulti {
						if selected == "" || cand.path < selected {
							selected = cand.path
						}
					}
				}
				if selected == "" {
					for _, cand := range seasonCandidates {
						if cand.episode == minEpisode {
							if selected == "" || cand.path < selected {
								selected = cand.path
							}
						}
					}
				}
				if selected != "" {
					log.Printf("⚠️ 未找到 S%02dE%02d，选择该季最小集: %s", targetSeason, targetEpisode, selected)
					return selected, nil
				}
			}
		}
	}

	largestFile := ""
	var maxSize int64 = -1
	for _, vf := range videoFiles {
		if vf.size > maxSize {
			maxSize = vf.size
			largestFile = vf.path
		}
	}
	log.Printf("✅ 已选定最大文件 (大小: %.2f GB): %s", float64(maxSize)/1024/1024/1024, largestFile)
	if maxSize < 100*1024*1024 {
		log.Printf("⚠️ 警告: 选中的最大文件小于 100MB，可能不是正片。")
	}
	return largestFile, nil
}
func selectWellDistributedEvents(sortedEvents []SubtitleEvent, numToSelect int) []SubtitleEvent {
	if len(sortedEvents) <= numToSelect {
		return sortedEvents
	}
	n := len(sortedEvents)
	selected := make([]SubtitleEvent, 0, numToSelect)
	if numToSelect == 1 {
		midIndex := n / 2
		selected = append(selected, sortedEvents[midIndex])
	} else if numToSelect <= 3 {
		indices := []int{0, n / 2, n - 1}
		for i := 0; i < numToSelect && i < len(indices); i++ {
			selected = append(selected, sortedEvents[indices[i]])
		}
	} else {
		interval := n / (numToSelect + 1)
		for i := 0; i < numToSelect; i++ {
			index := interval * (i + 1)
			if index >= n {
				index = n - 1
			}
			selected = append(selected, sortedEvents[index])
		}
	}
	filteredSelected := make([]SubtitleEvent, 0, numToSelect)
	minInterval := 30.0
	for _, event := range selected {
		shouldAdd := true
		for _, existing := range filteredSelected {
			if math.Abs(event.StartTime-existing.StartTime) < minInterval {
				shouldAdd = false
				break
			}
		}
		if shouldAdd {
			filteredSelected = append(filteredSelected, event)
		} else {
			for _, altEvent := range sortedEvents {
				alreadySelected := false
				for _, s := range selected {
					if s.StartTime == altEvent.StartTime && s.EndTime == altEvent.EndTime {
						alreadySelected = true
						break
					}
				}
				if alreadySelected {
					continue
				}
				allGood := true
				for _, existing := range filteredSelected {
					if math.Abs(altEvent.StartTime-existing.StartTime) < minInterval {
						allGood = false
						break
					}
				}
				if allGood {
					filteredSelected = append(filteredSelected, altEvent)
					break
				}
			}
		}
	}
	if len(filteredSelected) < numToSelect {
		remaining := make([]SubtitleEvent, 0)
		for _, e := range sortedEvents {
			found := false
			for _, s := range filteredSelected {
				if s.StartTime == e.StartTime && s.EndTime == e.EndTime {
					found = true
					break
				}
			}
			if !found {
				remaining = append(remaining, e)
			}
		}
		needed := numToSelect - len(filteredSelected)
		if len(remaining) > 0 && needed > 0 {
			rand.Shuffle(len(remaining), func(i, j int) {
				remaining[i], remaining[j] = remaining[j], remaining[i]
			})
			for i := 0; i < needed && i < len(remaining); i++ {
				filteredSelected = append(filteredSelected, remaining[i])
			}
		}
	}
	if len(filteredSelected) > numToSelect {
		filteredSelected = filteredSelected[:numToSelect]
	}
	sort.Slice(filteredSelected, func(i, j int) bool {
		return filteredSelected[i].StartTime < filteredSelected[j].StartTime
	})
	return filteredSelected
}

// ======================= HTTP 处理器 (核心修改在这里) =======================

func allTorrentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "message": "仅支持 POST 方法"})
		return
	}
	var req TorrentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, map[string]interface{}{"success": false, "message": "无效的 JSON 请求体: " + err.Error()})
		return
	}
	if len(req.Downloaders) == 0 {
		writeJSONResponse(w, r, http.StatusOK, []NormalizedInfo{})
		return
	}
	var wg sync.WaitGroup
	resultsChan := make(chan []NormalizedTorrent, len(req.Downloaders))
	errChan := make(chan error, len(req.Downloaders))
	for _, config := range req.Downloaders {
		wg.Add(1)
		go fetchTorrentsForDownloader(&wg, config, req.IncludeComment, req.IncludeTrackers, resultsChan, errChan)
	}
	wg.Wait()
	close(resultsChan)
	close(errChan)
	allTorrentsRaw := make([]NormalizedTorrent, 0)
	for result := range resultsChan {
		allTorrentsRaw = append(allTorrentsRaw, result...)
	}
	for err := range errChan {
		log.Printf("错误: %v", err)
	}
	normalizedInfos := make([]NormalizedInfo, 0, len(allTorrentsRaw))
	for _, rawTorrent := range allTorrentsRaw {
		normalizedInfos = append(normalizedInfos, toNormalizedInfo(rawTorrent))
	}
	writeJSONResponse(w, r, http.StatusOK, normalizedInfos)
}
func statsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "message": "仅支持 POST 方法"})
		return
	}
	var configs []DownloaderConfig
	if err := json.NewDecoder(r.Body).Decode(&configs); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, map[string]interface{}{"success": false, "message": "无效的 JSON 请求体: " + err.Error()})
		return
	}
	if len(configs) == 0 {
		writeJSONResponse(w, r, http.StatusOK, []ServerStats{})
		return
	}
	var wg sync.WaitGroup
	resultsChan := make(chan ServerStats, len(configs))
	errChan := make(chan error, len(configs))
	for _, config := range configs {
		wg.Add(1)
		go fetchServerStatsForDownloader(&wg, config, resultsChan, errChan)
	}
	wg.Wait()
	close(resultsChan)
	close(errChan)
	allStats := make([]ServerStats, 0)
	for stats := range resultsChan {
		allStats = append(allStats, stats)
	}
	for err := range errChan {
		log.Printf("错误: %v", err)
	}
	writeJSONResponse(w, r, http.StatusOK, allStats)
}

func uploadLimitBatchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "message": "仅支持 POST 方法"})
		return
	}

	var req UploadLimitBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, map[string]interface{}{"success": false, "message": "无效的 JSON 请求体: " + err.Error()})
		return
	}

	results := make([]UploadLimitResult, 0, len(req.Downloaders))
	for _, dl := range req.Downloaders {
		result := UploadLimitResult{
			DownloaderID: dl.ID,
			Errors:       []string{},
		}

		switch dl.Type {
		case "qbittorrent":
			httpClient, err := newQBHTTPClient(dl.Host)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("qB HTTP客户端创建失败: %v", err))
				results = append(results, result)
				continue
			}
			if err := httpClient.Login(dl.Username, dl.Password); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("qB 登录失败: %v", err))
				results = append(results, result)
				continue
			}

			for _, action := range dl.Actions {
				if len(action.TorrentIDs) == 0 {
					continue
				}

				limitBytes := action.LimitMBps * 1024 * 1024
				if action.LimitMBps > 999 {
					limitBytes = -1
				}

				hashes := strings.Join(action.TorrentIDs, "|")
				setURL := fmt.Sprintf("%s/api/v2/torrents/setUploadLimit", dl.Host)
				data := url.Values{}
				data.Set("hashes", hashes)
				data.Set("limit", strconv.Itoa(limitBytes))

				httpReq, err := http.NewRequest("POST", setURL, strings.NewReader(data.Encode()))
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("qB 构建限速请求失败: %v", err))
					continue
				}
				httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				httpReq.Header.Set("Referer", dl.Host)

				httpResp, err := httpClient.Client.Do(httpReq)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("qB 设置限速失败: %v", err))
					continue
				}
				_ = httpResp.Body.Close()
				if httpResp.StatusCode != http.StatusOK {
					result.Errors = append(result.Errors, fmt.Sprintf("qB 设置限速返回状态码: %d", httpResp.StatusCode))
					continue
				}

				result.AppliedGroups++
				result.AppliedTorrents += len(action.TorrentIDs)
			}

		case "transmission":
			hostValue := dl.Host
			if !strings.HasPrefix(hostValue, "http://") && !strings.HasPrefix(hostValue, "https://") {
				hostValue = "http://" + hostValue
			}
			parsed, err := url.Parse(hostValue)
			if err != nil || parsed.Hostname() == "" {
				result.Errors = append(result.Errors, fmt.Sprintf("TR host 解析失败: %v", err))
				results = append(results, result)
				continue
			}

			rpcURL := fmt.Sprintf("http://%s/transmission/rpc", parsed.Host)
			client := &http.Client{Timeout: 30 * time.Second}

			for _, action := range dl.Actions {
				if len(action.TorrentIDs) == 0 {
					continue
				}

				args := map[string]interface{}{
					"ids": action.TorrentIDs,
				}
				if action.LimitMBps > 999 {
					args["uploadLimited"] = false
				} else {
					args["uploadLimit"] = action.LimitMBps * 1024
					args["uploadLimited"] = true
				}

				payload := map[string]interface{}{
					"method":    "torrent-set",
					"arguments": args,
				}

				body, _ := json.Marshal(payload)
				httpReq, err := http.NewRequest("POST", rpcURL, bytes.NewReader(body))
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("TR 构建请求失败: %v", err))
					continue
				}
				httpReq.Header.Set("Content-Type", "application/json")
				httpReq.SetBasicAuth(dl.Username, dl.Password)

				httpResp, err := client.Do(httpReq)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("TR 设置限速失败: %v", err))
					continue
				}

				if httpResp.StatusCode == http.StatusConflict {
					sessionID := httpResp.Header.Get("X-Transmission-Session-Id")
					_ = httpResp.Body.Close()

					httpReq2, err := http.NewRequest("POST", rpcURL, bytes.NewReader(body))
					if err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("TR 二次构建请求失败: %v", err))
						continue
					}
					httpReq2.Header.Set("Content-Type", "application/json")
					httpReq2.Header.Set("X-Transmission-Session-Id", sessionID)
					httpReq2.SetBasicAuth(dl.Username, dl.Password)

					httpResp, err = client.Do(httpReq2)
					if err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("TR 带会话ID重试失败: %v", err))
						continue
					}
				}

				if httpResp.StatusCode != http.StatusOK {
					result.Errors = append(result.Errors, fmt.Sprintf("TR 设置限速返回状态码: %d", httpResp.StatusCode))
					_ = httpResp.Body.Close()
					continue
				}

				respBody, _ := io.ReadAll(httpResp.Body)
				_ = httpResp.Body.Close()
				var rpcResp map[string]interface{}
				if err := json.Unmarshal(respBody, &rpcResp); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("TR 响应解析失败: %v", err))
					continue
				}
				if res, ok := rpcResp["result"].(string); !ok || res != "success" {
					result.Errors = append(result.Errors, fmt.Sprintf("TR RPC返回失败: %v", rpcResp["result"]))
					continue
				}

				result.AppliedGroups++
				result.AppliedTorrents += len(action.TorrentIDs)
			}

		default:
			result.Errors = append(result.Errors, fmt.Sprintf("不支持的下载器类型: %s", dl.Type))
		}

		results = append(results, result)
	}

	writeJSONResponse(w, r, http.StatusOK, UploadLimitBatchResponse{
		Success: true,
		Results: results,
	})
}

// [核心修改] 改进错误处理，失败时跳过而不是中断
func screenshotHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, ScreenshotResponse{Success: false, Message: "仅支持 POST 方法"})
		return
	}
	var reqData ScreenshotRequest
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, ScreenshotResponse{Success: false, Message: "无效的 JSON 请求体: " + err.Error()})
		return
	}
	initialPath := normalizePath(reqData.RemotePath)
	if initialPath == "" {
		writeJSONResponse(w, r, http.StatusBadRequest, ScreenshotResponse{Success: false, Message: "remote_path 不能为空"})
		return
	}

	statusCode := http.StatusOK
	response := ScreenshotResponse{}
	mode := strings.ToLower(strings.TrimSpace(reqData.Mode))

	err := withMountedISOIfNeeded(initialPath, "截图请求", func(resolvedPath string) error {
		videoPath, err := findTargetVideoFile(resolvedPath, reqData.ContentName)
		if err != nil {
			statusCode = http.StatusBadRequest
			response = ScreenshotResponse{Success: false, Message: err.Error()}
			return err
		}

		duration, err := getVideoDuration(videoPath)
		if err != nil {
			statusCode = http.StatusInternalServerError
			response = ScreenshotResponse{Success: false, Message: "获取视频时长失败: " + err.Error()}
			return err
		}

		if mode == "preview" {
			candidates, previewErr := generatePreviewCandidates(videoPath, duration, reqData.PreviewCount)
			if previewErr != nil {
				statusCode = http.StatusInternalServerError
				response = ScreenshotResponse{Success: false, Message: previewErr.Error()}
				return previewErr
			}
			response = ScreenshotResponse{
				Success:           true,
				Message:           fmt.Sprintf("成功生成 %d 张候选截图", len(candidates)),
				PreviewCandidates: candidates,
			}
			return nil
		}

		if mode == "inspect" {
			_, subtitleIndex, _, subtitleErr := findBestChineseSubtitleStream(videoPath)
			if subtitleErr != nil {
				log.Printf("警告: 探测中文字幕流时发生错误: %v", subtitleErr)
			}
			hasSubtitleStream := subtitleIndex >= 0
			if !hasSubtitleStream {
				fallbackIndex, _, fallbackErr := findFirstSubtitleStream(videoPath)
				if fallbackErr != nil {
					statusCode = http.StatusInternalServerError
					response = ScreenshotResponse{Success: false, Message: fallbackErr.Error()}
					return fallbackErr
				}
				hasSubtitleStream = fallbackIndex >= 0
			}
			response = ScreenshotResponse{
				Success:           true,
				HasSubtitleStream: hasSubtitleStream,
				Message: func() string {
					if hasSubtitleStream {
						return "检测到可用字幕流"
					}
					return "未检测到可用字幕流"
				}(),
			}
			return nil
		}

		chineseSubtitleSID, _, _, subtitleErr := findBestChineseSubtitleStream(videoPath)
		if subtitleErr != nil {
			log.Printf("警告: 探测中文字幕流时发生错误: %v", subtitleErr)
		}
		subtitleSID := 0
		if chineseSubtitleSID > 0 {
			subtitleSID = chineseSubtitleSID
			log.Printf("   ✅ 找到中文字幕，将挂载字幕截图")
		} else {
			log.Printf("   ℹ️ 未检测到明确的中文字幕，将截取无字幕画面。")
		}

		screenshotPoints := make([]float64, 0, 5)
		const numScreenshots = 5
		if mode == "finalize" {
			screenshotPoints = sanitizeSelectedScreenshotTimes(reqData.SelectedTimes, duration)
			if len(screenshotPoints) == 0 {
				statusCode = http.StatusBadRequest
				response = ScreenshotResponse{Success: false, Message: "selected_times 不能为空，且至少需要 1 个有效时间点"}
				return fmt.Errorf("selected_times 不能为空")
			}
		} else {
			screenshotPoints = buildSmartScreenshotPointsForPreview(videoPath, duration, numScreenshots)
			if len(screenshotPoints) < numScreenshots {
				log.Printf("警告: 智能截图不足，回退到按百分比截图。")
				percentages := []float64{0.15, 0.30, 0.50, 0.70, 0.85}
				screenshotPoints = make([]float64, 0, len(percentages))
				for _, p := range percentages {
					screenshotPoints = append(screenshotPoints, duration*p)
				}
			}
		}

		tempDir, err := os.MkdirTemp("", "screenshots-*")
		if err != nil {
			statusCode = http.StatusInternalServerError
			response = ScreenshotResponse{Success: false, Message: "创建临时目录失败: " + err.Error()}
			return err
		}
		defer os.RemoveAll(tempDir)

		uploadedURLs := make([]string, 0, len(screenshotPoints))
		for i, point := range screenshotPoints {
			log.Printf("开始处理第 %d/%d 张截图...", i+1, len(screenshotPoints))
			totalSeconds := int(point)
			hours, minutes, seconds := totalSeconds/3600, (totalSeconds%3600)/60, totalSeconds%60
			timeStr := fmt.Sprintf("%02dh%02dm%02ds", hours, minutes, seconds)
			fileName := fmt.Sprintf("s%d_%s.png", i+1, timeStr)
			intermediatePngPath := filepath.Join(tempDir, "raw_"+fileName)
			finalPngPath := filepath.Join(tempDir, fileName)

			if err := takeScreenshot(videoPath, intermediatePngPath, point, subtitleSID); err != nil {
				log.Printf("错误: 第 %d 张图截图失败: %v。跳过此图。", i+1, err)
				continue
			}
			if err := convertPngToOptimizedPng(intermediatePngPath, finalPngPath); err != nil {
				log.Printf("错误: 第 %d 张图PNG压缩失败: %v。跳过此图。", i+1, err)
				continue
			}

			showURL, err := uploadToPixhost(finalPngPath)
			if err != nil {
				log.Printf("错误: 第 %d 张图上传失败: %v。跳过此图。", i+1, err)
				continue
			}

			directURL := strings.Replace(showURL, "https://pixhost.to/show/", "https://img2.pixhost.to/images/", 1)
			uploadedURLs = append(uploadedURLs, directURL)
			log.Printf("✅ 第 %d/%d 张截图处理成功: %s", i+1, len(screenshotPoints), fileName)
		}

		if len(uploadedURLs) == 0 {
			msg := "所有截图处理均失败，请检查日志获取详细信息。"
			log.Println(msg)
			statusCode = http.StatusInternalServerError
			response = ScreenshotResponse{Success: false, Message: msg}
			return fmt.Errorf("%s", msg)
		}

		var bbcodeBuilder strings.Builder
		for _, url := range uploadedURLs {
			bbcodeBuilder.WriteString(fmt.Sprintf("[img]%s[/img]\n", url))
		}

		successMsg := fmt.Sprintf("成功上传 %d/%d 张截图", len(uploadedURLs), len(screenshotPoints))
		log.Println(successMsg)
		response = ScreenshotResponse{
			Success: true,
			Message: successMsg,
			BBCode:  strings.TrimSpace(bbcodeBuilder.String()),
		}
		return nil
	})
	if err != nil {
		if statusCode == http.StatusOK || response.Success {
			statusCode = http.StatusInternalServerError
			response = ScreenshotResponse{Success: false, Message: err.Error()}
		}
		writeJSONResponse(w, r, statusCode, response)
		return
	}

	writeJSONResponse(w, r, statusCode, response)
}
func mediainfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, MediaInfoResponse{Success: false, Message: "仅支持 POST 方法"})
		return
	}
	var reqData MediaInfoRequest
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, MediaInfoResponse{Success: false, Message: "无效的 JSON 请求体: " + err.Error()})
		return
	}
	initialPath := normalizePath(reqData.RemotePath)
	if initialPath == "" {
		writeJSONResponse(w, r, http.StatusBadRequest, MediaInfoResponse{Success: false, Message: "remote_path 不能为空"})
		return
	}

	log.Printf("MediaInfo请求: 开始处理路径 '%s'", initialPath)

	statusCode := http.StatusOK
	response := MediaInfoResponse{}
	err := withMountedISOIfNeeded(initialPath, "MediaInfo请求", func(resolvedPath string) error {
		log.Printf("MediaInfo请求: 实际处理路径 '%s'", resolvedPath)
		if isBlurayDisc(resolvedPath) {
			log.Printf("MediaInfo请求: 检测到蓝光原盘目录，返回 is_bdmv=true 由控制端决定后续操作: %s", resolvedPath)
			response = MediaInfoResponse{
				Success: true,
				Message: "检测到蓝光原盘",
				IsBDMV:  true,
			}
			return nil
		}

		videoPath, innerErr := findTargetVideoFile(resolvedPath, reqData.ContentName)
		if innerErr != nil {
			log.Printf("MediaInfo请求: 查找视频文件失败: %v", innerErr)
			statusCode = http.StatusBadRequest
			response = MediaInfoResponse{Success: false, Message: innerErr.Error()}
			return innerErr
		}

		log.Printf("正在获取 MediaInfo: %s", videoPath)
		mediaInfoText, innerErr := executeCommandWithTimeout(10*time.Minute, "mediainfo", "--Output=text", videoPath)
		if innerErr != nil {
			log.Printf("MediaInfo请求: mediainfo命令执行失败: %v", innerErr)
			statusCode = http.StatusInternalServerError
			response = MediaInfoResponse{Success: false, Message: "获取 MediaInfo 失败: " + innerErr.Error()}
			return innerErr
		}

		log.Printf("MediaInfo请求: 成功获取MediaInfo，长度: %d 字节", len(mediaInfoText))
		response = MediaInfoResponse{
			Success:   true,
			Message:   "MediaInfo 获取成功",
			MediaInfo: strings.TrimSpace(mediaInfoText),
		}
		return nil
	})
	if err != nil {
		if statusCode == http.StatusOK || response.Success {
			statusCode = http.StatusInternalServerError
			response = MediaInfoResponse{Success: false, Message: err.Error()}
		}
		writeJSONResponse(w, r, statusCode, response)
		return
	}
	writeJSONResponse(w, r, statusCode, response)
}
func fileCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, FileCheckResponse{Success: false, Message: "仅支持 POST 方法"})
		return
	}
	var reqData FileCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, FileCheckResponse{Success: false, Message: "无效的 JSON 请求体: " + err.Error()})
		return
	}
	remotePath := normalizePath(reqData.RemotePath)
	if remotePath == "" {
		writeJSONResponse(w, r, http.StatusBadRequest, FileCheckResponse{Success: false, Message: "remote_path 不能为空"})
		return
	}
	log.Printf("文件检查请求: 正在检查路径 '%s'", remotePath)
	fileInfo, err := os.Stat(remotePath)
	if os.IsNotExist(err) {
		log.Printf("文件检查请求: 路径不存在 '%s'", remotePath)
		writeJSONResponse(w, r, http.StatusOK, FileCheckResponse{
			Success: true,
			Message: "检查完成",
			Exists:  false,
		})
		return
	}
	if err != nil {
		log.Printf("文件检查请求: 访问路径失败 '%s': %v", remotePath, err)
		writeJSONResponse(w, r, http.StatusInternalServerError, FileCheckResponse{
			Success: false,
			Message: fmt.Sprintf("访问路径失败: %v", err),
		})
		return
	}
	isFile := !fileInfo.IsDir()
	size := fileInfo.Size()
	log.Printf("文件检查请求: 路径存在 '%s' (是否文件: %v, 大小: %d 字节)", remotePath, isFile, size)
	writeJSONResponse(w, r, http.StatusOK, FileCheckResponse{
		Success: true,
		Message: "检查完成",
		Exists:  true,
		IsFile:  isFile,
		Size:    size,
	})
}
func batchFileCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, BatchFileCheckResponse{Success: false, Message: "仅支持 POST 方法"})
		return
	}
	var reqData BatchFileCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, BatchFileCheckResponse{Success: false, Message: "无效的 JSON 请求体: " + err.Error()})
		return
	}
	if len(reqData.RemotePaths) == 0 {
		writeJSONResponse(w, r, http.StatusBadRequest, BatchFileCheckResponse{Success: false, Message: "remote_paths 不能为空"})
		return
	}
	log.Printf("批量文件检查请求: 正在检查 %d 个路径", len(reqData.RemotePaths))
	results := make([]FileCheckResult, 0, len(reqData.RemotePaths))
	for _, rawPath := range reqData.RemotePaths {
		remotePath := normalizePath(rawPath)
		result := FileCheckResult{
			Path:   remotePath,
			Exists: false,
			IsFile: false,
			Size:   0,
		}
		fileInfo, err := os.Stat(remotePath)
		if os.IsNotExist(err) {
			results = append(results, result)
			continue
		}
		if err != nil {
			log.Printf("批量文件检查: 访问路径失败 '%s': %v", remotePath, err)
			results = append(results, result)
			continue
		}
		result.Exists = true
		result.IsFile = !fileInfo.IsDir()
		result.Size = fileInfo.Size()
		results = append(results, result)
	}
	log.Printf("批量文件检查请求: 完成检查 %d 个路径，其中 %d 个存在",
		len(reqData.RemotePaths),
		countExisting(results))
	writeJSONResponse(w, r, http.StatusOK, BatchFileCheckResponse{
		Success: true,
		Message: "批量检查完成",
		Results: results,
	})
}
func countExisting(results []FileCheckResult) int {
	count := 0
	for _, r := range results {
		if r.Exists {
			count++
		}
	}
	return count
}

func episodeCountHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, EpisodeCountResponse{Success: false, Message: "仅支持 POST 方法"})
		return
	}
	var reqData EpisodeCountRequest
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, EpisodeCountResponse{Success: false, Message: "无效的 JSON 请求体: " + err.Error()})
		return
	}
	remotePath := normalizePath(reqData.RemotePath)
	if remotePath == "" {
		writeJSONResponse(w, r, http.StatusBadRequest, EpisodeCountResponse{Success: false, Message: "remote_path 不能为空"})
		return
	}
	log.Printf("集数统计请求: 正在统计路径 '%s'", remotePath)
	if _, err := os.Stat(remotePath); os.IsNotExist(err) {
		log.Printf("集数统计请求: 路径不存在 '%s'", remotePath)
		writeJSONResponse(w, r, http.StatusOK, EpisodeCountResponse{
			Success: false,
			Message: "路径不存在",
		})
		return
	}
	videoExtensions := map[string]bool{
		".mkv": true, ".mp4": true, ".ts": true, ".avi": true,
		".wmv": true, ".mov": true, ".flv": true, ".m2ts": true,
	}
	episodePattern := regexp.MustCompile(`[Ss](\d{1,2})[Ee](\d{1,3})`)
	episodeSet := make(map[string]bool)
	seasonNumbers := make(map[int]bool)
	err := filepath.Walk(remotePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if !videoExtensions[ext] {
			return nil
		}
		matches := episodePattern.FindStringSubmatch(info.Name())
		if len(matches) >= 3 {
			season, _ := strconv.Atoi(matches[1])
			episode, _ := strconv.Atoi(matches[2])
			key := fmt.Sprintf("S%dE%d", season, episode)
			episodeSet[key] = true
			seasonNumbers[season] = true
		}
		return nil
	})
	if err != nil {
		log.Printf("集数统计请求: 遍历目录失败 '%s': %v", remotePath, err)
		writeJSONResponse(w, r, http.StatusInternalServerError, EpisodeCountResponse{
			Success: false,
			Message: fmt.Sprintf("遍历目录失败: %v", err),
		})
		return
	}
	if len(episodeSet) == 0 {
		log.Printf("集数统计请求: 未找到剧集文件 '%s'", remotePath)
		writeJSONResponse(w, r, http.StatusOK, EpisodeCountResponse{
			Success:      true,
			Message:      "未找到剧集文件",
			EpisodeCount: 0,
		})
		return
	}
	mainSeason := 1
	if len(seasonNumbers) > 0 {
		for season := range seasonNumbers {
			if season < mainSeason || mainSeason == 0 {
				mainSeason = season
			}
		}
	}
	seasonEpisodeCount := 0
	for key := range episodeSet {
		if strings.HasPrefix(key, fmt.Sprintf("S%d", mainSeason)) {
			seasonEpisodeCount++
		}
	}
	log.Printf("集数统计请求: 路径 '%s' 找到第%d季共 %d 集", remotePath, mainSeason, seasonEpisodeCount)
	writeJSONResponse(w, r, http.StatusOK, EpisodeCountResponse{
		Success:      true,
		Message:      "统计完成",
		EpisodeCount: seasonEpisodeCount,
		SeasonNumber: mainSeason,
	})
}

// ======================= 主函数 (无变动) =======================

func main() {
	port := "9090"
	if len(os.Args) > 1 {
		port = os.Args[1]
		if !strings.HasPrefix(port, ":") {
			port = ":" + port
		}
	} else {
		port = ":9090"
	}
	http.HandleFunc("/api/torrents/all", allTorrentsHandler)
	http.HandleFunc("/api/stats/server", statsHandler)
	http.HandleFunc("/api/torrents/upload-limit/batch", uploadLimitBatchHandler)
	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, r, http.StatusOK, map[string]string{"status": "ok", "message": "qBittorrent代理服务运行正常"})
	})
	http.HandleFunc("/api/media/screenshot", screenshotHandler)
	http.HandleFunc("/api/media/mediainfo", mediainfoHandler)
	RegisterBDInfoRoutes()
	http.HandleFunc("/api/file/check", fileCheckHandler)
	http.HandleFunc("/api/file/batch-check", batchFileCheckHandler)
	http.HandleFunc("/api/media/episode-count", episodeCountHandler)
	log.Println("aa增强版qBittorrent代理服务器正在启动...")
	log.Println("API端点:")
	log.Println("  POST /api/torrents/all - 获取种子信息")
	log.Println("  POST /api/stats/server - 获取服务器统计")
	log.Println("  POST /api/torrents/upload-limit/batch - 批量设置种子上传限速")
	log.Println("  GET  /api/health      - 健康检查")
	log.Println("  POST /api/media/screenshot - 远程截图并上传图床")
	log.Println("  POST /api/media/mediainfo  - 远程获取MediaInfo")
	log.Println("  POST /api/media/bdinfo    - 远程获取BDInfo (由 bdinfo 处理)")
	log.Println("  POST /api/file/check       - 远程文件存在性检查")
	log.Println("  POST /api/file/batch-check - 批量远程文件存在性检查")
	log.Println("  POST /api/media/episode-count - 远程目录集数统计")
	log.Printf("监听端口: %s", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("启动服务器失败: %v", err)
	}
}
