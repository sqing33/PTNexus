package downloaderclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type proxyDeleteTorrentsRequest struct {
	Downloader  proxyDownloaderConfig `json:"downloader"`
	Hashes      []string              `json:"hashes"`
	DeleteFiles bool                  `json:"delete_files"`
}

type proxyDownloaderConfig struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type proxyDeleteTorrentsResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// deleteQBTorrentsByProxy 通过盒子代理删除远程 qBittorrent 任务。
// 参数/返回：hashes 为 qBittorrent hash，deleteFiles 控制是否同步删除本地文件；成功返回 nil。
// 失败场景：代理地址解析失败、代理不可达、下载器认证失败或 qBittorrent 删除接口失败时返回 error。
// 副作用：代理会在远程下载器中删除任务，并按 deleteFiles 决定是否删除任务文件。
func (d Downloader) deleteQBTorrentsByProxy(hashes []string, deleteFiles bool) error {
	proxyPort := d.ProxyPort
	if proxyPort <= 0 {
		proxyPort = 9090
	}

	parsedURL, err := parseHostURL(d.Host)
	if err != nil {
		return fmt.Errorf("解析 host 失败: %w", err)
	}
	proxyIP := strings.TrimSpace(parsedURL.Hostname())
	if proxyIP == "" {
		proxyIP = extractHostFallback(d.Host)
	}
	if proxyIP == "" {
		return fmt.Errorf("无法解析代理地址: host=%s", strings.TrimSpace(d.Host))
	}
	originPort := strings.TrimSpace(parsedURL.Port())
	if originPort == "" {
		originPort = "8080"
	}

	payload := proxyDeleteTorrentsRequest{
		Downloader: proxyDownloaderConfig{
			ID:       d.ID,
			Type:     d.Type,
			Host:     "http://127.0.0.1:" + originPort,
			Username: d.Username,
			Password: d.Password,
		},
		Hashes:      hashes,
		DeleteFiles: deleteFiles,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("构造代理删除请求失败: %w", err)
	}

	proxyURL := fmt.Sprintf("http://%s:%d/api/torrents/delete", proxyIP, proxyPort)
	request, err := http.NewRequest(http.MethodPost, proxyURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建代理删除请求失败: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 90 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("代理删除请求失败: %w", err)
	}
	defer response.Body.Close()

	bodyBytes, _ := io.ReadAll(response.Body)
	bodyText := strings.TrimSpace(string(bodyBytes))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message := compactProxyBody(bodyText)
		if message == "" {
			message = response.Status
		}
		return fmt.Errorf("代理删除失败: HTTP %d %s", response.StatusCode, message)
	}
	if bodyText == "" || bodyText == "0" || strings.EqualFold(bodyText, "ok") || strings.EqualFold(bodyText, "true") {
		return nil
	}
	responseBody := proxyDeleteTorrentsResponse{}
	if err := json.Unmarshal(bodyBytes, &responseBody); err != nil {
		return fmt.Errorf("代理删除响应格式异常，请更新并重启盒子代理: %s", compactProxyBody(bodyText))
	}
	if !responseBody.Success {
		message := strings.TrimSpace(responseBody.Message)
		if message == "" {
			message = "代理返回 success=false"
		}
		return fmt.Errorf("代理删除失败: %s", message)
	}
	return nil
}
