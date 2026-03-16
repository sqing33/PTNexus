//go:build windows

package media

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
)

var windowsISOMountMutex sync.Mutex

type windowsDiskImageState struct {
	Attached    bool   `json:"attached"`
	DriveLetter string `json:"drive_letter"`
}

func openISOSession(isoPath string, scene string) (*MediaSession, error) {
	trimmedISOPath := strings.TrimSpace(isoPath)
	info, err := os.Stat(trimmedISOPath)
	if err != nil {
		return nil, fmt.Errorf("访问 ISO 文件失败: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("ISO 路径必须是文件: %s", trimmedISOPath)
	}

	if _, err := exec.LookPath("powershell.exe"); err != nil {
		return nil, fmt.Errorf("未找到 powershell.exe，无法在 Windows 上自动挂载 ISO")
	}

	windowsISOMountMutex.Lock()
	defer windowsISOMountMutex.Unlock()

	state, err := queryWindowsDiskImageState(trimmedISOPath)
	if err != nil {
		return nil, err
	}
	if state.Attached && strings.TrimSpace(state.DriveLetter) != "" {
		drivePath := windowsDriveRoot(state.DriveLetter)
		logx.Infof(isoSessionLogModule, "复用已挂载 ISO scene=%s iso=%s drive=%s", scene, trimmedISOPath, drivePath)
		return &MediaSession{
			OriginalPath: trimmedISOPath,
			ResolvedPath: drivePath,
			Mounted:      true,
			OwnedMount:   false,
			closeFn:      func() error { return nil },
		}, nil
	}

	startedAt := time.Now()
	if _, err := runWindowsPowerShell(fmt.Sprintf("Mount-DiskImage -ImagePath '%s' -StorageType ISO -Access ReadOnly -ErrorAction Stop | Out-Null", escapePowerShellLiteral(trimmedISOPath))); err != nil {
		return nil, fmt.Errorf("Windows ISO 挂载失败: %w", err)
	}

	state, err = waitWindowsDiskImageReady(trimmedISOPath)
	if err != nil {
		return nil, err
	}
	drivePath := windowsDriveRoot(state.DriveLetter)
	logx.Infof(isoSessionLogModule, "挂载成功 scene=%s iso=%s drive=%s elapsed_ms=%d", scene, trimmedISOPath, drivePath, time.Since(startedAt).Milliseconds())

	session := &MediaSession{
		OriginalPath: trimmedISOPath,
		ResolvedPath: drivePath,
		Mounted:      true,
		OwnedMount:   true,
	}
	session.closeFn = func() error {
		closeStartedAt := time.Now()
		if _, err := runWindowsPowerShell(fmt.Sprintf("Dismount-DiskImage -ImagePath '%s' -ErrorAction Stop | Out-Null", escapePowerShellLiteral(trimmedISOPath))); err != nil {
			logx.Warnf(isoSessionLogModule, "卸载失败 scene=%s iso=%s err=%v", scene, trimmedISOPath, err)
			return fmt.Errorf("Windows ISO 卸载失败: %w", err)
		}
		logx.Infof(isoSessionLogModule, "卸载成功 scene=%s iso=%s drive=%s elapsed_ms=%d", scene, trimmedISOPath, drivePath, time.Since(closeStartedAt).Milliseconds())
		return nil
	}
	return session, nil
}

func waitWindowsDiskImageReady(isoPath string) (windowsDiskImageState, error) {
	var lastState windowsDiskImageState
	for attempt := 0; attempt < 10; attempt++ {
		state, err := queryWindowsDiskImageState(isoPath)
		if err == nil && state.Attached && strings.TrimSpace(state.DriveLetter) != "" {
			return state, nil
		}
		if err == nil {
			lastState = state
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !lastState.Attached {
		return windowsDiskImageState{}, fmt.Errorf("Windows ISO 已请求挂载，但系统未返回盘符")
	}
	return windowsDiskImageState{}, fmt.Errorf("Windows ISO 已挂载，但未获取到可用盘符")
}

func queryWindowsDiskImageState(isoPath string) (windowsDiskImageState, error) {
	script := fmt.Sprintf(`
$img = Get-DiskImage -ImagePath '%s' -ErrorAction SilentlyContinue
if ($null -eq $img) {
  @{ attached = $false; drive_letter = '' } | ConvertTo-Json -Compress
  exit 0
}
$letter = ''
try {
  $vol = $img | Get-Volume -ErrorAction SilentlyContinue | Where-Object { $_.DriveLetter } | Select-Object -First 1
  if ($null -ne $vol) {
    $letter = [string]$vol.DriveLetter
  }
} catch {
}
@{ attached = [bool]$img.Attached; drive_letter = $letter } | ConvertTo-Json -Compress
`, escapePowerShellLiteral(isoPath))
	output, err := runWindowsPowerShell(script)
	if err != nil {
		return windowsDiskImageState{}, err
	}
	state := windowsDiskImageState{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &state); err != nil {
		return windowsDiskImageState{}, fmt.Errorf("解析 Windows ISO 状态失败: %w", err)
	}
	return state, nil
}

func runWindowsPowerShell(script string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Errorf(text)
	}
	return text, nil
}

func windowsDriveRoot(letter string) string {
	trimmed := strings.Trim(strings.TrimSpace(letter), ":\\/")
	return strings.ToUpper(trimmed) + ":\\"
}

func escapePowerShellLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
