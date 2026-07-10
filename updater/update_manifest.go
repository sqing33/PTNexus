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

type ManifestLookupDiagnostics struct {
	Strategy              string                     `json:"strategy,omitempty"`
	HintRefreshAttempted  bool                       `json:"hint_refresh_attempted,omitempty"`
	HintRefreshApplied    bool                       `json:"hint_refresh_applied,omitempty"`
	ForwardProbeAttempted bool                       `json:"forward_probe_attempted,omitempty"`
	ForwardProbeApplied   bool                       `json:"forward_probe_applied,omitempty"`
	ManifestSource        string                     `json:"manifest_source,omitempty"`
	InitialCandidates     []string                   `json:"initial_candidates,omitempty"`
	HintCandidates        []string                   `json:"hint_candidates,omitempty"`
	ForwardProbeCandidates []string                  `json:"forward_probe_candidates,omitempty"`
	ForwardProbeVersions  []string                   `json:"forward_probe_versions,omitempty"`
	InitialFetch          *candidateFetchDiagnostics `json:"initial_fetch,omitempty"`
	HintFetch             *candidateFetchDiagnostics `json:"hint_fetch,omitempty"`
	ForwardProbeFetch     *candidateFetchDiagnostics `json:"forward_probe_fetch,omitempty"`
	HintRefreshError      string                     `json:"hint_refresh_error,omitempty"`
	ForwardProbeError     string                     `json:"forward_probe_error,omitempty"`
}

type RemoteManifestResult struct {
	Manifest    *UpdateManifest           `json:"manifest,omitempty"`
	Source      string                    `json:"source,omitempty"`
	Diagnostics ManifestLookupDiagnostics `json:"diagnostics"`
}

func getRemoteManifestResultForMode(updateMode string, versionHints ...string) (*RemoteManifestResult, error) {
	cleanHints := make([]string, 0, len(versionHints))
	for _, hint := range versionHints {
		if v := strings.TrimSpace(hint); v != "" {
			cleanHints = append(cleanHints, v)
		}
	}

	validator := func(manifest *UpdateManifest) error {
		return validateUpdateManifest(manifest)
	}

	// beta：只拉 tag=beta 固定入口，跳过 latest/version-hint/forward-probe（那些会落到正式 release）
	if isBetaUpdateChannel() {
		candidates := betaManifestCandidates()
		result := &RemoteManifestResult{
			Diagnostics: ManifestLookupDiagnostics{
				Strategy:          "beta_release_tag",
				InitialCandidates: append([]string(nil), candidates...),
			},
		}
		manifest, source, initialDiag, err := fetchJSONFromCandidatesWithDiagnostics[UpdateManifest](
			context.Background(),
			candidates,
			15*time.Second,
			validator,
		)
		result.Diagnostics.InitialFetch = initialDiag
		if err != nil {
			return nil, fmt.Errorf("获取 UPDATE_MANIFEST.json 失败: %w", err)
		}
		result.Manifest = manifest
		result.Source = source
		result.Diagnostics.ManifestSource = source
		log.Printf("获取更新清单成功，使用源: %s (mode=%s channel=beta strategy=%s)", source, strings.TrimSpace(updateMode), result.Diagnostics.Strategy)
		return result, nil
	}

	candidates := manifestCandidates()
	result := &RemoteManifestResult{
		Diagnostics: ManifestLookupDiagnostics{
			Strategy:          "latest_first",
			InitialCandidates: append([]string(nil), candidates...),
		},
	}

	manifest, source, initialDiag, err := fetchJSONFromCandidatesWithDiagnostics[UpdateManifest](
		context.Background(),
		candidates,
		15*time.Second,
		validator,
	)
	result.Diagnostics.InitialFetch = initialDiag
	if err != nil && len(cleanHints) > 0 {
		hintCandidates := manifestVersionHintCandidates(cleanHints...)
		result.Diagnostics.Strategy = "version_hint_fallback"
		result.Diagnostics.HintCandidates = append([]string(nil), hintCandidates...)
		var hintDiag *candidateFetchDiagnostics
		manifest, source, hintDiag, err = fetchJSONFromCandidatesWithDiagnostics[UpdateManifest](
			context.Background(),
			hintCandidates,
			15*time.Second,
			validator,
		)
		result.Diagnostics.HintFetch = hintDiag
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
			result.Diagnostics.HintRefreshAttempted = true
			hintCandidates := manifestVersionHintCandidates(cleanHints...)
			if len(result.Diagnostics.HintCandidates) == 0 {
				result.Diagnostics.HintCandidates = append([]string(nil), hintCandidates...)
			}
			if hintedManifest, hintedSource, hintedDiag, hintedErr := fetchJSONFromCandidatesWithDiagnostics[UpdateManifest](
				context.Background(),
				hintCandidates,
				15*time.Second,
				validator,
			); hintedErr == nil && hintedManifest != nil {
				result.Diagnostics.HintFetch = hintedDiag
				hintedVersion := strings.TrimSpace(hintedManifest.Latest.Version)
				if currentVersion == "" || isNewerVersion(hintedVersion, currentVersion) {
					manifest = hintedManifest
					source = hintedSource
					currentVersion = hintedVersion
					result.Diagnostics.HintRefreshApplied = true
					result.Diagnostics.Strategy = "version_hint_refresh"
				}
			} else {
				result.Diagnostics.HintFetch = hintedDiag
				result.Diagnostics.HintRefreshError = hintedErr.Error()
			}
		}

		needForwardProbe := currentVersion == ""
		if !needForwardProbe {
			for _, hint := range cleanHints {
				if !isNewerVersion(currentVersion, hint) {
					needForwardProbe = true
					break
				}
			}
		}
		if needForwardProbe {
			forwardCandidates, probeVersions := manifestForwardProbeCandidates(cleanHints...)
			if len(forwardCandidates) > 0 {
				result.Diagnostics.ForwardProbeAttempted = true
				result.Diagnostics.ForwardProbeCandidates = append([]string(nil), forwardCandidates...)
				result.Diagnostics.ForwardProbeVersions = append([]string(nil), probeVersions...)
				if probedManifest, probedSource, probedDiag, probedErr := fetchJSONFromCandidatesWithDiagnostics[UpdateManifest](
					context.Background(),
					forwardCandidates,
					15*time.Second,
					validator,
				); probedErr == nil && probedManifest != nil {
					result.Diagnostics.ForwardProbeFetch = probedDiag
					probedVersion := strings.TrimSpace(probedManifest.Latest.Version)
					if currentVersion == "" || isNewerVersion(probedVersion, currentVersion) {
						manifest = probedManifest
						source = probedSource
						result.Diagnostics.ForwardProbeApplied = true
						result.Diagnostics.Strategy = "forward_patch_probe"
					}
				} else {
					result.Diagnostics.ForwardProbeFetch = probedDiag
					result.Diagnostics.ForwardProbeError = probedErr.Error()
				}
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("获取 UPDATE_MANIFEST.json 失败: %w", err)
	}
	result.Manifest = manifest
	result.Source = source
	result.Diagnostics.ManifestSource = source
	log.Printf("获取更新清单成功，使用源: %s (mode=%s strategy=%s)", source, strings.TrimSpace(updateMode), result.Diagnostics.Strategy)
	return result, nil
}

func getRemoteManifestForMode(updateMode string, versionHints ...string) (*UpdateManifest, error) {
	result, err := getRemoteManifestResultForMode(updateMode, versionHints...)
	if err != nil {
		return nil, err
	}
	return result.Manifest, nil
}

func getRemoteManifest(versionHints ...string) (*UpdateManifest, error) {
	return getRemoteManifestForMode(updateModeRuntimeInstall, versionHints...)
}
