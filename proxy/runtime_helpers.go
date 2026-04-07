package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func resolveRuntimeBaseDir() string {
	if configured := strings.TrimSpace(os.Getenv("PTNEXUS_BASE_DIR")); configured != "" {
		return filepath.Clean(configured)
	}

	if cwd, err := os.Getwd(); err == nil {
		cleaned := filepath.Clean(cwd)
		if looksLikeProxyRuntimeRoot(cleaned) {
			return cleaned
		}
	}

	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		if strings.TrimSpace(exeDir) != "" {
			return filepath.Clean(exeDir)
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		return filepath.Clean(cwd)
	}
	return "."
}

func resolveRuntimeDataDir() string {
	if configured := strings.TrimSpace(os.Getenv("PTNEXUS_DATA_DIR")); configured != "" {
		return filepath.Clean(configured)
	}
	return filepath.Join(resolveRuntimeBaseDir(), "runtime")
}

func looksLikeProxyRuntimeRoot(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}

	candidates := []string{
		filepath.Join(dir, "proxy.go"),
		filepath.Join(dir, "bdinfo.go"),
		filepath.Join(dir, "start.sh"),
		filepath.Join(dir, "start.ps1"),
		filepath.Join(dir, "bdinfo"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return true
		}
	}
	return false
}

func resolveISOMountRoot() string {
	if configured := strings.TrimSpace(os.Getenv("PTNEXUS_ISO_MOUNT_ROOT")); configured != "" {
		return filepath.Clean(configured)
	}
	return filepath.Join(resolveRuntimeDataDir(), "tmp", "iso-mounts")
}

func buildLinuxDockerISOMountHint() string {
	return "when running inside native Linux Docker, set PTNEXUS_ISO_MOUNT_ROOT=/app/data/tmp/iso-mounts and grant mount privileges such as SYS_ADMIN and loop devices; Docker Desktop / WSL does not support mounting ISO inside the container"
}

func isExplicitCommandPath(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false
	}
	if filepath.IsAbs(trimmed) {
		return true
	}
	if strings.ContainsAny(trimmed, `/\`) {
		return true
	}
	return filepath.VolumeName(trimmed) != ""
}

func toolEnvKey(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "mediainfo":
		return "PTNEXUS_MEDIAINFO_PATH"
	case "ffmpeg":
		return "PTNEXUS_FFMPEG_PATH"
	case "ffprobe":
		return "PTNEXUS_FFPROBE_PATH"
	case "mpv":
		return "PTNEXUS_MPV_PATH"
	default:
		return ""
	}
}

func resolveCommandPath(defaultName, envKey string) (string, error) {
	trimmedName := strings.TrimSpace(defaultName)
	if trimmedName == "" {
		return "", fmt.Errorf("command name is empty")
	}
	if isExplicitCommandPath(trimmedName) {
		return trimmedName, nil
	}
	if strings.TrimSpace(envKey) != "" {
		if configured := strings.TrimSpace(os.Getenv(envKey)); configured != "" {
			stat, err := os.Stat(configured)
			if err != nil {
				return "", fmt.Errorf("%s points to a missing file: %s", envKey, configured)
			}
			if stat.IsDir() {
				return "", fmt.Errorf("%s points to a directory instead of an executable: %s", envKey, configured)
			}
			return configured, nil
		}
	}
	path, err := exec.LookPath(trimmedName)
	if err != nil {
		return "", fmt.Errorf("executable not found: %s", trimmedName)
	}
	return path, nil
}

func resolveToolCommandPath(name string) (string, error) {
	return resolveCommandPath(name, toolEnvKey(name))
}

func extractMediaInfo(targetFile string) (string, error) {
	if output, err := executeCommandWithTimeout(10*time.Minute, "mediainfo", "--Output=text", targetFile); err == nil && strings.TrimSpace(output) != "" {
		return strings.TrimSpace(output), nil
	}
	if output, err := executeCommandWithTimeout(10*time.Minute, "ffprobe", "-hide_banner", "-i", targetFile); err == nil && strings.TrimSpace(output) != "" {
		return strings.TrimSpace(output), nil
	}
	return "", fmt.Errorf("failed to extract media info; install mediainfo or ffprobe, or configure PTNEXUS_MEDIAINFO_PATH / PTNEXUS_FFPROBE_PATH")
}
