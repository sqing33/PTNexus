package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// UpdateManifest describes the remote, signed-off update metadata.
//
// Phase 1 goal: map "latest" -> downloadable runtime bundle (artifact) per OS/ARCH.
// This allows online update without git.
type UpdateManifest struct {
	Schema int            `json:"schema"`
	Latest ManifestLatest `json:"latest"`
}

type ManifestLatest struct {
	Version       string           `json:"version"`
	Date          string           `json:"date,omitempty"`
	ForceUpdate   bool             `json:"force_update,omitempty"`
	DisableUpdate bool             `json:"disable_update,omitempty"`
	Note          string           `json:"note,omitempty"`
	Artifacts     []UpdateArtifact `json:"artifacts"`
}

type UpdateArtifact struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
	Format string `json:"format,omitempty"` // "tar.gz" (default) | "zip"
}

func getManifestURL() string {
	if explicit := strings.TrimSpace(getEnv("UPDATE_MANIFEST_URL", "")); explicit != "" {
		return explicit
	}
	switch getUpdateSource() {
	case "github":
		return "https://raw.githubusercontent.com/sqing33/PTNexus/main/UPDATE_MANIFEST.json"
	default:
		return "https://gitee.com/sqing33/PTNexus/raw/main/UPDATE_MANIFEST.json"
	}
}

func newUpdateHTTPClient(timeout time.Duration) *http.Client {
	// By default we keep the legacy behavior: do not use proxy to avoid proxy cache issues.
	// If you really need proxy (e.g. GitHub is blocked), set UPDATE_USE_PROXY=true.
	useProxy := isTruthy(getEnv("UPDATE_USE_PROXY", "false"))

	tr := &http.Transport{}
	if useProxy {
		tr.Proxy = http.ProxyFromEnvironment
	} else {
		tr.Proxy = func(req *http.Request) (*url.URL, error) {
			return nil, nil
		}
	}

	return &http.Client{Timeout: timeout, Transport: tr}
}

func fetchJSON(urlStr string, dst any) error {
	client := newUpdateHTTPClient(15 * time.Second)

	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
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

func getRemoteManifest() (*UpdateManifest, error) {
	base := getManifestURL()
	// Add timestamp to bypass caches.
	requestURL := fmt.Sprintf("%s?t=%d", base, time.Now().UnixNano())

	var manifest UpdateManifest
	if err := fetchJSON(requestURL, &manifest); err != nil {
		return nil, fmt.Errorf("获取 UPDATE_MANIFEST.json 失败 (%s): %w", filepath.Base(base), err)
	}
	if strings.TrimSpace(manifest.Latest.Version) == "" {
		return nil, fmt.Errorf("UPDATE_MANIFEST.json 缺少 latest.version")
	}
	return &manifest, nil
}
