package main

import (
	"strings"
	"testing"
)

func TestSelectDesktopInstallerPrefersPatch(t *testing.T) {
	t.Setenv("UPDATE_ARCH", "")

	installer, err := selectDesktopInstaller([]DesktopInstallerAsset{
		{
			Platform: desktopInstallerPlatformWindows,
			Arch:     "amd64",
			Kind:     desktopInstallerKindFull,
			URL:      "https://example.com/pt-nexus-v4.0.1-amd64-installer.exe",
		},
		{
			Arch: "amd64",
			Kind: desktopInstallerKindPatch,
			URL:  "https://example.com/pt-nexus-v4.0.1-amd64-update.exe",
		},
	}, "amd64")
	if err != nil {
		t.Fatalf("selectDesktopInstaller returned error: %v", err)
	}

	if installer.Kind != desktopInstallerKindPatch {
		t.Fatalf("unexpected kind: %s", installer.Kind)
	}
	if installer.FileName != "pt-nexus-v4.0.1-amd64-update.exe" {
		t.Fatalf("unexpected file name: %s", installer.FileName)
	}
}

func TestSelectDesktopInstallerTreatsEmptyKindAsFull(t *testing.T) {
	t.Setenv("UPDATE_ARCH", "")

	installer, err := selectDesktopInstaller([]DesktopInstallerAsset{
		{
			Arch: "amd64",
			URL:  "https://example.com/pt-nexus-v4.0.1-amd64-installer.exe",
		},
	}, "amd64")
	if err != nil {
		t.Fatalf("selectDesktopInstaller returned error: %v", err)
	}

	if installer.Kind != desktopInstallerKindFull {
		t.Fatalf("unexpected kind: %s", installer.Kind)
	}
}

func TestResolveDesktopInstallerRequiresSHA256(t *testing.T) {
	t.Setenv("UPDATE_SKIP_VERIFY", "false")
	t.Setenv("UPDATE_ARCH", "amd64")

	_, err := resolveDesktopInstallerForCurrentPlatform(&UpdateManifest{
		Latest: ManifestLatest{
			Version: "v4.0.1",
			DesktopInstallers: []DesktopInstallerAsset{
				{
					Platform: desktopInstallerPlatformWindows,
					Arch:     "amd64",
					URL:      "https://example.com/pt-nexus-v4.0.1-amd64-installer.exe",
				},
			},
		},
		History: []VersionInfo{{Version: "v4.0.1"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("unexpected error: %v", err)
	}
}
