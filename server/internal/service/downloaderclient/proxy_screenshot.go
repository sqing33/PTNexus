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
	RemotePath          string    `json:"remote_path"`
	ContentName         string    `json:"content_name,omitempty"`
	Mode                string    `json:"mode,omitempty"`
	PreviewCount        int       `json:"preview_count,omitempty"`
	SelectedTimes       []float64 `json:"selected_times,omitempty"`
	SelectedSubtitleSID *int      `json:"selected_subtitle_sid,omitempty"`
	PixhostDomain       string    `json:"pixhost_domain,omitempty"`
}

// ProxyScreenshotPreviewCandidate 描述盒子代理返回的低清截图候选。
type ProxyScreenshotPreviewCandidate struct {
	ID          string  `json:"id"`
	TimeSeconds float64 `json:"time_seconds"`
	TimeLabel   string  `json:"time_label"`
	PreviewData string  `json:"preview_data"`
	Recommended bool    `json:"recommended"`
}

type ProxyScreenshotSubtitleState string

const (
	ProxyScreenshotSubtitleStateConfirmedChinese     ProxyScreenshotSubtitleState = "confirmed_chinese"
	ProxyScreenshotSubtitleStateUsableButUnconfirmed ProxyScreenshotSubtitleState = "usable_but_unconfirmed"
	ProxyScreenshotSubtitleStateNoUsableSubtitle     ProxyScreenshotSubtitleState = "no_usable_subtitle"
)

// ProxyScreenshotSubtitleStream 描述盒子代理返回的字幕流信息。
type ProxyScreenshotSubtitleStream struct {
	SubtitleSID        int    `json:"subtitle_sid"`
	StreamIndex        int    `json:"stream_index"`
	CodecName          string `json:"codec_name"`
	Language           string `json:"language,omitempty"`
	Title              string `json:"title,omitempty"`
	DisplayName        string `json:"display_name"`
	IsConfidentChinese bool   `json:"is_confident_chinese"`
	IsDefault          bool   `json:"is_default"`
}

type proxyScreenshotResponse struct {
	Success            bool                              `json:"success"`
	Message            string                            `json:"message"`
	BBCode             string                            `json:"bbcode,omitempty"`
	SubtitleState      string                            `json:"subtitle_state,omitempty"`
	SubtitleStreams    []ProxyScreenshotSubtitleStream   `json:"subtitle_streams,omitempty"`
	CurrentSubtitleSID int                               `json:"current_subtitle_sid,omitempty"`
	PreviewCandidates  []ProxyScreenshotPreviewCandidate `json:"preview_candidates,omitempty"`
}

// ProxyScreenshotInspectResult 描述代理返回的字幕探测结果。
type ProxyScreenshotInspectResult struct {
	SubtitleState      ProxyScreenshotSubtitleState
	SubtitleStreams    []ProxyScreenshotSubtitleStream
	CurrentSubtitleSID int
}

// ProxyScreenshotPreviewResult 描述代理返回的候选截图与字幕上下文。
type ProxyScreenshotPreviewResult struct {
	Candidates         []ProxyScreenshotPreviewCandidate
	SubtitleState      ProxyScreenshotSubtitleState
	SubtitleStreams    []ProxyScreenshotSubtitleStream
	CurrentSubtitleSID int
}

// FetchScreenshotsByProxy 通过盒子代理远程截图并上传图床，返回截图 BBCode 文本。
// 参数/返回：remotePath 为盒子上的实际路径（通常是下载器返回的 save_path 或其子目录）；contentName 用于多文件时辅助选取目标视频；返回截图 BBCode。
// 失败场景：代理不可达、HTTP 返回异常、代理返回 success=false、响应解析失败、BBCode 为空。
// 副作用：会向盒子代理服务发起 HTTP 请求。
func (d Downloader) FetchScreenshotsByProxy(remotePath, contentName string, screenshotCount int, selectedSubtitleSID *int) (string, error) {
	if screenshotCount <= 0 {
		screenshotCount = 3
	}
	resp, err := d.requestProxyScreenshots(remotePath, contentName, "", screenshotCount, nil, selectedSubtitleSID)
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
func (d Downloader) FetchSelectedScreenshotsByProxy(
	remotePath string,
	contentName string,
	selectedTimes []float64,
	selectedSubtitleSID *int,
) (string, error) {
	resp, err := d.requestProxyScreenshots(remotePath, contentName, "finalize", len(selectedTimes), selectedTimes, selectedSubtitleSID)
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

// FetchRandomScreenshotsByProxy 通过盒子代理按随机时间点生成正式截图并上传。
// 参数/返回：screenshotCount 为期望截图数量；返回正式截图 BBCode。
// 失败场景：代理不可达、代理未返回有效截图或截图数量配置无效时返回错误。
// 副作用：会向盒子代理服务发起 HTTP 请求。
func (d Downloader) FetchRandomScreenshotsByProxy(
	remotePath string,
	contentName string,
	screenshotCount int,
	selectedSubtitleSID *int,
) (string, error) {
	if screenshotCount <= 0 {
		screenshotCount = 3
	}
	resp, err := d.requestProxyScreenshots(remotePath, contentName, "random_final", screenshotCount, nil, selectedSubtitleSID)
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

// InspectScreenshotByProxy 通过盒子代理探测目标视频的字幕状态与可切换字幕流。
// 失败场景：代理不可达、路径不存在、响应解析失败时返回错误。
// 副作用：会向盒子代理服务发起 HTTP 请求。
func (d Downloader) InspectScreenshotByProxy(remotePath, contentName string) (ProxyScreenshotInspectResult, error) {
	resp, err := d.requestProxyScreenshots(remotePath, contentName, "inspect", 0, nil, nil)
	if err != nil {
		return ProxyScreenshotInspectResult{}, err
	}
	return ProxyScreenshotInspectResult{
		SubtitleState:      ProxyScreenshotSubtitleState(strings.TrimSpace(resp.SubtitleState)),
		SubtitleStreams:    resp.SubtitleStreams,
		CurrentSubtitleSID: resp.CurrentSubtitleSID,
	}, nil
}

// FetchScreenshotPreviewsByProxy 通过盒子代理生成低清截图候选，供前端人工挑选。
// 参数/返回：previewCount 为候选数量；返回候选截图及当前字幕状态。
// 失败场景：代理不可达、HTTP 返回异常、代理返回 success=false、未返回候选。
// 副作用：会向盒子代理服务发起 HTTP 请求。
func (d Downloader) fetchScreenshotPreviewResponse(
	remotePath string,
	contentName string,
	previewCount int,
	selectedSubtitleSID *int,
) (proxyScreenshotResponse, error) {
	resp, err := d.requestProxyScreenshots(remotePath, contentName, "preview", previewCount, nil, selectedSubtitleSID)
	if err != nil {
		return proxyScreenshotResponse{}, err
	}
	if len(resp.PreviewCandidates) == 0 {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = "代理未返回可用候选截图"
		}
		return proxyScreenshotResponse{}, &ProxyAPIError{StatusCode: 500, Message: msg}
	}
	return resp, nil
}

func (d Downloader) FetchScreenshotPreviewsByProxy(
	remotePath string,
	contentName string,
	previewCount int,
	selectedSubtitleSID *int,
) (ProxyScreenshotPreviewResult, error) {
	resp, err := d.fetchScreenshotPreviewResponse(remotePath, contentName, previewCount, selectedSubtitleSID)
	if err != nil {
		return ProxyScreenshotPreviewResult{}, err
	}
	return ProxyScreenshotPreviewResult{
		Candidates:         resp.PreviewCandidates,
		SubtitleState:      ProxyScreenshotSubtitleState(strings.TrimSpace(resp.SubtitleState)),
		SubtitleStreams:    resp.SubtitleStreams,
		CurrentSubtitleSID: resp.CurrentSubtitleSID,
	}, nil
}

func (d Downloader) requestProxyScreenshots(
	remotePath string,
	contentName string,
	mode string,
	previewCount int,
	selectedTimes []float64,
	selectedSubtitleSID *int,
) (proxyScreenshotResponse, error) {
	trimmedPath := strings.TrimSpace(remotePath)
	if trimmedPath == "" {
		return proxyScreenshotResponse{}, &ProxyAPIError{StatusCode: 400, Message: "remote_path 不能为空"}
	}
	currentSubtitleSID := 0
	if selectedSubtitleSID != nil {
		currentSubtitleSID = *selectedSubtitleSID
	}
	logx.Infof(
		proxyScreenshotLogModule,
		"开始请求截图 remote_path=%s mode=%s preview_count=%d selected=%d subtitle_sid=%d",
		compactProxyBody(trimmedPath),
		strings.TrimSpace(mode),
		previewCount,
		len(selectedTimes),
		currentSubtitleSID,
	)

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
		RemotePath:          trimmedPath,
		ContentName:         strings.TrimSpace(contentName),
		Mode:                strings.TrimSpace(mode),
		PreviewCount:        previewCount,
		SelectedTimes:       selectedTimes,
		SelectedSubtitleSID: selectedSubtitleSID,
		PixhostDomain:       strings.TrimSpace(d.PixhostDomain),
	}
	payloadBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return proxyScreenshotResponse{}, &ProxyAPIError{StatusCode: 500, Message: "构造请求失败: " + err.Error()}
	}

	proxyURL := fmt.Sprintf("http://%s:%d/api/media/screenshot", proxyIP, proxyPort)
	logx.Infof(proxyScreenshotLogModule, "代理截图目标 mode=%s proxy_url=%s remote_path=%s", strings.TrimSpace(mode), proxyURL, compactProxyBody(trimmedPath))
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

	logx.Infof(
		proxyScreenshotLogModule,
		"请求截图成功 remote_path=%s mode=%s bbcode_len=%d image_urls=%d preview_count=%d subtitle_state=%s current_sid=%d",
		compactProxyBody(trimmedPath),
		strings.TrimSpace(mode),
		len([]rune(strings.TrimSpace(resp.BBCode))),
		strings.Count(strings.ToLower(resp.BBCode), "[img]"),
		len(resp.PreviewCandidates),
		strings.TrimSpace(resp.SubtitleState),
		resp.CurrentSubtitleSID,
	)
	return resp, nil
}
