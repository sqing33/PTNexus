package downloaderclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pt-nexus/server-go/internal/platform/logx"
)

const proxyMediaLogModule = "盒子代理-媒体信息"

// ProxyAPIError 描述盒子代理 API 返回的 HTTP 错误。
// 参数/返回：StatusCode 为 HTTP 状态码，Message 为可展示的错误文本。
// 失败场景：无。
// 副作用：无。
type ProxyAPIError struct {
	StatusCode int
	Message    string
}

func (e *ProxyAPIError) Error() string {
	if e == nil {
		return ""
	}
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = "未知错误"
	}
	code := e.StatusCode
	if code <= 0 {
		code = 500
	}
	return fmt.Sprintf("盒子代理请求失败: HTTP %d %s", code, msg)
}

type proxyMediaInfoRequest struct {
	RemotePath  string `json:"remote_path"`
	ContentName string `json:"content_name,omitempty"`
}

type proxyMediaInfoResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	MediaInfo string `json:"mediainfo,omitempty"`
	IsBDMV    bool   `json:"is_bdmv,omitempty"`
}

// FetchMediaInfoByProxy 通过盒子代理远程提取媒体信息文本（MediaInfo），用于本机无法直接访问视频文件的场景。
// 参数/返回：remotePath 为盒子上的实际路径（通常是下载器返回的 save_path）；contentName 用于多文件时辅助选取目标视频；返回 mediainfo 文本与是否为蓝光原盘目录标记。
// 失败场景：代理不可达、HTTP 返回异常、代理返回 success=false、响应解析失败。
// 副作用：会向盒子代理服务发起 HTTP 请求。
func (d Downloader) FetchMediaInfoByProxy(remotePath, contentName string) (string, bool, error) {
	trimmedPath := strings.TrimSpace(remotePath)
	if trimmedPath == "" {
		return "", false, &ProxyAPIError{StatusCode: 400, Message: "remote_path 不能为空"}
	}
	logx.Infof(proxyMediaLogModule, "开始请求MediaInfo remote_path=%s content_name_len=%d", compactProxyBody(trimmedPath), len([]rune(strings.TrimSpace(contentName))))

	proxyPort := d.ProxyPort
	if proxyPort <= 0 {
		proxyPort = 9090
	}

	parsedURL, err := parseHostURL(d.Host)
	if err != nil {
		return "", false, &ProxyAPIError{StatusCode: 500, Message: "解析 host 失败: " + err.Error()}
	}
	proxyIP := strings.TrimSpace(parsedURL.Hostname())
	if proxyIP == "" {
		proxyIP = extractHostFallback(d.Host)
	}
	if proxyIP == "" {
		return "", false, &ProxyAPIError{StatusCode: 500, Message: "无法解析代理地址: host=" + strings.TrimSpace(d.Host)}
	}

	requestPayload := proxyMediaInfoRequest{
		RemotePath:  trimmedPath,
		ContentName: strings.TrimSpace(contentName),
	}
	payloadBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return "", false, &ProxyAPIError{StatusCode: 500, Message: "构造请求失败: " + err.Error()}
	}

	proxyURL := fmt.Sprintf("http://%s:%d/api/media/mediainfo", proxyIP, proxyPort)
	request, err := http.NewRequest(http.MethodPost, proxyURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return "", false, &ProxyAPIError{StatusCode: 500, Message: "创建请求失败: " + err.Error()}
	}
	request.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		logx.Warnf(proxyMediaLogModule, "请求MediaInfo失败 remote_path=%s err=%v", compactProxyBody(trimmedPath), err)
		return "", false, &ProxyAPIError{StatusCode: 500, Message: "代理请求失败: " + err.Error()}
	}
	defer response.Body.Close()

	bodyBytes, _ := io.ReadAll(response.Body)
	bodyText := strings.TrimSpace(string(bodyBytes))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		logx.Warnf(proxyMediaLogModule, "请求MediaInfo失败 remote_path=%s status=%d body=%s", compactProxyBody(trimmedPath), response.StatusCode, compactProxyBody(bodyText))
		return "", false, &ProxyAPIError{StatusCode: response.StatusCode, Message: compactProxyBody(bodyText)}
	}

	resp := proxyMediaInfoResponse{}
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		logx.Warnf(proxyMediaLogModule, "解析MediaInfo响应失败 remote_path=%s err=%v", compactProxyBody(trimmedPath), err)
		return "", false, &ProxyAPIError{StatusCode: 500, Message: "解析代理响应失败: " + err.Error()}
	}
	if !resp.Success {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = "代理返回 success=false"
		}
		logx.Warnf(proxyMediaLogModule, "请求MediaInfo失败 remote_path=%s reason=%s", compactProxyBody(trimmedPath), compactProxyBody(msg))
		return "", false, &ProxyAPIError{StatusCode: 500, Message: msg}
	}

	if resp.IsBDMV {
		logx.Warnf(proxyMediaLogModule, "MediaInfo提示蓝光目录 remote_path=%s", compactProxyBody(trimmedPath))
		return "", true, nil
	}

	mediaInfo := strings.TrimSpace(resp.MediaInfo)
	if mediaInfo == "" {
		// 代理成功但没有产出文本，按失败处理，避免上层误判 completed。
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = "代理未返回有效 mediainfo 文本"
		}
		logx.Warnf(proxyMediaLogModule, "MediaInfo为空 remote_path=%s reason=%s", compactProxyBody(trimmedPath), compactProxyBody(msg))
		return "", false, &ProxyAPIError{StatusCode: 500, Message: msg}
	}

	logx.Infof(proxyMediaLogModule, "请求MediaInfo成功 remote_path=%s output_bytes=%d", compactProxyBody(trimmedPath), len(mediaInfo))
	return mediaInfo, false, nil
}

func compactProxyBody(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	const limit = 300
	if len([]rune(trimmed)) <= limit {
		return trimmed
	}
	return string([]rune(trimmed)[:limit]) + "..."
}
