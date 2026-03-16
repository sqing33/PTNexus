//go:build linux

package media

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
)

var linuxISOMountMutex sync.Mutex

func openISOSession(isoPath string, scene string) (*MediaSession, error) {
	trimmedISOPath := strings.TrimSpace(isoPath)
	info, err := os.Stat(trimmedISOPath)
	if err != nil {
		return nil, fmt.Errorf("访问 ISO 文件失败: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("ISO 路径必须是文件: %s", trimmedISOPath)
	}

	mountBin, err := exec.LookPath("mount")
	if err != nil {
		return nil, fmt.Errorf("未找到 mount 命令，请安装 util-linux 后重试")
	}
	umountBin, err := exec.LookPath("umount")
	if err != nil {
		return nil, fmt.Errorf("未找到 umount 命令，请安装 util-linux 后重试")
	}

	mountRoot := resolveISOMountRoot()
	if mkErr := os.MkdirAll(mountRoot, 0o755); mkErr != nil {
		return nil, fmt.Errorf("创建 ISO 挂载根目录失败: %w", mkErr)
	}

	linuxISOMountMutex.Lock()
	defer linuxISOMountMutex.Unlock()

	mountDir, err := os.MkdirTemp(mountRoot, "iso-*")
	if err != nil {
		return nil, fmt.Errorf("创建 ISO 挂载目录失败: %w", err)
	}

	startedAt := time.Now()
	cmd := exec.Command(mountBin, "-o", "loop,ro,nosuid,nodev,noexec", trimmedISOPath, mountDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(mountDir)
		text := strings.TrimSpace(string(output))
		if text == "" {
			text = err.Error()
		}
		return nil, fmt.Errorf("ISO 挂载失败，请确认当前进程具备 mount 权限且容器已开启 SYS_ADMIN/loop 设备支持: %s", text)
	}

	logx.Infof(isoSessionLogModule, "挂载成功 scene=%s iso=%s mount_dir=%s elapsed_ms=%d", scene, trimmedISOPath, mountDir, time.Since(startedAt).Milliseconds())

	session := &MediaSession{
		OriginalPath: trimmedISOPath,
		ResolvedPath: mountDir,
		Mounted:      true,
		OwnedMount:   true,
	}
	session.closeFn = func() error {
		closeStartedAt := time.Now()
		unmountCmd := exec.Command(umountBin, mountDir)
		unmountOutput, unmountErr := unmountCmd.CombinedOutput()
		removeErr := os.RemoveAll(mountDir)
		if unmountErr == nil && removeErr == nil {
			logx.Infof(isoSessionLogModule, "卸载成功 scene=%s iso=%s mount_dir=%s elapsed_ms=%d", scene, trimmedISOPath, mountDir, time.Since(closeStartedAt).Milliseconds())
			return nil
		}

		errParts := make([]string, 0, 2)
		if unmountErr != nil {
			text := strings.TrimSpace(string(unmountOutput))
			if text == "" {
				text = unmountErr.Error()
			}
			errParts = append(errParts, "卸载失败: "+text)
		}
		if removeErr != nil {
			errParts = append(errParts, "清理挂载目录失败: "+removeErr.Error())
		}
		finalErr := fmt.Errorf(strings.Join(errParts, "；"))
		logx.Warnf(isoSessionLogModule, "卸载失败 scene=%s iso=%s mount_dir=%s err=%v", scene, trimmedISOPath, filepath.Clean(mountDir), finalErr)
		return finalErr
	}
	return session, nil
}
