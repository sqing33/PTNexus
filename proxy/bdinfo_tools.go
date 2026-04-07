package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func resolveBDInfoBinaryPath() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("PTNEXUS_BDINFO_PATH")); configured != "" {
		if _, err := os.Stat(configured); err == nil {
			return configured, nil
		}
		return "", fmt.Errorf("PTNEXUS_BDINFO_PATH points to a missing file: %s", configured)
	}

	isWindows := runtime.GOOS == "windows"
	platformDir := "linux"
	binaryCandidates := []string{"BDInfo"}
	if isWindows {
		platformDir = "windows"
		binaryCandidates = []string{"BDInfo.exe", "BDInfo"}
	}

	candidateDirs := make([]string, 0)
	seenDirs := map[string]struct{}{}
	addDir := func(dir string) {
		trimmed := strings.TrimSpace(dir)
		if trimmed == "" {
			return
		}
		cleaned := filepath.Clean(trimmed)
		if _, exists := seenDirs[cleaned]; exists {
			return
		}
		seenDirs[cleaned] = struct{}{}
		candidateDirs = append(candidateDirs, cleaned)
	}

	if dir := strings.TrimSpace(os.Getenv("PTNEXUS_BDINFO_DIR")); dir != "" {
		addDir(dir)
		addDir(filepath.Join(dir, platformDir))
	}

	if baseDir := strings.TrimSpace(os.Getenv("PTNEXUS_BASE_DIR")); baseDir != "" {
		addDir(filepath.Join(baseDir, "bdinfo", platformDir))
		addDir(filepath.Join(baseDir, "bdinfo"))
		addDir(filepath.Join(baseDir, "server", "bdinfo", platformDir))
		addDir(filepath.Join(baseDir, "server", "bdinfo"))
	}

	if baseDir := resolveRuntimeBaseDir(); strings.TrimSpace(baseDir) != "" {
		addDir(filepath.Join(baseDir, "bdinfo", platformDir))
		addDir(filepath.Join(baseDir, "bdinfo"))
		addDir(filepath.Join(baseDir, "server", "bdinfo", platformDir))
		addDir(filepath.Join(baseDir, "server", "bdinfo"))
	}

	if cwd, err := os.Getwd(); err == nil {
		roots := []string{cwd, filepath.Dir(cwd), filepath.Dir(filepath.Dir(cwd))}
		for _, root := range roots {
			addDir(filepath.Join(root, "server", "bdinfo", platformDir))
			addDir(filepath.Join(root, "server", "bdinfo"))
			addDir(filepath.Join(root, "bdinfo", platformDir))
			addDir(filepath.Join(root, "bdinfo"))
		}
	}

	addDir(filepath.Join("/app/server/bdinfo", platformDir))
	addDir("/app/server/bdinfo")
	addDir(filepath.Join("/app/bdinfo", platformDir))
	addDir("/app/bdinfo")

	tried := make([]string, 0)
	for _, dir := range candidateDirs {
		for _, name := range binaryCandidates {
			candidate := filepath.Join(dir, name)
			tried = append(tried, candidate)
			if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
				return candidate, nil
			}
		}
	}

	lookPathNames := []string{"BDInfo", "bdinfo"}
	if isWindows {
		lookPathNames = []string{"BDInfo.exe", "BDInfo", "bdinfo"}
	}
	for _, name := range lookPathNames {
		if found, err := exec.LookPath(name); err == nil {
			return found, nil
		}
	}

	if len(tried) > 0 {
		return "", fmt.Errorf("BDInfo executable not found; configure PTNEXUS_BDINFO_PATH or PTNEXUS_BDINFO_DIR (checked: %s)", strings.Join(tried, ", "))
	}
	return "", fmt.Errorf("BDInfo executable not found; configure PTNEXUS_BDINFO_PATH or PTNEXUS_BDINFO_DIR")
}

func resolveBDInfoDataSubstractorPath(bdinfoPath string) string {
	dir := strings.TrimSpace(filepath.Dir(strings.TrimSpace(bdinfoPath)))
	if dir == "" {
		return ""
	}

	candidates := []string{"BDInfoDataSubstractor"}
	if runtime.GOOS == "windows" {
		candidates = []string{"BDInfoDataSubstractor.exe", "BDInfoDataSubstractor"}
	}

	for _, name := range candidates {
		candidate := filepath.Join(dir, name)
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			return candidate
		}
	}
	return ""
}
