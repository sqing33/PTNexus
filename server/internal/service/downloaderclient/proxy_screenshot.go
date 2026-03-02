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

const proxyScreenshotLogModule = "盒子代理-截图"

type proxyScreenshotRequest struct {
	RemotePath  string `json:"remote_path"`
	ContentName string `json:"content_name,omitempty"`
}

type proxyScreenshotResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	BBCode  string `json:"bbcode,omitempty"`
}

// FetchScreenshotsByProxy 通过盒子代理远程截图并上传图床，返回截图 BBCode 文本。
// 参数/返回：remotePath 为盒子上的实际路径（通常是下载器返回的 save_path 或其子目录）；contentName 用于多文件时辅助选取目标视频；返回截图 BBCode。
// 失败场景：代理不可达、HTTP 返回异常、代理返回 success=false、响应解析失败、BBCode 为空。
// 副作用：会向盒子代理服务发起 HTTP 请求。
func (d Downloader) FetchScreenshotsByProxy(remotePath, contentName string) (string, error) {
	trimmedPath := strings.TrimSpace(remotePath)
	if trimmedPath == "" {
		return "", &ProxyAPIError{StatusCode: 400, Message: "remote_path 不能为空"}
	}
	logx.Infof(proxyScreenshotLogModule, "开始请求截图 remote_path=%s content_name_len=%d", compactProxyBody(trimmedPath), len([]rune(strings.TrimSpace(contentName))))

	proxyPort := d.ProxyPort
	if proxyPort <= 0 {
		proxyPort = 9090
	}

	parsedURL, err := parseHostURL(d.Host)
	if err != nil {
		return "", &ProxyAPIError{StatusCode: 500, Message: "解析 host 失败: " + err.Error()}
	}
	proxyIP := strings.TrimSpace(parsedURL.Hostname())
	if proxyIP == "" {
		proxyIP = extractHostFallback(d.Host)
	}
	if proxyIP == "" {
		return "", &ProxyAPIError{StatusCode: 500, Message: "无法解析代理地址: host=" + strings.TrimSpace(d.Host)}
	}

	requestPayload := proxyScreenshotRequest{
		RemotePath:  trimmedPath,
		ContentName: strings.TrimSpace(contentName),
	}
	payloadBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return "", &ProxyAPIError{StatusCode: 500, Message: "构造请求失败: " + err.Error()}
	}

	proxyURL := fmt.Sprintf("http://%s:%d/api/media/screenshot", proxyIP, proxyPort)
	request, err := http.NewRequest(http.MethodPost, proxyURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return "", &ProxyAPIError{StatusCode: 500, Message: "创建请求失败: " + err.Error()}
	}
	request.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		logx.Warnf(proxyScreenshotLogModule, "请求截图失败 remote_path=%s err=%v", compactProxyBody(trimmedPath), err)
		return "", &ProxyAPIError{StatusCode: 500, Message: "代理请求失败: " + err.Error()}
	}
	defer response.Body.Close()

	bodyBytes, _ := io.ReadAll(response.Body)
	bodyText := strings.TrimSpace(string(bodyBytes))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		logx.Warnf(proxyScreenshotLogModule, "请求截图失败 remote_path=%s status=%d body=%s", compactProxyBody(trimmedPath), response.StatusCode, compactProxyBody(bodyText))
		return "", &ProxyAPIError{StatusCode: response.StatusCode, Message: compactProxyBody(bodyText)}
	}

	resp := proxyScreenshotResponse{}
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		logx.Warnf(proxyScreenshotLogModule, "解析截图响应失败 remote_path=%s err=%v", compactProxyBody(trimmedPath), err)
		return "", &ProxyAPIError{StatusCode: 500, Message: "解析代理响应失败: " + err.Error()}
	}
	if !resp.Success {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = "代理返回 success=false"
		}
		logx.Warnf(proxyScreenshotLogModule, "请求截图失败 remote_path=%s reason=%s", compactProxyBody(trimmedPath), compactProxyBody(msg))
		return "", &ProxyAPIError{StatusCode: 500, Message: msg}
	}

	bbcode := strings.TrimSpace(resp.BBCode)
	if bbcode == "" {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = "代理未返回有效 bbcode"
		}
		logx.Warnf(proxyScreenshotLogModule, "截图BBCode为空 remote_path=%s reason=%s", compactProxyBody(trimmedPath), compactProxyBody(msg))
		return "", &ProxyAPIError{StatusCode: 500, Message: msg}
	}

	logx.Infof(proxyScreenshotLogModule, "请求截图成功 remote_path=%s output_len=%d", compactProxyBody(trimmedPath), len([]rune(bbcode)))
	return bbcode, nil
}
