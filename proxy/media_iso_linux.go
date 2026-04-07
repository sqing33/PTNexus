//go:build linux

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var linuxISOMountMutex sync.Mutex

func openISOSession(isoPath string, scene string) (*MediaSession, error) {
	trimmedISOPath := normalizePath(isoPath)
	info, err := os.Stat(trimmedISOPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access ISO file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("ISO path must be a file: %s", trimmedISOPath)
	}

	mountBin, err := exec.LookPath("mount")
	if err != nil {
		return nil, fmt.Errorf("mount command not found; install util-linux first")
	}
	umountBin, err := exec.LookPath("umount")
	if err != nil {
		return nil, fmt.Errorf("umount command not found; install util-linux first")
	}

	mountRoot := resolveISOMountRoot()
	if mkErr := os.MkdirAll(mountRoot, 0o755); mkErr != nil {
		return nil, fmt.Errorf("failed to create ISO mount root: %w", mkErr)
	}

	linuxISOMountMutex.Lock()
	defer linuxISOMountMutex.Unlock()

	mountDir, err := os.MkdirTemp(mountRoot, "iso-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create ISO mount directory: %w", err)
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
		return nil, fmt.Errorf("failed to mount ISO; confirm mount privileges are available (%s). detail: %s", buildLinuxDockerISOMountHint(), text)
	}

	log.Printf("%s: mounted ISO iso=%s mount_dir=%s elapsed_ms=%d", scene, trimmedISOPath, mountDir, time.Since(startedAt).Milliseconds())

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
			log.Printf("%s: unmounted ISO iso=%s mount_dir=%s elapsed_ms=%d", scene, trimmedISOPath, mountDir, time.Since(closeStartedAt).Milliseconds())
			return nil
		}

		errParts := make([]string, 0, 2)
		if unmountErr != nil {
			text := strings.TrimSpace(string(unmountOutput))
			if text == "" {
				text = unmountErr.Error()
			}
			errParts = append(errParts, "unmount failed: "+text)
		}
		if removeErr != nil {
			errParts = append(errParts, "mount directory cleanup failed: "+removeErr.Error())
		}
		finalErr := fmt.Errorf("%s", strings.Join(errParts, "; "))
		log.Printf("%s: failed to unmount ISO iso=%s mount_dir=%s err=%v", scene, trimmedISOPath, filepath.Clean(mountDir), finalErr)
		return finalErr
	}
	return session, nil
}
