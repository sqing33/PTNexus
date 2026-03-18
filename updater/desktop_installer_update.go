package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	updateModeRuntimeInstall    = "runtime_install"
	updateModeInstallerDownload = "installer_download"

	desktopInstallerPlatformWindows = "windows-desktop"
	desktopInstallerKindPatch       = "patch"
	desktopInstallerKindFull        = "full"
)

type PreparedDesktopInstaller struct {
	Version      string    `json:"version"`
	Kind         string    `json:"kind,omitempty"`
	FilePath     string    `json:"file_path,omitempty"`
	FileName     string    `json:"file_name,omitempty"`
	SHA256       string    `json:"sha256,omitempty"`
	Size         int64     `json:"size,omitempty"`
	DownloadedAt time.Time `json:"downloaded_at"`
}

func isWindowsDesktopRuntime() bool {
	return runtime.GOOS == "windows" && strings.EqualFold(strings.TrimSpace(getEnv("PTNEXUS_RUNTIME", "")), "desktop")
}

func normalizeDesktopInstallerPlatform(platform string) string {
	clean := strings.ToLower(strings.TrimSpace(platform))
	if clean == "" {
		return desktopInstallerPlatformWindows
	}
	return clean
}

func normalizeDesktopInstallerKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", desktopInstallerKindFull:
		return desktopInstallerKindFull
	case desktopInstallerKindPatch:
		return desktopInstallerKindPatch
	default:
		return desktopInstallerKindFull
	}
}

func selectDesktopInstaller(installers []DesktopInstallerAsset, goarch string) (DesktopInstallerAsset, error) {
	archWant := strings.ToLower(strings.TrimSpace(getEnv("UPDATE_ARCH", goarch)))
	var matched []DesktopInstallerAsset

	for _, item := range installers {
		if normalizeDesktopInstallerPlatform(item.Platform) != desktopInstallerPlatformWindows {
			continue
		}
		if strings.ToLower(strings.TrimSpace(item.Arch)) != archWant {
			continue
		}
		item.Kind = normalizeDesktopInstallerKind(item.Kind)
		if strings.TrimSpace(item.FileName) == "" {
			item.FileName = bundleFileNameFromURL(item.URL)
		}
		matched = append(matched, item)
	}

	for _, kind := range []string{desktopInstallerKindPatch, desktopInstallerKindFull} {
		for _, item := range matched {
			if item.Kind == kind {
				return item, nil
			}
		}
	}

	available := make([]string, 0, len(installers))
	for _, item := range installers {
		available = append(
			available,
			fmt.Sprintf(
				"%s/%s/%s",
				normalizeDesktopInstallerPlatform(item.Platform),
				strings.TrimSpace(item.Arch),
				normalizeDesktopInstallerKind(item.Kind),
			),
		)
	}
	return DesktopInstallerAsset{}, fmt.Errorf("未找到匹配当前平台的桌面安装包 (want=%s/%s, available=%s)", desktopInstallerPlatformWindows, archWant, strings.Join(available, ", "))
}

func desktopInstallerDownloadCandidates(installer DesktopInstallerAsset) []string {
	items := make([]string, 0, 1+len(installer.MirrorURLs))
	items = append(items, installer.URL)
	items = append(items, installer.MirrorURLs...)
	return normalizeURLCandidates(items...)
}

func resolveDesktopInstallerForCurrentPlatform(manifest *UpdateManifest) (DesktopInstallerAsset, error) {
	if manifest == nil {
		return DesktopInstallerAsset{}, errors.New("manifest is nil")
	}

	installer, err := selectDesktopInstaller(manifest.Latest.DesktopInstallers, runtime.GOARCH)
	if err != nil {
		return DesktopInstallerAsset{}, err
	}

	candidates := desktopInstallerDownloadCandidates(installer)
	if len(candidates) == 0 {
		return DesktopInstallerAsset{}, errors.New("desktop installer url 与 mirror_urls 均为空")
	}
	if strings.TrimSpace(installer.SHA256) == "" && !isTruthy(getEnv("UPDATE_SKIP_VERIFY", "false")) {
		return DesktopInstallerAsset{}, errors.New("desktop installer sha256 为空")
	}

	for _, raw := range candidates {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || strings.TrimSpace(raw) == "" {
			return DesktopInstallerAsset{}, fmt.Errorf("desktop installer 下载地址无效: %q", raw)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return DesktopInstallerAsset{}, fmt.Errorf("desktop installer scheme 不支持: %q", raw)
		}
	}

	if strings.TrimSpace(installer.FileName) == "" {
		installer.FileName = bundleFileNameFromURL(candidates[0])
	}
	installer.Kind = normalizeDesktopInstallerKind(installer.Kind)
	return installer, nil
}

func downloadLatestDesktopInstaller(ctx context.Context, manifest *UpdateManifest) (*PreparedDesktopInstaller, error) {
	if manifest == nil {
		return nil, errors.New("manifest is nil")
	}

	remoteVersion := strings.TrimSpace(manifest.Latest.Version)
	localVersion := getLocalVersion()
	if !isNewerVersion(remoteVersion, localVersion) {
		return &PreparedDesktopInstaller{Version: remoteVersion, DownloadedAt: time.Now()}, nil
	}

	installer, err := resolveDesktopInstallerForCurrentPlatform(manifest)
	if err != nil {
		return nil, err
	}

	timeoutStr := getEnv("UPDATE_DOWNLOAD_TIMEOUT", "20m")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil || timeout <= 0 {
		timeout = 20 * time.Minute
	}
	timeout = computeDownloadTimeout(timeout, installer.Size)

	candidates := desktopInstallerDownloadCandidates(installer)
	probeResults, err := rankProbeCandidates(ctx, candidates, 6*time.Second)
	if err != nil {
		return nil, err
	}

	versionToken := sanitizePathToken(remoteVersion)
	downloadRoot := filepath.Join(updateDir, "downloads", "installers", versionToken)

	var (
		downloadPath string
		fileName     string
		gotSHA       string
		chosenURL    string
		downloadErrs []string
	)

	for _, probe := range probeResults {
		fileName = strings.TrimSpace(installer.FileName)
		if fileName == "" {
			fileName = bundleFileNameFromURL(probe.URL)
		}
		downloadPath = filepath.Join(downloadRoot, fileName)

		log.Printf("开始下载桌面安装包: version=%s kind=%s arch=%s url=%s", remoteVersion, installer.Kind, installer.Arch, probe.URL)
		gotSHA, err = downloadWithSHA256(ctx, probe.URL, downloadPath, installer.SHA256, timeout, defaultDownloadIdleTimeout)
		if err == nil {
			chosenURL = probe.URL
			break
		}
		downloadErrs = append(downloadErrs, fmt.Sprintf("%s -> %v", probe.URL, err))
		log.Printf("桌面安装包下载失败，准备切换下一个源: url=%s err=%v", probe.URL, err)
	}

	if chosenURL == "" {
		return nil, fmt.Errorf("所有桌面安装包下载源均失败: %s", strings.Join(downloadErrs, "; "))
	}

	return &PreparedDesktopInstaller{
		Version:      remoteVersion,
		Kind:         installer.Kind,
		FilePath:     downloadPath,
		FileName:     fileName,
		SHA256:       gotSHA,
		Size:         installer.Size,
		DownloadedAt: time.Now(),
	}, nil
}
