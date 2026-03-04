package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	githubChangelogURL = "https://raw.githubusercontent.com/sqing33/PTNexus/main/CHANGELOG.json"
	giteeChangelogURL  = "https://gitee.com/sqing33/PTNexus/raw/main/CHANGELOG.json"
	githubManifestURL  = "https://raw.githubusercontent.com/sqing33/PTNexus/main/UPDATE_MANIFEST.json"
	giteeManifestURL   = "https://gitee.com/sqing33/PTNexus/raw/main/UPDATE_MANIFEST.json"
)

func normalizeURLCandidates(urls ...string) []string {
	seen := make(map[string]struct{}, len(urls))
	out := make([]string, 0, len(urls))
	for _, raw := range urls {
		u := strings.TrimSpace(raw)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

func changelogCandidates() []string {
	return normalizeURLCandidates(githubChangelogURL, giteeChangelogURL)
}

func manifestCandidates() []string {
	// Backward compatibility for existing deployments that explicitly override manifest URL.
	override := strings.TrimSpace(getEnv("UPDATE_MANIFEST_URL", ""))
	return normalizeURLCandidates(override, githubManifestURL, giteeManifestURL)
}

func withNoCacheQuery(base string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		if strings.Contains(base, "?") {
			return fmt.Sprintf("%s&t=%d", base, time.Now().UnixNano())
		}
		return fmt.Sprintf("%s?t=%d", base, time.Now().UnixNano())
	}
	q := parsed.Query()
	q.Set("t", fmt.Sprintf("%d", time.Now().UnixNano()))
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

type candidateFetchResult[T any] struct {
	baseURL string
	value   *T
	err     error
}

func fetchJSONFromCandidates[T any](ctx context.Context, candidates []string, timeout time.Duration) (*T, string, error) {
	list := normalizeURLCandidates(candidates...)
	if len(list) == 0 {
		return nil, "", fmt.Errorf("没有可用的候选更新源")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan candidateFetchResult[T], len(list))
	for _, baseURL := range list {
		baseURL := baseURL
		go func() {
			requestURL := withNoCacheQuery(baseURL)
			var parsed T
			err := fetchJSONWithContext(reqCtx, requestURL, &parsed, timeout)
			if err != nil {
				results <- candidateFetchResult[T]{baseURL: baseURL, err: err}
				return
			}
			results <- candidateFetchResult[T]{baseURL: baseURL, value: &parsed}
		}()
	}

	errMessages := make([]string, 0, len(list))
	for i := 0; i < len(list); i++ {
		result := <-results
		if result.err == nil && result.value != nil {
			cancel()
			return result.value, result.baseURL, nil
		}
		errMessages = append(errMessages, fmt.Sprintf("%s -> %v", result.baseURL, result.err))
	}

	return nil, "", fmt.Errorf("所有候选源均失败: %s", strings.Join(errMessages, "; "))
}
