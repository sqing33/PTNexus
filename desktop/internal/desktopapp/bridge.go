package desktopapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DesktopRequest 是桌面端（Wails）与 Go sidecar 之间的通用请求描述。
// 说明：用于替代前端直接走 HTTP API，统一通过 Wails 绑定方法代理。
type DesktopRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`

	BodyText   string `json:"bodyText,omitempty"`
	BodyBase64 string `json:"bodyBase64,omitempty"`

	// TimeoutMs 为 0 时使用默认超时（60s）。
	TimeoutMs int `json:"timeoutMs,omitempty"`
}

// DesktopResponse 是 DesktopRequest 的响应。
type DesktopResponse struct {
	Status     int               `json:"status"`
	StatusText string            `json:"statusText,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	BodyBase64 string            `json:"bodyBase64,omitempty"`
}

// SidecarEndpoints 描述桌面端 sidecar 的访问入口。
type SidecarEndpoints struct {
	ServerBaseURL  string
	UpdaterBaseURL string
}

// DoDesktopRequest 将请求转发到对应 sidecar 并返回完整响应。
// 约定：/api/* 与 /health 走 server；/update/* 走 updater；其他路径返回错误。
func DoDesktopRequest(ctx context.Context, endpoints SidecarEndpoints, req DesktopRequest) (DesktopResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		return DesktopResponse{}, fmt.Errorf("missing url")
	}

	targetURL, err := resolveDesktopTargetURL(endpoints, rawURL)
	if err != nil {
		return DesktopResponse{}, err
	}

	body, err := buildRequestBody(req)
	if err != nil {
		return DesktopResponse{}, err
	}

	timeout := 60 * time.Second
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}

	requestCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	httpReq, err := http.NewRequestWithContext(requestCtx, method, targetURL, body)
	if err != nil {
		return DesktopResponse{}, fmt.Errorf("build request failed: %w", err)
	}
	for k, v := range req.Headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		httpReq.Header.Set(k, v)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return DesktopResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	headers := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) == 0 {
			continue
		}
		headers[k] = strings.Join(v, ", ")
	}

	return DesktopResponse{
		Status:     resp.StatusCode,
		StatusText: resp.Status,
		Headers:    headers,
		BodyBase64: base64.StdEncoding.EncodeToString(data),
	}, nil
}

func resolveDesktopTargetURL(endpoints SidecarEndpoints, rawURL string) (string, error) {
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL, nil
	}

	path := rawURL
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	switch {
	case path == "/health" || strings.HasPrefix(path, "/api/") || path == "/api":
		base := strings.TrimRight(strings.TrimSpace(endpoints.ServerBaseURL), "/")
		if base == "" {
			return "", fmt.Errorf("server endpoint not ready")
		}
		return base + path, nil
	case strings.HasPrefix(path, "/update/") || path == "/update":
		base := strings.TrimRight(strings.TrimSpace(endpoints.UpdaterBaseURL), "/")
		if base == "" {
			return "", fmt.Errorf("updater endpoint not ready")
		}
		return base + path, nil
	default:
		return "", fmt.Errorf("unsupported desktop url: %s", rawURL)
	}
}

func buildRequestBody(req DesktopRequest) (io.Reader, error) {
	if req.BodyBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(req.BodyBase64)
		if err != nil {
			return nil, fmt.Errorf("decode base64 body failed: %w", err)
		}
		return bytes.NewReader(decoded), nil
	}
	if req.BodyText != "" {
		return strings.NewReader(req.BodyText), nil
	}
	return nil, nil
}
