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

type proxyBDInfoRequest struct {
	RemotePath  string `json:"remote_path"`
	TaskID      string `json:"task_id"`
	CallbackURL string `json:"callback_url,omitempty"`
}

type proxyBDInfoResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	TaskID  string `json:"task_id,omitempty"`
}

// StartBDInfoByProxy 通过盒子代理启动 BDInfo 提取任务（异步），并由代理回调控制端写库与更新进度。
// 参数/返回：remotePath 为盒子侧蓝光根目录路径；taskID 为控制端生成并写入数据库的任务标识；callbackBaseURL 为控制端可被盒子访问的回调基地址（如 http://192.168.1.100:5275），函数会自动拼接为 /api/migrate/bdinfo。
// 失败场景：代理不可达、HTTP 返回异常、代理返回 success=false 或响应解析失败。
// 副作用：会向盒子代理服务发起 HTTP 请求，并触发后续进度/完成回调到控制端。
func (d Downloader) StartBDInfoByProxy(remotePath, taskID, callbackBaseURL string) error {
	trimmedPath := strings.TrimSpace(remotePath)
	trimmedTaskID := strings.TrimSpace(taskID)
	if trimmedPath == "" {
		return &ProxyAPIError{StatusCode: 400, Message: "remote_path 不能为空"}
	}
	if trimmedTaskID == "" {
		return &ProxyAPIError{StatusCode: 400, Message: "task_id 不能为空"}
	}
	logx.Infof(proxyBDInfoLogModule, "提交BDInfo任务 task_id=%s remote_path=%s callback_base_set=%t", trimmedTaskID, compactProxyBody(trimmedPath), strings.TrimSpace(callbackBaseURL) != "")

	proxyPort := d.ProxyPort
	if proxyPort <= 0 {
		proxyPort = 9090
	}

	parsedURL, err := parseHostURL(d.Host)
	if err != nil {
		return &ProxyAPIError{StatusCode: 500, Message: "解析 host 失败: " + err.Error()}
	}
	proxyIP := strings.TrimSpace(parsedURL.Hostname())
	if proxyIP == "" {
		proxyIP = extractHostFallback(d.Host)
	}
	if proxyIP == "" {
		return &ProxyAPIError{StatusCode: 500, Message: "无法解析代理地址: host=" + strings.TrimSpace(d.Host)}
	}

	callbackURL := normalizeBDInfoCallbackURL(callbackBaseURL)
	requestPayload := proxyBDInfoRequest{
		RemotePath:  trimmedPath,
		TaskID:      trimmedTaskID,
		CallbackURL: callbackURL,
	}
	payloadBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return &ProxyAPIError{StatusCode: 500, Message: "构造请求失败: " + err.Error()}
	}

	proxyURL := fmt.Sprintf("http://%s:%d/api/media/bdinfo", proxyIP, proxyPort)
	request, err := http.NewRequest(http.MethodPost, proxyURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return &ProxyAPIError{StatusCode: 500, Message: "创建请求失败: " + err.Error()}
	}
	request.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		logx.Warnf(proxyBDInfoLogModule, "提交BDInfo任务失败 task_id=%s remote_path=%s err=%v", trimmedTaskID, compactProxyBody(trimmedPath), err)
		return &ProxyAPIError{StatusCode: 500, Message: "代理请求失败: " + err.Error()}
	}
	defer response.Body.Close()

	bodyBytes, _ := io.ReadAll(response.Body)
	bodyText := strings.TrimSpace(string(bodyBytes))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		logx.Warnf(proxyBDInfoLogModule, "提交BDInfo任务失败 task_id=%s remote_path=%s status=%d body=%s", trimmedTaskID, compactProxyBody(trimmedPath), response.StatusCode, compactProxyBody(bodyText))
		return &ProxyAPIError{StatusCode: response.StatusCode, Message: compactProxyBody(bodyText)}
	}

	resp := proxyBDInfoResponse{}
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		logx.Warnf(proxyBDInfoLogModule, "解析BDInfo提交响应失败 task_id=%s remote_path=%s err=%v", trimmedTaskID, compactProxyBody(trimmedPath), err)
		return &ProxyAPIError{StatusCode: 500, Message: "解析代理响应失败: " + err.Error()}
	}
	if !resp.Success {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = "代理返回 success=false"
		}
		logx.Warnf(proxyBDInfoLogModule, "提交BDInfo任务失败 task_id=%s remote_path=%s reason=%s", trimmedTaskID, compactProxyBody(trimmedPath), compactProxyBody(msg))
		return &ProxyAPIError{StatusCode: 500, Message: msg}
	}
	logx.Infof(proxyBDInfoLogModule, "提交BDInfo任务成功 task_id=%s remote_path=%s", trimmedTaskID, compactProxyBody(trimmedPath))
	return nil
}

func normalizeBDInfoCallbackURL(callbackBaseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(callbackBaseURL), "/")
	if trimmed == "" {
		return ""
	}
	if strings.HasSuffix(trimmed, "/api/migrate/bdinfo") {
		return trimmed
	}
	return trimmed + "/api/migrate/bdinfo"
}
