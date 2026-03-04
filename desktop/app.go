package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/pt-nexus/server/desktop/internal/desktopapp"
	"github.com/pt-nexus/server/desktop/internal/tray"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx         context.Context
	trayService tray.Service

	sidecarMu sync.Mutex
	endpoints desktopapp.SidecarEndpoints
	server    *desktopapp.Sidecar
	updater   *desktopapp.Sidecar

	sseMu     sync.Mutex
	sseCancel map[string]context.CancelFunc
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		trayService: tray.New(),
		sseCancel:   map[string]context.CancelFunc{},
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.trayService != nil {
		a.trayService.Start(ctx)
	}

	// 注入桌面运行时路径（DB 配置文件等），并尝试拉起 sidecar。
	env := desktopapp.EnsureDesktopRuntimeEnv()
	if err := a.ensureSidecarsStarted(env); err != nil {
		fmt.Printf("PT Nexus 桌面端 sidecar 启动失败: %v\n", err)
	}
}

// shutdown 在应用退出时调用，用于托盘资源清理。
func (a *App) shutdown(ctx context.Context) {
	a.ctx = ctx
	if a.trayService != nil {
		a.trayService.Stop()
	}

	a.stopAllSSE()
	a.stopSidecars()
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

// GetDatabaseConfigFilePath 返回桌面端数据库配置文件路径。
func (a *App) GetDatabaseConfigFilePath() string {
	desktopapp.EnsureDesktopRuntimeEnv()
	return desktopapp.DatabaseConfigFilePath()
}

// OpenDatabaseConfigFile 使用系统默认程序打开桌面端 database.json。
func (a *App) OpenDatabaseConfigFile() error {
	desktopapp.EnsureDesktopRuntimeEnv()
	desktopapp.EnsureDatabaseConfigFile()
	path := desktopapp.DatabaseConfigFilePath()
	return openWithSystemDefault(path)
}

// OpenExternalURL 使用系统默认浏览器打开外部链接。
func (a *App) OpenExternalURL(rawURL string) error {
	target := strings.TrimSpace(rawURL)
	if target == "" {
		return fmt.Errorf("missing url")
	}

	parsed, err := url.ParseRequestURI(target)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported url scheme: %s", parsed.Scheme)
	}

	return openWithSystemDefault(target)
}

// DesktopRequest 通过 Wails 原生绑定代理 /api 与 /update 请求到本地 sidecar。
func (a *App) DesktopRequest(req desktopapp.DesktopRequest) (desktopapp.DesktopResponse, error) {
	env := desktopapp.EnsureDesktopRuntimeEnv()
	if err := a.ensureSidecarsForURL(env, req.URL); err != nil {
		return desktopapp.DesktopResponse{}, err
	}
	return desktopapp.DoDesktopRequest(a.ctxOrBackground(), a.endpoints, req)
}

// DesktopSSESubscribe 建立到 sidecar 的 SSE 流，并通过 window.runtime.EventsOn 推送到前端。
// 返回：
// - id：用于取消订阅
// - eventName：前端监听的事件名
func (a *App) DesktopSSESubscribe(url string) (map[string]string, error) {
	raw := strings.TrimSpace(url)
	if raw == "" {
		return nil, fmt.Errorf("missing url")
	}

	env := desktopapp.EnsureDesktopRuntimeEnv()
	if err := a.ensureSidecarsForURL(env, raw); err != nil {
		return nil, err
	}

	id := fmt.Sprintf("sse-%d", time.Now().UnixNano())
	eventName := "ptnexus:sse:" + id

	streamCtx, cancel := context.WithCancel(a.ctxOrBackground())
	a.sseMu.Lock()
	a.sseCancel[id] = cancel
	a.sseMu.Unlock()

	target := a.resolveSidecarURL(raw)
	go a.runSSE(streamCtx, eventName, target)

	return map[string]string{
		"id":        id,
		"eventName": eventName,
	}, nil
}

// DesktopSSEUnsubscribe 取消 SSE 订阅。
func (a *App) DesktopSSEUnsubscribe(id string) {
	a.sseMu.Lock()
	cancel := a.sseCancel[id]
	delete(a.sseCancel, id)
	a.sseMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) ctxOrBackground() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) ensureSidecarsForURL(env desktopapp.DesktopRuntimeEnv, rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return fmt.Errorf("missing url")
	}
	if strings.HasPrefix(trimmed, "/update") {
		return a.ensureSidecarsStarted(env)
	}
	// 默认 /api 与 /health 只依赖 server。
	return a.ensureServerStarted(env)
}

func (a *App) ensureSidecarsStarted(env desktopapp.DesktopRuntimeEnv) error {
	if err := a.ensureServerStarted(env); err != nil {
		return err
	}
	return a.ensureUpdaterStarted(env)
}

func (a *App) ensureServerStarted(env desktopapp.DesktopRuntimeEnv) error {
	a.sidecarMu.Lock()
	defer a.sidecarMu.Unlock()

	if a.server != nil && a.endpoints.ServerBaseURL != "" {
		return nil
	}
	server, err := desktopapp.StartServerSidecar(env)
	if err != nil {
		return err
	}
	a.server = server
	if server != nil {
		a.endpoints.ServerBaseURL = server.BaseURL
	}
	return nil
}

func (a *App) ensureUpdaterStarted(env desktopapp.DesktopRuntimeEnv) error {
	a.sidecarMu.Lock()
	defer a.sidecarMu.Unlock()

	if a.updater != nil && a.endpoints.UpdaterBaseURL != "" {
		return nil
	}
	updater, err := desktopapp.StartUpdaterSidecar(env)
	if err != nil {
		return err
	}
	a.updater = updater
	if updater != nil {
		a.endpoints.UpdaterBaseURL = updater.BaseURL
	}
	return nil
}

func (a *App) stopSidecars() {
	a.sidecarMu.Lock()
	server := a.server
	updater := a.updater
	a.server = nil
	a.updater = nil
	a.endpoints = desktopapp.SidecarEndpoints{}
	a.sidecarMu.Unlock()

	if updater != nil {
		updater.Stop()
	}
	if server != nil {
		server.Stop()
	}
}

func (a *App) stopAllSSE() {
	a.sseMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(a.sseCancel))
	for _, cancel := range a.sseCancel {
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
	}
	a.sseCancel = map[string]context.CancelFunc{}
	a.sseMu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

func (a *App) resolveSidecarURL(rawURL string) string {
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	path := rawURL
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	if strings.HasPrefix(path, "/update") && a.endpoints.UpdaterBaseURL != "" {
		return strings.TrimRight(a.endpoints.UpdaterBaseURL, "/") + path
	}
	return strings.TrimRight(a.endpoints.ServerBaseURL, "/") + path
}

func (a *App) runSSE(ctx context.Context, eventName string, targetURL string) {
	if strings.TrimSpace(targetURL) == "" {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		wailsruntime.EventsEmit(a.ctxOrBackground(), eventName, map[string]any{"type": "error", "error": err.Error()})
		return
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		wailsruntime.EventsEmit(a.ctxOrBackground(), eventName, map[string]any{"type": "error", "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	go func() {
		<-ctx.Done()
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		wailsruntime.EventsEmit(a.ctxOrBackground(), eventName, map[string]any{
			"type":  "error",
			"error": fmt.Sprintf("SSE status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body))),
		})
		return
	}

	wailsruntime.EventsEmit(a.ctxOrBackground(), eventName, map[string]any{"type": "open"})

	reader := bufio.NewReader(resp.Body)
	dataLines := make([]string, 0, 2)

	flush := func() {
		if len(dataLines) == 0 {
			return
		}
		data := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		wailsruntime.EventsEmit(a.ctxOrBackground(), eventName, map[string]any{
			"type": "message",
			"data": data,
		})
	}

	for {
		select {
		case <-ctx.Done():
			wailsruntime.EventsEmit(a.ctxOrBackground(), eventName, map[string]any{"type": "close"})
			return
		default:
		}

		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			flush()
			if ctx.Err() != nil {
				wailsruntime.EventsEmit(a.ctxOrBackground(), eventName, map[string]any{"type": "close"})
				return
			}
			wailsruntime.EventsEmit(a.ctxOrBackground(), eventName, map[string]any{"type": "error", "error": readErr.Error()})
			return
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

func openWithSystemDefault(target string) error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	configureCommandForPlatform(cmd)
	return cmd.Start()
}
