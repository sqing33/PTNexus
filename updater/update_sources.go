package main

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	githubManifestReleaseURLTemplate = "https://github.com/sqing33/PTNexus/releases/download/%s/UPDATE_MANIFEST.json"
	githubManifestReleaseLatestURL   = "https://github.com/sqing33/PTNexus/releases/latest/download/UPDATE_MANIFEST.json"
	githubManifestRawGoURL           = "https://raw.githubusercontent.com/sqing33/PTNexus/go/UPDATE_MANIFEST.json"
	githubManifestRawMainURL         = "https://raw.githubusercontent.com/sqing33/PTNexus/main/UPDATE_MANIFEST.json"

	giteeManifestReleaseURLTemplate = "https://gitee.com/sqing33/PTNexus/releases/download/%s/UPDATE_MANIFEST.json"
	giteeManifestReleaseLatestURL   = "https://gitee.com/sqing33/PTNexus/releases/latest/download/UPDATE_MANIFEST.json"
	giteeManifestRawGoURL           = "https://gitee.com/sqing33/PTNexus/raw/go/UPDATE_MANIFEST.json"
	giteeManifestRawMainURL         = "https://gitee.com/sqing33/PTNexus/raw/main/UPDATE_MANIFEST.json"

	// 滚动 prerelease tag=beta 下的固定 manifest 入口（与正式 latest 隔离）
	githubManifestBetaReleaseURL = "https://github.com/sqing33/PTNexus/releases/download/beta/UPDATE_MANIFEST.json"
	giteeManifestBetaReleaseURL  = "https://gitee.com/sqing33/PTNexus/releases/download/beta/UPDATE_MANIFEST.json"
)

const (
	// Manifest is expected to be published as a Release asset.
	// Runtime metadata is served only from Release assets.
	updateManifestRawFallbackEnv = "UPDATE_MANIFEST_RAW_FALLBACK"
	updateChannelEnv             = "UPDATE_CHANNEL"

	updateChannelStable = "stable"
	updateChannelBeta   = "beta"
	betaReleaseTag      = "beta"
)

// updateChannel 返回 stable|beta；未知值回退 stable，避免误入测试通道。
func updateChannel() string {
	raw := strings.ToLower(strings.TrimSpace(getEnv(updateChannelEnv, updateChannelStable)))
	switch raw {
	case updateChannelBeta, "preview", "test":
		return updateChannelBeta
	default:
		return updateChannelStable
	}
}

func isBetaUpdateChannel() bool {
	return updateChannel() == updateChannelBeta
}

// betaManifestCandidates 仅指向 tag=beta 的固定入口，不含 /releases/latest。
func betaManifestCandidates() []string {
	override := strings.TrimSpace(getEnv("UPDATE_MANIFEST_URL", ""))
	return normalizeURLCandidates(
		override,
		giteeManifestBetaReleaseURL,
		githubManifestBetaReleaseURL,
	)
}

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

func manifestReleaseCandidatesForVersion(version string) []string {
	clean := strings.TrimSpace(version)
	if clean == "" {
		return nil
	}

	escaped := url.PathEscape(clean)
	// Gitee first (reachable from China), then GitHub as fallback
	return []string{
		fmt.Sprintf(giteeManifestReleaseURLTemplate, escaped),
		fmt.Sprintf(githubManifestReleaseURLTemplate, escaped),
	}
}

func manifestVersionHintCandidates(versionHints ...string) []string {
	override := strings.TrimSpace(getEnv("UPDATE_MANIFEST_URL", ""))
	candidates := make([]string, 0, 1+len(versionHints)*2)
	candidates = append(candidates, override)
	for _, hint := range versionHints {
		candidates = append(candidates, manifestReleaseCandidatesForVersion(hint)...)
	}
	return normalizeURLCandidates(candidates...)
}

func parseVersionParts(version string) ([]int, bool) {
	clean := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(version), "v"), "V"))
	if clean == "" {
		return nil, false
	}
	parts := strings.Split(clean, ".")
	parsed := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		parsed = append(parsed, value)
	}
	return parsed, true
}

func buildForwardProbeVersionHints(versionHints ...string) []string {
	const maxPatchProbe = 3
	seen := make(map[string]struct{}, len(versionHints)*maxPatchProbe)
	out := make([]string, 0, len(versionHints)*maxPatchProbe)
	for _, hint := range versionHints {
		parts, ok := parseVersionParts(hint)
		if !ok || len(parts) < 3 {
			continue
		}
		base := append([]int(nil), parts...)
		for i := 1; i <= maxPatchProbe; i++ {
			candidateParts := append([]int(nil), base...)
			candidateParts[len(candidateParts)-1] += i
			segments := make([]string, 0, len(candidateParts))
			for _, part := range candidateParts {
				segments = append(segments, strconv.Itoa(part))
			}
			candidate := "v" + strings.Join(segments, ".")
			if _, exists := seen[candidate]; exists {
				continue
			}
			seen[candidate] = struct{}{}
			out = append(out, candidate)
		}
	}
	return out
}

func manifestForwardProbeCandidates(versionHints ...string) ([]string, []string) {
	probeVersions := buildForwardProbeVersionHints(versionHints...)
	return manifestVersionHintCandidates(probeVersions...), probeVersions
}

func manifestCandidates(versionHints ...string) []string {
	if isBetaUpdateChannel() {
		// beta 通道：只读滚动 prerelease tag=beta，绝不拼正式 latest / 正式 version tag
		return betaManifestCandidates()
	}

	// Allow explicit override for deployments that publish manifest in another location.
	override := strings.TrimSpace(getEnv("UPDATE_MANIFEST_URL", ""))
	candidates := make([]string, 0, 1+len(versionHints)*2+2)
	candidates = append(candidates, override)
	for _, hint := range versionHints {
		// 稳定通道不把 beta 版本号当作 release tag 去探测
		if strings.EqualFold(strings.TrimSpace(hint), betaReleaseTag) {
			continue
		}
		candidates = append(candidates, manifestReleaseCandidatesForVersion(hint)...)
	}
	candidates = append(candidates,
		giteeManifestReleaseLatestURL,
		githubManifestReleaseLatestURL,
	)
	if isTruthy(getEnv(updateManifestRawFallbackEnv, "false")) {
		candidates = append(candidates,
			giteeManifestRawGoURL,
			giteeManifestRawMainURL,
			githubManifestRawGoURL,
			githubManifestRawMainURL,
		)
	}
	return normalizeURLCandidates(candidates...)
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

type candidateFetchAttempt struct {
	URL     string `json:"url"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type candidateFetchDiagnostics struct {
	Attempts []candidateFetchAttempt `json:"attempts,omitempty"`
}

func fetchJSONFromCandidatesWithDiagnostics[T any](ctx context.Context, candidates []string, timeout time.Duration, validators ...func(*T) error) (*T, string, *candidateFetchDiagnostics, error) {
	list := normalizeURLCandidates(candidates...)
	if len(list) == 0 {
		return nil, "", &candidateFetchDiagnostics{}, fmt.Errorf("没有可用的候选更新源")
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

	diagnostics := &candidateFetchDiagnostics{Attempts: make([]candidateFetchAttempt, 0, len(list))}
	errMessages := make([]string, 0, len(list))
	for i := 0; i < len(list); i++ {
		result := <-results
		if result.err == nil && result.value != nil {
			validatorErr := error(nil)
			for _, validate := range validators {
				if validate == nil {
					continue
				}
				if err := validate(result.value); err != nil {
					validatorErr = err
					break
				}
			}
			if validatorErr == nil {
				diagnostics.Attempts = append(diagnostics.Attempts, candidateFetchAttempt{URL: result.baseURL, Success: true})
				cancel()
				return result.value, result.baseURL, diagnostics, nil
			}
			diagnostics.Attempts = append(diagnostics.Attempts, candidateFetchAttempt{URL: result.baseURL, Success: false, Error: validatorErr.Error()})
			errMessages = append(errMessages, fmt.Sprintf("%s -> %v", result.baseURL, validatorErr))
			continue
		}
		errText := "unknown error"
		if result.err != nil {
			errText = result.err.Error()
		}
		diagnostics.Attempts = append(diagnostics.Attempts, candidateFetchAttempt{URL: result.baseURL, Success: false, Error: errText})
		errMessages = append(errMessages, fmt.Sprintf("%s -> %v", result.baseURL, result.err))
	}

	return nil, "", diagnostics, fmt.Errorf("所有候选源均失败: %s", strings.Join(errMessages, "; "))
}

func fetchJSONFromCandidates[T any](ctx context.Context, candidates []string, timeout time.Duration, validators ...func(*T) error) (*T, string, error) {
	value, source, _, err := fetchJSONFromCandidatesWithDiagnostics(ctx, candidates, timeout, validators...)
	return value, source, err
}
