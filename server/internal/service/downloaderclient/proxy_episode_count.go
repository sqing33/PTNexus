package downloaderclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
)

const proxyEpisodeCountLogModule = "盒子代理-集数统计"

type proxyEpisodeCountRequest struct {
	RemotePath string `json:"remote_path"`
}

type proxyEpisodeCountResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	EpisodeCount int    `json:"episode_count,omitempty"`
	SeasonNumber int    `json:"season_number,omitempty"`
}

// FetchEpisodeCountByProxy 通过盒子代理统计目录中的剧集数量（SxxExx）。
// 参数/返回：remotePath 为盒子上的实际路径，成功返回集数与季号。
// 失败场景：代理不可达、HTTP 返回异常、代理返回 success=false、响应解析失败时返回 error。
// 副作用：会向盒子代理服务发起 HTTP 请求。
func (d Downloader) FetchEpisodeCountByProxy(remotePath string) (int, int, error) {
	trimmedPath := strings.TrimSpace(remotePath)
	if trimmedPath == "" {
		return 0, 0, &ProxyAPIError{StatusCode: 400, Message: "remote_path 不能为空"}
	}
	logx.Infof(proxyEpisodeCountLogModule, "开始请求集数统计 remote_path=%s", compactProxyBody(trimmedPath))

	proxyPort := d.ProxyPort
	if proxyPort <= 0 {
		proxyPort = 9090
	}

	parsedURL, err := parseHostURL(d.Host)
	if err != nil {
		return 0, 0, &ProxyAPIError{StatusCode: 500, Message: "解析 host 失败: " + err.Error()}
	}
	proxyIP := strings.TrimSpace(parsedURL.Hostname())
	if proxyIP == "" {
		proxyIP = extractHostFallback(d.Host)
	}
	if proxyIP == "" {
		return 0, 0, &ProxyAPIError{StatusCode: 500, Message: "无法解析代理地址: host=" + strings.TrimSpace(d.Host)}
	}

	payload, err := json.Marshal(proxyEpisodeCountRequest{RemotePath: trimmedPath})
	if err != nil {
		return 0, 0, &ProxyAPIError{StatusCode: 500, Message: "构造请求失败: " + err.Error()}
	}

	proxyURL := fmt.Sprintf("http://%s:%d/api/media/episode-count", proxyIP, proxyPort)
	req, err := http.NewRequest(http.MethodPost, proxyURL, bytes.NewReader(payload))
	if err != nil {
		return 0, 0, &ProxyAPIError{StatusCode: 500, Message: "创建请求失败: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		logx.Warnf(proxyEpisodeCountLogModule, "请求集数统计失败 remote_path=%s err=%v", compactProxyBody(trimmedPath), err)
		return 0, 0, &ProxyAPIError{StatusCode: 500, Message: "代理请求失败: " + err.Error()}
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyText := strings.TrimSpace(string(bodyBytes))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		logx.Warnf(proxyEpisodeCountLogModule, "请求集数统计失败 remote_path=%s status=%d body=%s", compactProxyBody(trimmedPath), resp.StatusCode, compactProxyBody(bodyText))
		return 0, 0, &ProxyAPIError{StatusCode: resp.StatusCode, Message: compactProxyBody(bodyText)}
	}

	parsed := proxyEpisodeCountResponse{}
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		logx.Warnf(proxyEpisodeCountLogModule, "解析集数统计响应失败 remote_path=%s err=%v", compactProxyBody(trimmedPath), err)
		return 0, 0, &ProxyAPIError{StatusCode: 500, Message: "解析代理响应失败: " + err.Error()}
	}
	if !parsed.Success {
		msg := strings.TrimSpace(parsed.Message)
		if msg == "" {
			msg = "代理返回 success=false"
		}
		statusCode := 500
		if strings.Contains(msg, "路径不存在") || strings.Contains(msg, "remote_path 不能为空") {
			statusCode = 400
		}
		logx.Warnf(proxyEpisodeCountLogModule, "请求集数统计失败 remote_path=%s reason=%s", compactProxyBody(trimmedPath), compactProxyBody(msg))
		return 0, 0, &ProxyAPIError{StatusCode: statusCode, Message: msg}
	}

	episodeCount := parsed.EpisodeCount
	if episodeCount < 0 {
		episodeCount = 0
	}
	seasonNumber := parsed.SeasonNumber
	if seasonNumber <= 0 {
		seasonNumber = 1
	}

	logx.Infof(proxyEpisodeCountLogModule, "请求集数统计成功 remote_path=%s season=%d episode_count=%d", compactProxyBody(trimmedPath), seasonNumber, episodeCount)
	return episodeCount, seasonNumber, nil
}
