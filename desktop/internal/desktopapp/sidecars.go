package desktopapp

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Sidecar 描述一个随桌面端启动的后台进程（server / updater）。
type Sidecar struct {
	Name    string
	BaseURL string
	cmd     *exec.Cmd
}

// Stop 尝试停止 sidecar 子进程（若由当前进程启动）。
func (s *Sidecar) Stop() {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Kill()
	_, _ = s.cmd.Process.Wait()
}

// StartServerSidecar 启动 server 后端进程（监听 127.0.0.1:5275）。
// 说明：桌面端与 server 的交互通过 Wails 绑定方法代理到该进程，避免前端直连 HTTP API。
func StartServerSidecar(env DesktopRuntimeEnv) (*Sidecar, error) {
	addr := "127.0.0.1:5275"
	baseURL := "http://" + addr

	if isHTTPReady(addr) {
		return &Sidecar{Name: "server", BaseURL: baseURL}, nil
	}

	cmd, err := buildServerSidecarCommand(env)
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start server failed: %w", err)
	}

	if err := waitHTTPReady(addr, 12*time.Second); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, fmt.Errorf("server health check failed: %w", err)
	}

	return &Sidecar{Name: "server", BaseURL: baseURL, cmd: cmd}, nil
}

// StartUpdaterSidecar 启动 updater 更新进程（监听 127.0.0.1:5274）。
func StartUpdaterSidecar(env DesktopRuntimeEnv) (*Sidecar, error) {
	addr := "127.0.0.1:5274"
	baseURL := "http://" + addr

	if isHTTPReady(addr) {
		return &Sidecar{Name: "updater", BaseURL: baseURL}, nil
	}

	cmd, err := buildUpdaterSidecarCommand(env)
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start updater failed: %w", err)
	}

	if err := waitHTTPReady(addr, 10*time.Second); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, fmt.Errorf("updater health check failed: %w", err)
	}

	return &Sidecar{Name: "updater", BaseURL: baseURL, cmd: cmd}, nil
}

func buildServerSidecarCommand(env DesktopRuntimeEnv) (*exec.Cmd, error) {
	serverPath, serverArgs, serverDir, err := resolveServerCommand()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(serverPath, serverArgs...)
	if serverDir != "" {
		cmd.Dir = serverDir
	}

	cmd.Env = append(os.Environ(),
		"SERVER_HOST=127.0.0.1",
		"SERVER_PORT=5275",
		"PTNEXUS_BASE_DIR="+strings.TrimSpace(env.ResourceDir),
		"PTNEXUS_DATA_DIR="+strings.TrimSpace(env.DataDir),
		"PTNEXUS_DB_CONFIG_FILE="+strings.TrimSpace(env.DBConfigFile),
		"PTNEXUS_LOG_DIR="+strings.TrimSpace(env.LogDir),
		"PTNEXUS_STATIC_DIR="+strings.TrimSpace(env.StaticDir),
		"PTNEXUS_SITES_DATA_FILE="+strings.TrimSpace(env.SitesDataFile),
		"PTNEXUS_GLOBAL_MAPPINGS="+strings.TrimSpace(env.GlobalMapYML),
		"PTNEXUS_RUNTIME=desktop",
	)
	configureCommandForPlatform(cmd)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd, nil
}

func buildUpdaterSidecarCommand(env DesktopRuntimeEnv) (*exec.Cmd, error) {
	updaterPath, updaterArgs, updaterDir, err := resolveUpdaterCommand()
	if err != nil {
		return nil, err
	}

	updateDir := strings.TrimSpace(os.Getenv("UPDATE_DIR"))
	if updateDir == "" {
		updateDir = filepath.Join(env.DataDir, "updates")
	}
	repoDir := strings.TrimSpace(os.Getenv("REPO_DIR"))
	if repoDir == "" {
		repoDir = filepath.Join(updateDir, "repo")
	}

	localChangelog := strings.TrimSpace(os.Getenv("LOCAL_CONFIG_FILE"))
	if localChangelog == "" {
		// 生产环境：优先取资源目录内 CHANGELOG.json；开发环境下可由 updater 内部 DEV_ENV 逻辑兜底。
		localChangelog = filepath.Join(env.ResourceDir, "CHANGELOG.json")
	}

	cmd := exec.Command(updaterPath, updaterArgs...)
	if updaterDir != "" {
		cmd.Dir = updaterDir
	}

	cmd.Env = append(os.Environ(),
		"UPDATER_PORT=5274",
		"SERVER_PORT=5275",
		"BATCH_PORT=5276",
		"UPDATE_DIR="+updateDir,
		"REPO_DIR="+repoDir,
		"LOCAL_CONFIG_FILE="+localChangelog,
		"PTNEXUS_BASE_DIR="+strings.TrimSpace(env.ResourceDir),
		"PTNEXUS_DATA_DIR="+strings.TrimSpace(env.DataDir),
		"PTNEXUS_LOG_DIR="+strings.TrimSpace(env.LogDir),
		"PTNEXUS_RUNTIME=desktop",
	)
	configureCommandForPlatform(cmd)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd, nil
}

func resolveServerCommand() (path string, args []string, dir string, err error) {
	if explicit := strings.TrimSpace(os.Getenv("PTNEXUS_SERVER_PATH")); explicit != "" {
		return explicit, nil, "", nil
	}

	name := "server"
	if runtime.GOOS == "windows" {
		name = "server.exe"
	}

	candidates := make([]string, 0, 6)

	if exePath, exeErr := os.Executable(); exeErr == nil && strings.TrimSpace(exePath) != "" {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates, filepath.Join(exeDir, name))
	}

	if cwd, cwdErr := os.Getwd(); cwdErr == nil && strings.TrimSpace(cwd) != "" {
		candidates = append(candidates, filepath.Join(cwd, name))
		candidates = append(candidates, filepath.Join(cwd, "build", "windows", "sidecar", name))
	}

	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate, nil, "", nil
		}
	}

	// 开发环境：允许直接 go run（避免强依赖本机提前构建 sidecar）。
	if cwd, cwdErr := os.Getwd(); cwdErr == nil && strings.TrimSpace(cwd) != "" {
		serverDir := filepath.Clean(filepath.Join(cwd, "..", "server"))
		if fileExists(filepath.Join(serverDir, "cmd", "server", "main.go")) {
			return "go", []string{"run", "./cmd/server"}, serverDir, nil
		}
	}

	return "", nil, "", fmt.Errorf("server binary not found; set PTNEXUS_SERVER_PATH or provide %s beside app", name)
}

func resolveUpdaterCommand() (path string, args []string, dir string, err error) {
	if explicit := strings.TrimSpace(os.Getenv("PTNEXUS_UPDATER_PATH")); explicit != "" {
		return explicit, nil, "", nil
	}

	name := "updater"
	if runtime.GOOS == "windows" {
		name = "updater.exe"
	}

	candidates := make([]string, 0, 6)

	if exePath, exeErr := os.Executable(); exeErr == nil && strings.TrimSpace(exePath) != "" {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates, filepath.Join(exeDir, name))
	}

	if cwd, cwdErr := os.Getwd(); cwdErr == nil && strings.TrimSpace(cwd) != "" {
		candidates = append(candidates, filepath.Join(cwd, name))
		candidates = append(candidates, filepath.Join(cwd, "build", "windows", "sidecar", name))
	}

	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate, nil, "", nil
		}
	}

	// 开发环境：允许 go run updater/updater.go。
	if cwd, cwdErr := os.Getwd(); cwdErr == nil && strings.TrimSpace(cwd) != "" {
		updaterDir := filepath.Clean(filepath.Join(cwd, "..", "updater"))
		if fileExists(filepath.Join(updaterDir, "updater.go")) {
			return "go", []string{"run", "./updater.go"}, updaterDir, nil
		}
	}

	return "", nil, "", fmt.Errorf("updater binary not found; set PTNEXUS_UPDATER_PATH or provide %s beside app", name)
}

func isHTTPReady(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()

	client := &http.Client{Timeout: 900 * time.Millisecond}
	resp, err := client.Get("http://" + addr + "/health")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}

func waitHTTPReady(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isHTTPReady(addr) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", addr)
}
