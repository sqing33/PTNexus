package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

func TestGetLocalVersionPrefersImageVersionOverRuntimeState(t *testing.T) {
	oldUpdateDir := updateDir
	oldLocalConfigFile := localConfigFile
	oldEmbeddedConfigFile := embeddedConfigFile
	t.Cleanup(func() {
		updateDir = oldUpdateDir
		localConfigFile = oldLocalConfigFile
		embeddedConfigFile = oldEmbeddedConfigFile
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

	t.Setenv("PTNEXUS_VERSION", "v4.0.0")
	t.Setenv("VERSION_FILE", "")

	version := getLocalVersion()
	if version != "v4.0.0" {
		t.Fatalf("expected image version v4.0.0, got %s", version)
	}
}
