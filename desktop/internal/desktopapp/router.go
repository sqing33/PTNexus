package desktopapp

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pt-nexus/server-go/internal/bootstrap"
	"github.com/pt-nexus/server-go/internal/platform/logx"
)

// RouteTargets 描述桌面路由分流要挂接的目标处理器。
type RouteTargets struct {
	APIHandler    http.Handler
	UpdateHandler http.Handler
}

var desktopLogInitOnce sync.Once

// NewRouteMux 创建桌面分流入口：
// 1) /api/* -> APIHandler（当前接 server-go）
// 2) /update/* -> UpdateHandler（后续接 updater）
// 3) 其他路径交给 Wails 静态资源处理器
func NewRouteMux(targets RouteTargets) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/"):
			if targets.APIHandler == nil {
				writeJSONError(w, http.StatusNotImplemented, "PT Nexus placeholder: API handler not wired")
				return
			}
			targets.APIHandler.ServeHTTP(w, r)
			return
		case strings.HasPrefix(r.URL.Path, "/update/"):
			if targets.UpdateHandler == nil {
				writeJSONError(w, http.StatusNotImplemented, "PT Nexus placeholder: update handler not wired")
				return
			}
			targets.UpdateHandler.ServeHTTP(w, r)
			return
		default:
			http.NotFound(w, r)
		}
	})
}

// NewServerGoAPIHandler 初始化 server-go 并返回 API 处理器。
// 若初始化失败，返回错误占位 handler，避免桌面进程整体启动失败。
func NewServerGoAPIHandler() http.Handler {
	ensureDesktopRuntimeEnv()
	initDesktopLogging()

	app, err := bootstrap.NewApp()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSONError(w, http.StatusServiceUnavailable, "server-go bootstrap failed: "+err.Error())
		})
	}
	return app.Engine
}

// NewUpdaterProxyHandler 启动 updater sidecar 并返回 /update 代理处理器。
// 返回的 cleanup 可在应用退出时回收子进程。
func NewUpdaterProxyHandler() (http.Handler, func()) {
	cmd, err := startUpdaterSidecar()
	if err != nil {
		return NewMockTargetHandler("updater /update not available: " + err.Error()), func() {}
	}

	targetURL, _ := url.Parse("http://127.0.0.1:5274")
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, proxyErr error) {
		writeJSONError(w, http.StatusBadGateway, "updater proxy error: "+proxyErr.Error())
	}

	cleanup := func() {
		if cmd.Process == nil {
			return
		}
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}

	return proxy, cleanup
}

// NewMockTargetHandler 返回占位处理器，用于当前阶段未接线的能力。
func NewMockTargetHandler(target string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONError(w, http.StatusNotImplemented, "PT Nexus placeholder: "+target)
	})
}

func startUpdaterSidecar() (*exec.Cmd, error) {
	updaterPath, err := resolveUpdaterBinaryPath()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(updaterPath)
	cmd.Env = append(os.Environ(),
		"UPDATER_PORT=5274",
		"SERVER_PORT=5275",
		"BATCH_PORT=5276",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start updater failed: %w", err)
	}

	if err := waitHTTPReady("127.0.0.1:5274", 8*time.Second); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, fmt.Errorf("updater health check failed: %w", err)
	}
	return cmd, nil
}

func resolveUpdaterBinaryPath() (string, error) {
	candidates := make([]string, 0, 6)

	if explicit := strings.TrimSpace(os.Getenv("PTNEXUS_UPDATER_PATH")); explicit != "" {
		candidates = append(candidates, explicit)
	}

	if exePath, err := os.Executable(); err == nil && strings.TrimSpace(exePath) != "" {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates, filepath.Join(exeDir, "updater.exe"))
	}

	if cwd, err := os.Getwd(); err == nil && strings.TrimSpace(cwd) != "" {
		candidates = append(candidates, filepath.Join(cwd, "updater.exe"))
		candidates = append(candidates, filepath.Join(cwd, "build", "windows", "sidecar", "updater.exe"))
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("updater.exe not found in candidates: %s", strings.Join(candidates, ", "))
}

func waitHTTPReady(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 1200 * time.Millisecond}

	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			resp, reqErr := client.Get("http://" + addr + "/health")
			if reqErr == nil {
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 500 {
					return nil
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", addr)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"message": message,
	})
}

// ensureDesktopRuntimeEnv 为桌面模式注入默认运行路径，避免 server-go 回退到 /app。
func ensureDesktopRuntimeEnv() {
	configRoot, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(configRoot) == "" {
		configRoot = "."
	}
	baseDir := filepath.Join(configRoot, "pt-nexus")
	dataDir := filepath.Join(baseDir, "data")
	dbConfigFile := filepath.Join(dataDir, "database.json")

	if strings.TrimSpace(os.Getenv("PTNEXUS_BASE_DIR")) == "" {
		_ = os.Setenv("PTNEXUS_BASE_DIR", baseDir)
	}
	if strings.TrimSpace(os.Getenv("PTNEXUS_DATA_DIR")) == "" {
		_ = os.Setenv("PTNEXUS_DATA_DIR", dataDir)
	}
	if strings.TrimSpace(os.Getenv("PTNEXUS_RUNTIME")) == "" {
		_ = os.Setenv("PTNEXUS_RUNTIME", "desktop")
	}
	if strings.TrimSpace(os.Getenv("PTNEXUS_DB_CONFIG_FILE")) == "" {
		_ = os.Setenv("PTNEXUS_DB_CONFIG_FILE", dbConfigFile)
	}
	if strings.TrimSpace(os.Getenv("PTNEXUS_STATIC_DIR")) == "" {
		_ = os.Setenv("PTNEXUS_STATIC_DIR", filepath.Join(baseDir, "dist"))
	}
	if strings.TrimSpace(os.Getenv("PTNEXUS_LOG_DIR")) == "" {
		_ = os.Setenv("PTNEXUS_LOG_DIR", filepath.Join(dataDir, "logs"))
	}

	_ = os.MkdirAll(dataDir, 0o755)
	_ = os.MkdirAll(filepath.Join(dataDir, "tmp"), 0o755)
	_ = os.MkdirAll(filepath.Join(dataDir, "logs"), 0o755)

	EnsureDatabaseConfigFile()
}

func DatabaseConfigFilePath() string {
	if configured := strings.TrimSpace(os.Getenv("PTNEXUS_DB_CONFIG_FILE")); configured != "" {
		return configured
	}
	dataDir := strings.TrimSpace(os.Getenv("PTNEXUS_DATA_DIR"))
	if dataDir == "" {
		configRoot, err := os.UserConfigDir()
		if err != nil || strings.TrimSpace(configRoot) == "" {
			configRoot = "."
		}
		dataDir = filepath.Join(configRoot, "pt-nexus", "data")
	}
	return filepath.Join(dataDir, "database.json")
}

func EnsureDatabaseConfigFile() {
	path := DatabaseConfigFilePath()
	if strings.TrimSpace(path) == "" {
		return
	}
	if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
		return
	}

	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	// 默认保持 sqlite：桌面开发阶段可先用 sqlite；后续用户可改为 mysql/pg。
	template := `{
  "type": "sqlite",
  "sqlite_path": "",
  "mysql": {
    "host": "",
    "port": 3306,
    "database": "",
    "user": "",
    "password": ""
  },
  "postgresql": {
    "host": "",
    "port": 5432,
    "database": "",
    "user": "",
    "password": "",
    "sslmode": "disable"
  }
}
`
	_ = os.WriteFile(path, []byte(template), 0o644)
}

// initDesktopLogging 初始化 server-go 的文件日志，确保桌面版可追溯启动期临时密码。
func initDesktopLogging() {
	desktopLogInitOnce.Do(func() {
		if err := logx.InitFromEnv(); err != nil {
			// 失败时降级为默认输出，避免影响应用启动。
			fmt.Printf("PT Nexus 日志初始化失败: %v\n", err)
			return
		}

		// 触发一次写入，确保日志文件实际创建，方便用户定位（尤其是 GUI 场景无控制台输出）。
		logx.Infof(
			"启动",
			"PT Nexus 桌面日志初始化完成 log_dir=%s data_dir=%s runtime=%s db_type=%s db_config_file=%s",
			logx.GetLogDir(),
			strings.TrimSpace(os.Getenv("PTNEXUS_DATA_DIR")),
			strings.TrimSpace(os.Getenv("PTNEXUS_RUNTIME")),
			strings.TrimSpace(os.Getenv("DB_TYPE")),
			strings.TrimSpace(os.Getenv("PTNEXUS_DB_CONFIG_FILE")),
		)
	})
}
