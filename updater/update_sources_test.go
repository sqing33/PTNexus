package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestCandidatesIncludeGitee(t *testing.T) {
	t.Setenv("UPDATE_MANIFEST_URL", "")

	version := "v4.0.2"
	candidates := manifestCandidates(version)
	if len(candidates) == 0 {
		t.Fatal("expected non-empty candidates")
	}

	expectedGitHub := fmt.Sprintf(githubManifestReleaseURLTemplate, version)
	expectedGitee := fmt.Sprintf(giteeManifestReleaseURLTemplate, version)
	if candidates[0] != expectedGitHub {
		t.Fatalf("expected first candidate %q, got %q", expectedGitHub, candidates[0])
	}
	if candidates[1] != expectedGitee {
		t.Fatalf("expected second candidate %q, got %q", expectedGitee, candidates[1])
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
