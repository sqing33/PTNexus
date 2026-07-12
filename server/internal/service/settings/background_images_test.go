package settings_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pt-nexus/server/internal/service/settings"
)

func TestEnsureDefaultBackgroundsSeedsTwoImages(t *testing.T) {
	dir := t.TempDir()
	svc := settings.NewSettingsServiceWithDataDir(nil, dir)
	if err := svc.EnsureDefaultBackgrounds(); err != nil {
		t.Fatalf("EnsureDefaultBackgrounds: %v", err)
	}
	items, err := svc.ListBackgroundImages()
	if err != nil {
		t.Fatalf("ListBackgroundImages: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("expected at least 2 default backgrounds, got %d", len(items))
	}
	for _, item := range items {
		if item.URL == "" || item.Name == "" {
			t.Fatalf("invalid item: %+v", item)
		}
		full := filepath.Join(dir, "backgrounds", item.Name)
		if _, err := os.Stat(full); err != nil {
			t.Fatalf("missing file %s: %v", full, err)
		}
	}
	// second call should not duplicate
	if err := svc.EnsureDefaultBackgrounds(); err != nil {
		t.Fatalf("EnsureDefaultBackgrounds second: %v", err)
	}
	items2, err := svc.ListBackgroundImages()
	if err != nil {
		t.Fatalf("List second: %v", err)
	}
	if len(items2) != len(items) {
		t.Fatalf("expected no duplicate seed, before=%d after=%d", len(items), len(items2))
	}
}