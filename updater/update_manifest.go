package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// UpdateManifest describes the remote, signed-off update metadata.
//
// Phase 1 goal: map "latest" -> downloadable runtime bundle (artifact) per OS/ARCH.
// This allows online update without git.
type UpdateManifest struct {
	Schema  int            `json:"schema"`
	Latest  ManifestLatest `json:"latest"`
	History []VersionInfo  `json:"history"`
}

type ManifestLatest struct {
	Version           string                  `json:"version"`
	Date              string                  `json:"date,omitempty"`
	ForceUpdate       bool                    `json:"force_update,omitempty"`
	DisableUpdate     bool                    `json:"disable_update,omitempty"`
	Note              string                  `json:"note,omitempty"`
	Artifacts         []UpdateArtifact        `json:"artifacts"`
	DesktopInstallers []DesktopInstallerAsset `json:"desktop_installers,omitempty"`
}

type UpdateArtifact struct {
	OS         string   `json:"os"`
	Arch       string   `json:"arch"`
	URL        string   `json:"url"`
	MirrorURLs []string `json:"mirror_urls,omitempty"`
	SHA256     string   `json:"sha256"`
	Size       int64    `json:"size,omitempty"`
	Format     string   `json:"format,omitempty"` // "tar.gz" (default) | "zip"
}

type DesktopInstallerAsset struct {
	Platform   string   `json:"platform,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Arch       string   `json:"arch"`
	FileName   string   `json:"file_name,omitempty"`
	URL        string   `json:"url"`
	MirrorURLs []string `json:"mirror_urls,omitempty"`
	SHA256     string   `json:"sha256"`
	Size       int64    `json:"size,omitempty"`
}

func newUpdateHTTPClient(timeout time.Duration) *http.Client {
	proxyFunc, err := buildUpdaterProxyFunc(loadUpdaterNetworkProxyConfig())
	if err != nil {
		log.Printf("网络代理配置无效，更新模块回退直连 err=%v", err)
		proxyFunc = func(req *http.Request) (*url.URL, error) {
			return nil, nil
		}
	}
	tr := &http.Transport{Proxy: proxyFunc}
	return &http.Client{Timeout: timeout, Transport: tr}
}

func fetchJSONWithContext(ctx context.Context, urlStr string, dst any, timeout time.Duration) error {
	client := newUpdateHTTPClient(timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("解析 JSON 失败: %w", err)
	}
	return nil
}

func fetchJSON(urlStr string, dst any) error {
	return fetchJSONWithContext(context.Background(), urlStr, dst, 15*time.Second)
}

func validateUpdateManifest(manifest *UpdateManifest) error {
	if manifest == nil {
		return fmt.Errorf("更新清单为空")
	}
	if strings.TrimSpace(manifest.Latest.Version) == "" {
		return fmt.Errorf("UPDATE_MANIFEST.json 缺少 latest.version")
	}
	if len(manifest.History) == 0 {
		return fmt.Errorf("UPDATE_MANIFEST.json 缺少 history")
	}
	latestHistory := manifest.History[0]
	if strings.TrimSpace(latestHistory.Version) == "" {
		return fmt.Errorf("UPDATE_MANIFEST.json 缺少 history[0].version")
	}
	if strings.TrimSpace(latestHistory.Version) != strings.TrimSpace(manifest.Latest.Version) {
		return fmt.Errorf("UPDATE_MANIFEST.json latest.version 与 history[0].version 不一致")
	}
	return nil
}

func validateUpdateManifestForMode(manifest *UpdateManifest, updateMode string) error {
	if err := validateUpdateManifest(manifest); err != nil {
		return err
	}
	switch strings.TrimSpace(updateMode) {
	case updateModeInstallerDownload:
		_, err := resolveDesktopInstallerForCurrentPlatform(manifest)
		if err != nil {
			return fmt.Errorf("桌面安装包信息不可用: %w", err)
		}
	default:
		_, err := resolveManifestArtifactForCurrentPlatform(manifest)
		if err != nil {
			return fmt.Errorf("运行时更新产物不可用: %w", err)
		}
	}
	return nil
}

func getRemoteManifestForMode(updateMode string, versionHints ...string) (*UpdateManifest, error) {
	cleanHints := make([]string, 0, len(versionHints))
	for _, hint := range versionHints {
		if v := strings.TrimSpace(hint); v != "" {
			cleanHints = append(cleanHints, v)
		}
	}

	validator := func(manifest *UpdateManifest) error {
		return validateUpdateManifestForMode(manifest, updateMode)
	}

	candidates := manifestCandidates()
	manifest, source, err := fetchJSONFromCandidates[UpdateManifest](
		context.Background(),
		candidates,
		15*time.Second,
		validator,
	)
	if err != nil && len(cleanHints) > 0 {
		manifest, source, err = fetchJSONFromCandidates[UpdateManifest](
			context.Background(),
			manifestVersionHintCandidates(cleanHints...),
			15*time.Second,
			validator,
		)
	}
	if err == nil && manifest != nil && len(cleanHints) > 0 {
		currentVersion := strings.TrimSpace(manifest.Latest.Version)
		needHintRefresh := currentVersion == ""
		if !needHintRefresh {
			for _, hint := range cleanHints {
				if !isNewerVersion(currentVersion, hint) {
					needHintRefresh = true
					break
				}
			}
		}
		if needHintRefresh {
			if hintedManifest, hintedSource, hintedErr := fetchJSONFromCandidates[UpdateManifest](
				context.Background(),
				manifestVersionHintCandidates(cleanHints...),
				15*time.Second,
				validator,
			); hintedErr == nil && hintedManifest != nil {
				hintedVersion := strings.TrimSpace(hintedManifest.Latest.Version)
				if currentVersion == "" || isNewerVersion(hintedVersion, currentVersion) {
					manifest = hintedManifest
					source = hintedSource
				}
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("获取 UPDATE_MANIFEST.json 失败: %w", err)
	}
	log.Printf("获取更新清单成功，使用源: %s (mode=%s)", source, strings.TrimSpace(updateMode))
	return manifest, nil
}

func getRemoteManifest(versionHints ...string) (*UpdateManifest, error) {
	return getRemoteManifestForMode(updateModeRuntimeInstall, versionHints...)
}
