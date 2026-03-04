package desktopapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

// RouteTargets 描述桌面路由分流要挂接的目标处理器。
type RouteTargets struct {
	APIHandler    http.Handler
	UpdateHandler http.Handler
	IndexHTML     []byte
}

// NewRouteMux 创建桌面分流入口：
// 1) /api/* -> APIHandler（当前接 server）
// 2) /update/* -> UpdateHandler（后续接 updater）
// 3) 其他 GET 路径返回 index.html（支持 Vue history 路由刷新）
func NewRouteMux(targets RouteTargets) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/api" || strings.HasPrefix(path, "/api/"):
			if targets.APIHandler == nil {
				writeJSONError(w, http.StatusNotImplemented, "桌面端已切换为 Wails 原生通信：请使用 window.go.main.App.DesktopRequest 调用后端")
				return
			}
			targets.APIHandler.ServeHTTP(w, r)
			return
		case path == "/update" || strings.HasPrefix(path, "/update/"):
			if targets.UpdateHandler == nil {
				writeJSONError(w, http.StatusNotImplemented, "桌面端已切换为 Wails 原生通信：请使用 window.go.main.App.DesktopRequest 调用更新模块")
				return
			}
			targets.UpdateHandler.ServeHTTP(w, r)
			return
		default:
			if r.Method == http.MethodGet && len(targets.IndexHTML) > 0 {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				_, _ = bytes.NewReader(targets.IndexHTML).WriteTo(w)
				return
			}
			http.NotFound(w, r)
		}
	})
}

// NewMockTargetHandler 返回占位处理器，用于当前阶段未接线的能力。
func NewMockTargetHandler(target string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONError(w, http.StatusNotImplemented, "PT Nexus placeholder: "+target)
	})
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"message": message,
	})
}
