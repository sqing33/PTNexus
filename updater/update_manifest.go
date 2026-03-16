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
	OS         string   `json:"os"`
	Arch       string   `json:"arch"`
	URL        string   `json:"url"`
	MirrorURLs []string `json:"mirror_urls,omitempty"`
	SHA256     string   `json:"sha256"`
	Size       int64    `json:"size,omitempty"`
	Format     string   `json:"format,omitempty"` // "tar.gz" (default) | "zip"
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

func getRemoteManifest(versionHints ...string) (*UpdateManifest, error) {
	cleanHints := make([]string, 0, len(versionHints))
	for _, hint := range versionHints {
		if v := strings.TrimSpace(hint); v != "" {
			cleanHints = append(cleanHints, v)
		}
	}

	// 未显式提供版本提示时，尝试先从 CHANGELOG 读取最新版本，拼出 release/tag 地址。
	if len(cleanHints) == 0 {
		if cfg, err := getRemoteConfig(); err == nil && len(cfg.History) > 0 {
			if v := strings.TrimSpace(cfg.History[0].Version); v != "" {
				cleanHints = append(cleanHints, v)
			}
		} else if err != nil {
			log.Printf("获取更新清单前读取远程版本失败，继续尝试默认地址: %v", err)
		}
	}

	manifest, source, err := fetchJSONFromCandidates[UpdateManifest](context.Background(), manifestCandidates(cleanHints...), 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("获取 UPDATE_MANIFEST.json 失败: %w", err)
	}
	if strings.TrimSpace(manifest.Latest.Version) == "" {
		return nil, fmt.Errorf("UPDATE_MANIFEST.json 缺少 latest.version")
	}
	log.Printf("获取更新清单成功，使用源: %s", source)
	return manifest, nil
}
