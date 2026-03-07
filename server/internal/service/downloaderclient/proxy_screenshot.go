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
	RemotePath    string    `json:"remote_path"`
	ContentName   string    `json:"content_name,omitempty"`
	Mode          string    `json:"mode,omitempty"`
	PreviewCount  int       `json:"preview_count,omitempty"`
	SelectedTimes []float64 `json:"selected_times,omitempty"`
}

// ProxyScreenshotPreviewCandidate 描述盒子代理返回的低清截图候选。
type ProxyScreenshotPreviewCandidate struct {
	ID          string  `json:"id"`
	TimeSeconds float64 `json:"time_seconds"`
	TimeLabel   string  `json:"time_label"`
	PreviewData string  `json:"preview_data"`
	Recommended bool    `json:"recommended"`
}

type proxyScreenshotResponse struct {
	Success           bool                              `json:"success"`
	Message           string                            `json:"message"`
	BBCode            string                            `json:"bbcode,omitempty"`
	HasSubtitleStream bool                              `json:"has_subtitle_stream,omitempty"`
	PreviewCandidates []ProxyScreenshotPreviewCandidate `json:"preview_candidates,omitempty"`
}

// FetchScreenshotsByProxy 通过盒子代理远程截图并上传图床，返回截图 BBCode 文本。
// 参数/返回：remotePath 为盒子上的实际路径（通常是下载器返回的 save_path 或其子目录）；contentName 用于多文件时辅助选取目标视频；返回截图 BBCode。
// 失败场景：代理不可达、HTTP 返回异常、代理返回 success=false、响应解析失败、BBCode 为空。
// 副作用：会向盒子代理服务发起 HTTP 请求。
func (d Downloader) FetchScreenshotsByProxy(remotePath, contentName string) (string, error) {
	resp, err := d.requestProxyScreenshots(remotePath, contentName, "", 0, nil)
	if err != nil {
		return "", err
	}
	bbcode := strings.TrimSpace(resp.BBCode)
	if bbcode == "" {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = "代理未返回有效 bbcode"
		}
		logx.Warnf(proxyScreenshotLogModule, "截图BBCode为空 remote_path=%s reason=%s", compactProxyBody(strings.TrimSpace(remotePath)), compactProxyBody(msg))
		return "", &ProxyAPIError{StatusCode: 500, Message: msg}
	}
	return bbcode, nil
}

// FetchSelectedScreenshotsByProxy 按用户选择的时间点通过盒子代理生成正式截图并上传。
// 参数/返回：selectedTimes 为前端选中的时间点（秒）；返回正式截图 BBCode。
// 失败场景：时间点为空、代理不可达、代理未返回有效截图。
// 副作用：会向盒子代理服务发起 HTTP 请求。
func (d Downloader) FetchSelectedScreenshotsByProxy(remotePath, contentName string, selectedTimes []float64) (string, error) {
	resp, err := d.requestProxyScreenshots(remotePath, contentName, "finalize", 0, selectedTimes)
	if err != nil {
		return "", err
	}
	bbcode := strings.TrimSpace(resp.BBCode)
	if bbcode == "" {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = "代理未返回有效 bbcode"
		}
		return "", &ProxyAPIError{StatusCode: 500, Message: msg}
	}
	return bbcode, nil
}

// HasUsableSubtitleStreamByProxy 通过盒子代理探测目标视频是否存在可用字幕流。
// 参数/返回：返回 true 表示存在可用于原始截图流程的字幕流；false 表示应走候选截图选择。
// 失败场景：代理不可达、路径不存在、响应解析失败时返回错误。
// 副作用：会向盒子代理服务发起 HTTP 请求。
func (d Downloader) HasUsableSubtitleStreamByProxy(remotePath, contentName string) (bool, error) {
	resp, err := d.requestProxyScreenshots(remotePath, contentName, "inspect", 0, nil)
	if err != nil {
		return false, err
	}
	return resp.HasSubtitleStream, nil
}

// FetchScreenshotPreviewsByProxy 通过盒子代理生成低清截图候选，供前端人工挑选。
// 参数/返回：previewCount 为候选数量；返回带 data URI 的候选截图列表。
// 失败场景：代理不可达、HTTP 返回异常、代理返回 success=false、未返回候选。
// 副作用：会向盒子代理服务发起 HTTP 请求。
func (d Downloader) FetchScreenshotPreviewsByProxy(remotePath, contentName string, previewCount int) ([]ProxyScreenshotPreviewCandidate, error) {
	resp, err := d.requestProxyScreenshots(remotePath, contentName, "preview", previewCount, nil)
	if err != nil {
		return nil, err
	}
	if len(resp.PreviewCandidates) == 0 {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = "代理未返回可用候选截图"
		}
		return nil, &ProxyAPIError{StatusCode: 500, Message: msg}
	}
	return resp.PreviewCandidates, nil
}

func (d Downloader) requestProxyScreenshots(remotePath, contentName, mode string, previewCount int, selectedTimes []float64) (proxyScreenshotResponse, error) {
	trimmedPath := strings.TrimSpace(remotePath)
	if trimmedPath == "" {
		return proxyScreenshotResponse{}, &ProxyAPIError{StatusCode: 400, Message: "remote_path 不能为空"}
	}
	logx.Infof(proxyScreenshotLogModule, "开始请求截图 remote_path=%s mode=%s preview_count=%d selected=%d", compactProxyBody(trimmedPath), strings.TrimSpace(mode), previewCount, len(selectedTimes))

	proxyPort := d.ProxyPort
	if proxyPort <= 0 {
		proxyPort = 9090
	}

	parsedURL, err := parseHostURL(d.Host)
	if err != nil {
		return proxyScreenshotResponse{}, &ProxyAPIError{StatusCode: 500, Message: "解析 host 失败: " + err.Error()}
	}
	proxyIP := strings.TrimSpace(parsedURL.Hostname())
	if proxyIP == "" {
		proxyIP = extractHostFallback(d.Host)
	}
	if proxyIP == "" {
		return proxyScreenshotResponse{}, &ProxyAPIError{StatusCode: 500, Message: "无法解析代理地址: host=" + strings.TrimSpace(d.Host)}
	}

	requestPayload := proxyScreenshotRequest{
		RemotePath:    trimmedPath,
		ContentName:   strings.TrimSpace(contentName),
		Mode:          strings.TrimSpace(mode),
		PreviewCount:  previewCount,
		SelectedTimes: selectedTimes,
	}
	payloadBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return proxyScreenshotResponse{}, &ProxyAPIError{StatusCode: 500, Message: "构造请求失败: " + err.Error()}
	}

	proxyURL := fmt.Sprintf("http://%s:%d/api/media/screenshot", proxyIP, proxyPort)
	request, err := http.NewRequest(http.MethodPost, proxyURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return proxyScreenshotResponse{}, &ProxyAPIError{StatusCode: 500, Message: "创建请求失败: " + err.Error()}
	}
	request.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		logx.Warnf(proxyScreenshotLogModule, "请求截图失败 remote_path=%s err=%v", compactProxyBody(trimmedPath), err)
		return proxyScreenshotResponse{}, &ProxyAPIError{StatusCode: 500, Message: "代理请求失败: " + err.Error()}
	}
	defer response.Body.Close()

	bodyBytes, _ := io.ReadAll(response.Body)
	bodyText := strings.TrimSpace(string(bodyBytes))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		logx.Warnf(proxyScreenshotLogModule, "请求截图失败 remote_path=%s status=%d body=%s", compactProxyBody(trimmedPath), response.StatusCode, compactProxyBody(bodyText))
		return proxyScreenshotResponse{}, &ProxyAPIError{StatusCode: response.StatusCode, Message: compactProxyBody(bodyText)}
	}

	resp := proxyScreenshotResponse{}
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		logx.Warnf(proxyScreenshotLogModule, "解析截图响应失败 remote_path=%s err=%v", compactProxyBody(trimmedPath), err)
		return proxyScreenshotResponse{}, &ProxyAPIError{StatusCode: 500, Message: "解析代理响应失败: " + err.Error()}
	}
	if !resp.Success {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = "代理返回 success=false"
		}
		logx.Warnf(proxyScreenshotLogModule, "请求截图失败 remote_path=%s reason=%s", compactProxyBody(trimmedPath), compactProxyBody(msg))
		return proxyScreenshotResponse{}, &ProxyAPIError{StatusCode: 500, Message: msg}
	}

	logx.Infof(proxyScreenshotLogModule, "请求截图成功 remote_path=%s mode=%s bbcode_len=%d preview_count=%d", compactProxyBody(trimmedPath), strings.TrimSpace(mode), len([]rune(strings.TrimSpace(resp.BBCode))), len(resp.PreviewCandidates))
	return resp, nil
}
