package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestManifestCandidatesIncludeGitee(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")
	t.Setenv(updateManifestRawFallbackEnv, "false")

	version := "v4.0.2"
	candidates := manifestCandidates(version)
	if len(candidates) == 0 {
		t.Fatal("expected non-empty candidates")
	}

	expectedGitee := fmt.Sprintf(giteeManifestReleaseURLTemplate, version)
	expectedGitHub := fmt.Sprintf(githubManifestReleaseURLTemplate, version)
	if candidates[0] != expectedGitee {
		t.Fatalf("expected first candidate %q, got %q", expectedGitee, candidates[0])
	}
	if candidates[1] != expectedGitHub {
		t.Fatalf("expected second candidate %q, got %q", expectedGitHub, candidates[1])
	}

	hasGitee := false
	for _, item := range candidates {
		if strings.Contains(strings.ToLower(item), "gitee") {
			hasGitee = true
			break
		}
	}
	if !hasGitee {
		t.Fatal("expected at least one gitee candidate in manifest candidates")
	}
}

func TestManifestCandidatesWithoutHintsUseLatestOnly(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")
	t.Setenv(updateManifestRawFallbackEnv, "false")

	candidates := manifestCandidates()
	if len(candidates) != 2 {
		t.Fatalf("expected exactly 2 latest candidates, got %d: %#v", len(candidates), candidates)
	}
	if candidates[0] != giteeManifestReleaseLatestURL {
		t.Fatalf("expected first candidate %q, got %q", giteeManifestReleaseLatestURL, candidates[0])
	}
	if candidates[1] != githubManifestReleaseLatestURL {
		t.Fatalf("expected second candidate %q, got %q", githubManifestReleaseLatestURL, candidates[1])
	}
}

func TestManifestVersionHintCandidatesExcludeLatestFallback(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")
	candidates := manifestVersionHintCandidates("v4.0.16")
	if len(candidates) != 2 {
		t.Fatalf("expected exactly 2 version-hint candidates, got %d: %#v", len(candidates), candidates)
	}
	if candidates[0] != fmt.Sprintf(giteeManifestReleaseURLTemplate, "v4.0.16") {
		t.Fatalf("unexpected first candidate: %q", candidates[0])
	}
	if candidates[1] != fmt.Sprintf(githubManifestReleaseURLTemplate, "v4.0.16") {
		t.Fatalf("unexpected second candidate: %q", candidates[1])
	}
}

func TestManifestCandidatesDefaultToReleaseAssetsOnly(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")
	t.Setenv(updateManifestRawFallbackEnv, "false")

	candidates := manifestCandidates("v4.0.16")
	for _, candidate := range candidates {
		if strings.Contains(candidate, "/raw/") || strings.Contains(candidate, "raw.githubusercontent.com") {
			t.Fatalf("expected default manifest candidates to exclude raw fallback, got %q", candidate)
		}
	}
}

func TestManifestCandidatesCanOptIntoRawFallback(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")
	t.Setenv(updateManifestRawFallbackEnv, "true")

	candidates := manifestCandidates("v4.0.16")
	joined := strings.Join(candidates, "\n")
	if !strings.Contains(joined, giteeManifestRawGoURL) {
		t.Fatalf("expected raw fallback to include %q", giteeManifestRawGoURL)
	}
	if !strings.Contains(joined, githubManifestRawGoURL) {
		t.Fatalf("expected raw fallback to include %q", githubManifestRawGoURL)
	}
}

func TestFetchJSONFromCandidatesSkipsUnusableManifest(t *testing.T) {
	t.Setenv("UPDATE_OS", "linux")
	t.Setenv("UPDATE_ARCH", "amd64")

	bad := `{"schema":2,"latest":{"version":"v4.0.16","artifacts":[{"os":"linux","arch":"amd64","url":"https://example.com/runtime.tar.gz","sha256":""}]},"history":[{"version":"v4.0.16","changes":["bad"]}]}`
	good := `{"schema":2,"latest":{"version":"v4.0.16","artifacts":[{"os":"linux","arch":"amd64","url":"https://example.com/runtime.tar.gz","sha256":"abc123"}]},"history":[{"version":"v4.0.16","changes":["good"]}]}`

	server := newManifestTestServer(t, map[string]manifestTestResponse{
		"/bad.json":  {body: bad},
		"/good.json": {body: good},
	})

	manifest, source, err := fetchJSONFromCandidates[UpdateManifest](
		context.Background(),
		[]string{server.URL + "/bad.json", server.URL + "/good.json"},
		2*time.Second,
		func(manifest *UpdateManifest) error {
			return validateUpdateManifestForMode(manifest, updateModeRuntimeInstall)
		},
	)
	if err != nil {
		t.Fatalf("expected usable manifest fallback, got error: %v", err)
	}
	if source != server.URL+"/good.json" {
		t.Fatalf("expected source %q, got %q", server.URL+"/good.json", source)
	}
	if manifest == nil || manifest.Latest.Version != "v4.0.16" {
		t.Fatalf("expected manifest version v4.0.16, got %#v", manifest)
	}
}

func TestManifestCandidatesKeepsOverrideFirst(t *testing.T) {
	override := "https://override.example.com/manifest.json"
	t.Setenv("UPDATE_MANIFEST_URL", override)
	t.Setenv(updateManifestRawFallbackEnv, "false")

	candidates := manifestCandidates("v4.0.16")
	if len(candidates) == 0 {
		t.Fatal("expected non-empty candidates")
	}
	if candidates[0] != override {
		t.Fatalf("expected override candidate first, got %q", candidates[0])
	}
}

func TestGetRemoteManifestForModePrefersLatestBeforeVersionHints(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")
	t.Setenv(updateManifestRawFallbackEnv, "false")
	t.Setenv("UPDATE_OS", "linux")
	t.Setenv("UPDATE_ARCH", "amd64")

	oldGiteeLatest := giteeManifestReleaseLatestURL
	oldGithubLatest := githubManifestReleaseLatestURL
	oldGiteeTemplate := giteeManifestReleaseURLTemplate
	oldGithubTemplate := githubManifestReleaseURLTemplate
	t.Cleanup(func() {
		giteeManifestReleaseLatestURL = oldGiteeLatest
		githubManifestReleaseLatestURL = oldGithubLatest
		giteeManifestReleaseURLTemplate = oldGiteeTemplate
		githubManifestReleaseURLTemplate = oldGithubTemplate
	})

	latest := `{"schema":2,"latest":{"version":"v4.0.17","artifacts":[{"os":"linux","arch":"amd64","url":"https://example.com/runtime.tar.gz","sha256":"newsha"}]},"history":[{"version":"v4.0.17","changes":["new"]}]}`
	oldVersion := `{"schema":2,"latest":{"version":"v4.0.16","artifacts":[{"os":"linux","arch":"amd64","url":"https://example.com/runtime.tar.gz","sha256":"oldsha"}]},"history":[{"version":"v4.0.16","changes":["old"]}]}`

	server := newManifestTestServer(t, map[string]manifestTestResponse{
		"/latest.json":  {body: latest},
		"/v4.0.16.json": {body: oldVersion},
	})

	giteeManifestReleaseLatestURL = server.URL + "/latest.json"
	githubManifestReleaseLatestURL = server.URL + "/latest.json"
	giteeManifestReleaseURLTemplate = server.URL + "/%s.json"
	githubManifestReleaseURLTemplate = server.URL + "/%s.json"

	manifest, err := getRemoteManifestForMode(updateModeRuntimeInstall, "v4.0.16")
	if err != nil {
		t.Fatalf("expected manifest fetch success, got error: %v", err)
	}
	if manifest == nil || manifest.Latest.Version != "v4.0.17" {
		t.Fatalf("expected latest manifest version v4.0.17, got %#v", manifest)
	}
}

func TestGetRemoteManifestForModeFallsBackToVersionHintsWhenLatestUnavailable(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")
	t.Setenv(updateManifestRawFallbackEnv, "false")
	t.Setenv("UPDATE_OS", "linux")
	t.Setenv("UPDATE_ARCH", "amd64")

	oldGiteeLatest := giteeManifestReleaseLatestURL
	oldGithubLatest := githubManifestReleaseLatestURL
	oldGiteeTemplate := giteeManifestReleaseURLTemplate
	oldGithubTemplate := githubManifestReleaseURLTemplate
	t.Cleanup(func() {
		giteeManifestReleaseLatestURL = oldGiteeLatest
		githubManifestReleaseLatestURL = oldGithubLatest
		giteeManifestReleaseURLTemplate = oldGiteeTemplate
		githubManifestReleaseURLTemplate = oldGithubTemplate
	})

	oldVersion := `{"schema":2,"latest":{"version":"v4.0.16","artifacts":[{"os":"linux","arch":"amd64","url":"https://example.com/runtime.tar.gz","sha256":"oldsha"}]},"history":[{"version":"v4.0.16","changes":["old"]}]}`

	server := newManifestTestServer(t, map[string]manifestTestResponse{
		"/v4.0.16.json": {body: oldVersion},
	})

	giteeManifestReleaseLatestURL = server.URL + "/missing-latest.json"
	githubManifestReleaseLatestURL = server.URL + "/missing-latest.json"
	giteeManifestReleaseURLTemplate = server.URL + "/%s.json"
	githubManifestReleaseURLTemplate = server.URL + "/%s.json"

	manifest, err := getRemoteManifestForMode(updateModeRuntimeInstall, "v4.0.16")
	if err != nil {
		t.Fatalf("expected version-hint fallback success, got error: %v", err)
	}
	if manifest == nil || manifest.Latest.Version != "v4.0.16" {
		t.Fatalf("expected fallback manifest version v4.0.16, got %#v", manifest)
	}
}

func TestGetRemoteManifestForModeUsesVersionHintWhenLatestIsStale(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")
	t.Setenv(updateManifestRawFallbackEnv, "false")
	t.Setenv("UPDATE_OS", "linux")
	t.Setenv("UPDATE_ARCH", "amd64")

	oldGiteeLatest := giteeManifestReleaseLatestURL
	oldGithubLatest := githubManifestReleaseLatestURL
	oldGiteeTemplate := giteeManifestReleaseURLTemplate
	oldGithubTemplate := githubManifestReleaseURLTemplate
	t.Cleanup(func() {
		giteeManifestReleaseLatestURL = oldGiteeLatest
		githubManifestReleaseLatestURL = oldGithubLatest
		giteeManifestReleaseURLTemplate = oldGiteeTemplate
		githubManifestReleaseURLTemplate = oldGithubTemplate
	})

	latest := `{"schema":2,"latest":{"version":"v4.0.18","artifacts":[{"os":"linux","arch":"amd64","url":"https://example.com/runtime.tar.gz","sha256":"latestsha"}]},"history":[{"version":"v4.0.18","changes":["old latest"]}]}`
	hinted := `{"schema":2,"latest":{"version":"v4.0.19","artifacts":[{"os":"linux","arch":"amd64","url":"https://example.com/runtime.tar.gz","sha256":"hintsha"}]},"history":[{"version":"v4.0.19","changes":["new hinted"]}]}`

	server := newManifestTestServer(t, map[string]manifestTestResponse{
		"/latest.json":  {body: latest},
		"/v4.0.18.json": {body: latest},
		"/v4.0.19.json": {body: hinted},
	})

	giteeManifestReleaseLatestURL = server.URL + "/latest.json"
	githubManifestReleaseLatestURL = server.URL + "/latest.json"
	giteeManifestReleaseURLTemplate = server.URL + "/%s.json"
	githubManifestReleaseURLTemplate = server.URL + "/%s.json"

	manifest, err := getRemoteManifestForMode(updateModeRuntimeInstall, "v4.0.19")
	if err != nil {
		t.Fatalf("expected hinted manifest fetch success, got error: %v", err)
	}
	if manifest == nil || manifest.Latest.Version != "v4.0.19" {
		t.Fatalf("expected hinted manifest version v4.0.19, got %#v", manifest)
	}
}

func TestGetRemoteManifestForModeKeepsLatestWhenItIsNewerThanHint(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")
	t.Setenv(updateManifestRawFallbackEnv, "false")
	t.Setenv("UPDATE_OS", "linux")
	t.Setenv("UPDATE_ARCH", "amd64")

	oldGiteeLatest := giteeManifestReleaseLatestURL
	oldGithubLatest := githubManifestReleaseLatestURL
	oldGiteeTemplate := giteeManifestReleaseURLTemplate
	oldGithubTemplate := githubManifestReleaseURLTemplate
	t.Cleanup(func() {
		giteeManifestReleaseLatestURL = oldGiteeLatest
		githubManifestReleaseLatestURL = oldGithubLatest
		giteeManifestReleaseURLTemplate = oldGiteeTemplate
		githubManifestReleaseURLTemplate = oldGithubTemplate
	})

	latest := `{"schema":2,"latest":{"version":"v4.0.19","artifacts":[{"os":"linux","arch":"amd64","url":"https://example.com/runtime.tar.gz","sha256":"latestsha"}]},"history":[{"version":"v4.0.19","changes":["latest"]}]}`
	hinted := `{"schema":2,"latest":{"version":"v4.0.18","artifacts":[{"os":"linux","arch":"amd64","url":"https://example.com/runtime.tar.gz","sha256":"hintsha"}]},"history":[{"version":"v4.0.18","changes":["hinted"]}]}`

	server := newManifestTestServer(t, map[string]manifestTestResponse{
		"/latest.json":  {body: latest},
		"/v4.0.18.json": {body: hinted},
	})

	giteeManifestReleaseLatestURL = server.URL + "/latest.json"
	githubManifestReleaseLatestURL = server.URL + "/latest.json"
	giteeManifestReleaseURLTemplate = server.URL + "/%s.json"
	githubManifestReleaseURLTemplate = server.URL + "/%s.json"

	manifest, err := getRemoteManifestForMode(updateModeRuntimeInstall, "v4.0.18")
	if err != nil {
		t.Fatalf("expected latest manifest fetch success, got error: %v", err)
	}
	if manifest == nil || manifest.Latest.Version != "v4.0.19" {
		t.Fatalf("expected latest manifest version v4.0.19, got %#v", manifest)
	}
}

func TestGetRemoteManifestForModeUsesForwardPatchProbeWhenLatestIsStale(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")
	t.Setenv(updateManifestRawFallbackEnv, "false")
	t.Setenv("UPDATE_OS", "linux")
	t.Setenv("UPDATE_ARCH", "amd64")

	oldGiteeLatest := giteeManifestReleaseLatestURL
	oldGithubLatest := githubManifestReleaseLatestURL
	oldGiteeTemplate := giteeManifestReleaseURLTemplate
	oldGithubTemplate := githubManifestReleaseURLTemplate
	t.Cleanup(func() {
		giteeManifestReleaseLatestURL = oldGiteeLatest
		githubManifestReleaseLatestURL = oldGithubLatest
		giteeManifestReleaseURLTemplate = oldGiteeTemplate
		githubManifestReleaseURLTemplate = oldGithubTemplate
	})

	latest := `{"schema":2,"latest":{"version":"v4.0.22","artifacts":[{"os":"linux","arch":"amd64","url":"https://example.com/runtime.tar.gz","sha256":"latestsha"}]},"history":[{"version":"v4.0.22","changes":["stale latest"]}]}`
	probed := `{"schema":2,"latest":{"version":"v4.0.23","artifacts":[{"os":"linux","arch":"amd64","url":"https://example.com/runtime.tar.gz","sha256":"probesha"}]},"history":[{"version":"v4.0.23","changes":["forward probe"]}]}`

	server := newManifestTestServer(t, map[string]manifestTestResponse{
		"/latest.json":  {body: latest},
		"/v4.0.22.json": {body: latest},
		"/v4.0.23.json": {body: probed},
	})

	giteeManifestReleaseLatestURL = server.URL + "/latest.json"
	githubManifestReleaseLatestURL = server.URL + "/latest.json"
	giteeManifestReleaseURLTemplate = server.URL + "/%s.json"
	githubManifestReleaseURLTemplate = server.URL + "/%s.json"

	result, err := getRemoteManifestResultForMode(updateModeRuntimeInstall, "v4.0.22")
	if err != nil {
		t.Fatalf("expected forward probe manifest fetch success, got error: %v", err)
	}
	if result == nil || result.Manifest == nil || result.Manifest.Latest.Version != "v4.0.23" {
		t.Fatalf("expected probed manifest version v4.0.23, got %#v", result)
	}
	if result.Source != server.URL+"/v4.0.23.json" {
		t.Fatalf("expected probed source %q, got %#v", server.URL+"/v4.0.23.json", result)
	}
	if !result.Diagnostics.ForwardProbeAttempted {
		t.Fatalf("expected forward probe attempted, got %#v", result.Diagnostics)
	}
	if !result.Diagnostics.ForwardProbeApplied {
		t.Fatalf("expected forward probe applied, got %#v", result.Diagnostics)
	}
	if result.Diagnostics.Strategy != "forward_patch_probe" {
		t.Fatalf("expected forward_patch_probe strategy, got %#v", result.Diagnostics)
	}
	if len(result.Diagnostics.ForwardProbeVersions) == 0 || result.Diagnostics.ForwardProbeVersions[0] != "v4.0.23" {
		t.Fatalf("expected forward probe versions to include v4.0.23, got %#v", result.Diagnostics.ForwardProbeVersions)
	}
}

func TestGetRemoteManifestForModeKeepsLatestWhenForwardPatchProbeMisses(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")
	t.Setenv(updateManifestRawFallbackEnv, "false")
	t.Setenv("UPDATE_OS", "linux")
	t.Setenv("UPDATE_ARCH", "amd64")

	oldGiteeLatest := giteeManifestReleaseLatestURL
	oldGithubLatest := githubManifestReleaseLatestURL
	oldGiteeTemplate := giteeManifestReleaseURLTemplate
	oldGithubTemplate := githubManifestReleaseURLTemplate
	t.Cleanup(func() {
		giteeManifestReleaseLatestURL = oldGiteeLatest
		githubManifestReleaseLatestURL = oldGithubLatest
		giteeManifestReleaseURLTemplate = oldGiteeTemplate
		githubManifestReleaseURLTemplate = oldGithubTemplate
	})

	latest := `{"schema":2,"latest":{"version":"v4.0.22","artifacts":[{"os":"linux","arch":"amd64","url":"https://example.com/runtime.tar.gz","sha256":"latestsha"}]},"history":[{"version":"v4.0.22","changes":["stale latest"]}]}`

	server := newManifestTestServer(t, map[string]manifestTestResponse{
		"/latest.json":  {body: latest},
		"/v4.0.22.json": {body: latest},
	})

	giteeManifestReleaseLatestURL = server.URL + "/latest.json"
	githubManifestReleaseLatestURL = server.URL + "/latest.json"
	giteeManifestReleaseURLTemplate = server.URL + "/%s.json"
	githubManifestReleaseURLTemplate = server.URL + "/%s.json"

	result, err := getRemoteManifestResultForMode(updateModeRuntimeInstall, "v4.0.22")
	if err != nil {
		t.Fatalf("expected latest manifest fetch success, got error: %v", err)
	}
	if result == nil || result.Manifest == nil || result.Manifest.Latest.Version != "v4.0.22" {
		t.Fatalf("expected latest manifest version v4.0.22, got %#v", result)
	}
	if result.Diagnostics.Strategy != "latest_first" {
		t.Fatalf("expected latest_first strategy when probe misses, got %#v", result.Diagnostics)
	}
	if !result.Diagnostics.ForwardProbeAttempted {
		t.Fatalf("expected forward probe attempted, got %#v", result.Diagnostics)
	}
	if result.Diagnostics.ForwardProbeApplied {
		t.Fatalf("expected forward probe not applied, got %#v", result.Diagnostics)
	}
	if result.Diagnostics.ForwardProbeError == "" {
		t.Fatalf("expected forward probe error to be recorded, got %#v", result.Diagnostics)
	}
}

func TestGetLocalVersionPrefersCurrentRuntimeOverImageVersion(t *testing.T) {
	oldUpdateDir := updateDir
	oldLocalConfigFile := localConfigFile
	oldEmbeddedConfigFile := embeddedConfigFile
	oldLocalVersionFilePath := localVersionFilePath
	t.Cleanup(func() {
		updateDir = oldUpdateDir
		localConfigFile = oldLocalConfigFile
		embeddedConfigFile = oldEmbeddedConfigFile
		localVersionFilePath = oldLocalVersionFilePath
	})

	root := t.TempDir()
	updateDir = filepath.Join(root, "updates")
	localConfigFile = filepath.Join(updateDir, "local", "CHANGELOG.json")
	embeddedConfigFile = filepath.Join(root, "embedded", "CHANGELOG.json")

	releaseServerDir := filepath.Join(updateDir, "releases", "v4.0.2", "server")
	if err := os.MkdirAll(releaseServerDir, 0o755); err != nil {
		t.Fatalf("mkdir release server dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(releaseServerDir, "server"), []byte("bin"), 0o644); err != nil {
		t.Fatalf("write server binary: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(localConfigFile), 0o755); err != nil {
		t.Fatalf("mkdir local config dir: %v", err)
	}
	if err := os.WriteFile(localConfigFile, []byte(`{"history":[{"version":"v4.0.1"}]}`), 0o644); err != nil {
		t.Fatalf("write local config: %v", err)
	}

	currentLink := filepath.Join(updateDir, "current")
	if err := os.Symlink(releaseServerDir, currentLink); err != nil {
		t.Skipf("symlink unsupported in current environment: %v", err)
	}

	t.Setenv("PTNEXUS_VERSION", "")
	t.Setenv("VERSION_FILE", "")

	appVersionPath := filepath.Join(root, "VERSION")
	if err := os.WriteFile(appVersionPath, []byte("v4.0.0\n"), 0o644); err != nil {
		t.Fatalf("write app version: %v", err)
	}
	localVersionFilePath = appVersionPath

	version := getLocalVersion()
	if version != "v4.0.2" {
		t.Fatalf("expected current runtime version v4.0.2, got %s", version)
	}
}

func TestGetLocalVersionDetailsFallsBackToEnvVersion(t *testing.T) {
	oldUpdateDir := updateDir
	oldLocalConfigFile := localConfigFile
	oldEmbeddedConfigFile := embeddedConfigFile
	oldLocalVersionFilePath := localVersionFilePath
	t.Cleanup(func() {
		updateDir = oldUpdateDir
		localConfigFile = oldLocalConfigFile
		embeddedConfigFile = oldEmbeddedConfigFile
		localVersionFilePath = oldLocalVersionFilePath
	})

	root := t.TempDir()
	updateDir = filepath.Join(root, "updates")
	localConfigFile = filepath.Join(root, "missing-local.json")
	embeddedConfigFile = filepath.Join(root, "missing-embedded.json")
	localVersionFilePath = filepath.Join(root, "missing-version")

	t.Setenv("PTNEXUS_VERSION", "v4.0.23")
	t.Setenv("VERSION_FILE", "")

	details := getLocalVersionDetails()
	if details.Version != "v4.0.23" {
		t.Fatalf("expected env version v4.0.23, got %#v", details)
	}
	if details.Source != "env:PTNEXUS_VERSION" {
		t.Fatalf("expected env source, got %#v", details)
	}
}

func TestBuildUpdateDecisionRuntimeArtifactMissing(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")
	t.Setenv(updateManifestRawFallbackEnv, "false")
	t.Setenv("PTNEXUS_VERSION", "v4.0.18")
	t.Setenv("VERSION_FILE", "")

	oldGiteeLatest := giteeManifestReleaseLatestURL
	oldGithubLatest := githubManifestReleaseLatestURL
	oldGiteeTemplate := giteeManifestReleaseURLTemplate
	oldGithubTemplate := githubManifestReleaseURLTemplate
	t.Cleanup(func() {
		giteeManifestReleaseLatestURL = oldGiteeLatest
		githubManifestReleaseLatestURL = oldGithubLatest
		giteeManifestReleaseURLTemplate = oldGiteeTemplate
		githubManifestReleaseURLTemplate = oldGithubTemplate
	})

	manifestBody := `{"schema":2,"latest":{"version":"v4.0.19","artifacts":[{"os":"unsupported-os","arch":"unsupported-arch","url":"https://example.com/runtime.tar.gz","sha256":"abc123"}]},"history":[{"version":"v4.0.19","changes":["missing platform"]}]}`
	server := newManifestTestServer(t, map[string]manifestTestResponse{
		"/latest.json": {body: manifestBody},
	})

	giteeManifestReleaseLatestURL = server.URL + "/latest.json"
	githubManifestReleaseLatestURL = server.URL + "/latest.json"
	giteeManifestReleaseURLTemplate = server.URL + "/%s.json"
	githubManifestReleaseURLTemplate = server.URL + "/%s.json"

	decision := buildUpdateDecision(updateModeRuntimeInstall)
	if !decision.HasUpdate {
		t.Fatalf("expected update to be detected, got %#v", decision)
	}
	if decision.ReasonCode != "platform_artifact_missing" {
		t.Fatalf("expected platform_artifact_missing, got %#v", decision)
	}
	if decision.PlatformReady {
		t.Fatalf("expected platform not ready, got %#v", decision)
	}
	if disable, _ := decision.UpdateControl["disable_update"].(bool); !disable {
		t.Fatalf("expected disable_update true, got %#v", decision.UpdateControl)
	}
}

func TestBuildUpdateDecisionRemoteManifestUnavailable(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")
	t.Setenv(updateManifestRawFallbackEnv, "false")
	t.Setenv("PTNEXUS_VERSION", "v4.0.18")
	t.Setenv("VERSION_FILE", "")

	oldGiteeLatest := giteeManifestReleaseLatestURL
	oldGithubLatest := githubManifestReleaseLatestURL
	oldGiteeTemplate := giteeManifestReleaseURLTemplate
	oldGithubTemplate := githubManifestReleaseURLTemplate
	t.Cleanup(func() {
		giteeManifestReleaseLatestURL = oldGiteeLatest
		githubManifestReleaseLatestURL = oldGithubLatest
		giteeManifestReleaseURLTemplate = oldGiteeTemplate
		githubManifestReleaseURLTemplate = oldGithubTemplate
	})

	server := newManifestTestServer(t, map[string]manifestTestResponse{})
	giteeManifestReleaseLatestURL = server.URL + "/missing.json"
	githubManifestReleaseLatestURL = server.URL + "/missing.json"
	giteeManifestReleaseURLTemplate = server.URL + "/missing-%s.json"
	githubManifestReleaseURLTemplate = server.URL + "/missing-%s.json"

	decision := buildUpdateDecision(updateModeRuntimeInstall)
	if decision.ReasonCode != "remote_manifest_unavailable" {
		t.Fatalf("expected remote_manifest_unavailable, got %#v", decision)
	}
	if decision.ManifestSource != "" {
		t.Fatalf("expected empty manifest source, got %#v", decision)
	}
}

func TestCheckUpdateHandlerIncludesDecisionDiagnostics(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")
	t.Setenv(updateManifestRawFallbackEnv, "false")
	t.Setenv("PTNEXUS_VERSION", "v4.0.18")
	t.Setenv("VERSION_FILE", "")

	oldGiteeLatest := giteeManifestReleaseLatestURL
	oldGithubLatest := githubManifestReleaseLatestURL
	oldGiteeTemplate := giteeManifestReleaseURLTemplate
	oldGithubTemplate := githubManifestReleaseURLTemplate
	t.Cleanup(func() {
		giteeManifestReleaseLatestURL = oldGiteeLatest
		githubManifestReleaseLatestURL = oldGithubLatest
		giteeManifestReleaseURLTemplate = oldGiteeTemplate
		githubManifestReleaseURLTemplate = oldGithubTemplate
	})

	manifestBody := fmt.Sprintf(`{"schema":2,"latest":{"version":"v4.0.19","artifacts":[{"os":%q,"arch":%q,"url":"https://example.com/runtime.tar.gz","sha256":"abc123"}]},"history":[{"version":"v4.0.19","changes":["ok"]}]}`,
		runtime.GOOS,
		runtime.GOARCH,
	)
	server := newManifestTestServer(t, map[string]manifestTestResponse{
		"/latest.json": {body: manifestBody},
	})

	giteeManifestReleaseLatestURL = server.URL + "/latest.json"
	githubManifestReleaseLatestURL = server.URL + "/latest.json"
	giteeManifestReleaseURLTemplate = server.URL + "/%s.json"
	githubManifestReleaseURLTemplate = server.URL + "/%s.json"

	req := httptest.NewRequest(http.MethodGet, "/update/check", nil)
	rec := httptest.NewRecorder()
	checkUpdateHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp updateDecision
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, rec.Body.String())
	}
	if resp.ReasonCode != "update_available" {
		t.Fatalf("expected update_available, got %#v", resp)
	}
	if resp.LocalVersionSource != "env:PTNEXUS_VERSION" {
		t.Fatalf("expected local_version_source from env, got %#v", resp)
	}
	if resp.ManifestSource != server.URL+"/latest.json" {
		t.Fatalf("expected manifest source %q, got %#v", server.URL+"/latest.json", resp)
	}
	if resp.ManifestStrategy != "latest_first" {
		t.Fatalf("expected latest_first strategy, got %#v", resp)
	}
}

func TestGetLocalVersionHandlerReturnsLocalVersionAndSource(t *testing.T) {
	t.Setenv("PTNEXUS_VERSION", "v4.0.23")
	t.Setenv("VERSION_FILE", "")

	req := httptest.NewRequest(http.MethodGet, "/update/local-version", nil)
	rec := httptest.NewRecorder()
	getLocalVersionHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp localVersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, rec.Body.String())
	}
	if !resp.Success {
		t.Fatalf("expected success true, got %#v", resp)
	}
	if resp.LocalVersion != "v4.0.23" {
		t.Fatalf("expected local_version v4.0.23, got %#v", resp)
	}
	if resp.LocalVersionSource != "env:PTNEXUS_VERSION" {
		t.Fatalf("expected local_version_source env:PTNEXUS_VERSION, got %#v", resp)
	}
	if cacheControl := rec.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "no-cache") {
		t.Fatalf("expected no-cache header, got %q", cacheControl)
	}
}
