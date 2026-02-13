package downloaderclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pt-nexus/server-go/internal/platform/logx"
)

const proxyBDInfoLogModule = "盒子代理-BDInfo"

// ProxyBDInfoTaskStatus 描述盒子代理侧 BDInfo 任务的进度与结果快照。
// 参数/返回：字段命名与代理接口保持一致，供上层轮询并驱动控制端 BDInfoState/写库。
// 失败场景：无。
// 副作用：无。
type ProxyBDInfoTaskStatus struct {
	TaskID          string  `json:"task_id"`
	ProgressPercent float64 `json:"progress_percent"`
	CurrentFile     string  `json:"current_file"`
	ElapsedTime     string  `json:"elapsed_time"`
	RemainingTime   string  `json:"remaining_time"`
	Status          string  `json:"status"` // running/completed/failed
	DiscSize        int64   `json:"disc_size,omitempty"`
	BDInfoContent   string  `json:"bdinfo_content,omitempty"`
	ErrorMessage    string  `json:"error_message,omitempty"`
}

type proxyBDInfoProgressResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Task    ProxyBDInfoTaskStatus `json:"task"`
}

// FetchBDInfoProgressByProxy 通过盒子代理轮询 BDInfo 任务进度与结果。
// 参数/返回：taskID 为控制端/代理端共用的任务标识；成功返回任务状态快照。
// 失败场景：代理不可达、HTTP 返回异常、响应解析失败、success=false。
// 副作用：会向盒子代理服务发起 HTTP 请求。
func (d Downloader) FetchBDInfoProgressByProxy(taskID string) (ProxyBDInfoTaskStatus, error) {
	trimmedTaskID := strings.TrimSpace(taskID)
	if trimmedTaskID == "" {
		return ProxyBDInfoTaskStatus{}, &ProxyAPIError{StatusCode: 400, Message: "task_id 不能为空"}
	}

	proxyPort := d.ProxyPort
	if proxyPort <= 0 {
		proxyPort = 9090
	}

	parsedURL, err := parseHostURL(d.Host)
	if err != nil {
		return ProxyBDInfoTaskStatus{}, &ProxyAPIError{StatusCode: 500, Message: "解析 host 失败: " + err.Error()}
	}
	proxyIP := strings.TrimSpace(parsedURL.Hostname())
	if proxyIP == "" {
		proxyIP = extractHostFallback(d.Host)
	}
	if proxyIP == "" {
		return ProxyBDInfoTaskStatus{}, &ProxyAPIError{StatusCode: 500, Message: "无法解析代理地址: host=" + strings.TrimSpace(d.Host)}
	}

	progressURL := fmt.Sprintf("http://%s:%d/api/media/bdinfo/progress/%s", proxyIP, proxyPort, trimmedTaskID)
	req, err := http.NewRequest(http.MethodGet, progressURL, nil)
	if err != nil {
		return ProxyBDInfoTaskStatus{}, &ProxyAPIError{StatusCode: 500, Message: "创建请求失败: " + err.Error()}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ProxyBDInfoTaskStatus{}, &ProxyAPIError{StatusCode: 500, Message: "代理请求失败: " + err.Error()}
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyText := strings.TrimSpace(string(bodyBytes))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ProxyBDInfoTaskStatus{}, &ProxyAPIError{StatusCode: resp.StatusCode, Message: compactProxyBody(bodyText)}
	}

	parsed := proxyBDInfoProgressResponse{}
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return ProxyBDInfoTaskStatus{}, &ProxyAPIError{StatusCode: 500, Message: "解析代理响应失败: " + err.Error()}
	}
	if !parsed.Success {
		msg := strings.TrimSpace(parsed.Message)
		if msg == "" {
			msg = "代理返回 success=false"
		}
		return ProxyBDInfoTaskStatus{}, &ProxyAPIError{StatusCode: 500, Message: msg}
	}

	status := parsed.Task
	if strings.TrimSpace(status.TaskID) == "" {
		status.TaskID = trimmedTaskID
	}
	logx.Debugf(proxyBDInfoLogModule, "轮询BDInfo进度成功 task_id=%s status=%s progress=%.2f", trimmedTaskID, strings.TrimSpace(status.Status), status.ProgressPercent)
	return status, nil
}
