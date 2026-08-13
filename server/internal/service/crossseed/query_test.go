package crossseed

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pt-nexus/server/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestCrossSeedService(t *testing.T) *CrossSeedService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	stmts := []string{
		`CREATE TABLE seed_parameters (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			hash TEXT,
			torrent_id TEXT,
			site_name TEXT,
			nickname TEXT,
			name TEXT,
			title TEXT,
			subtitle TEXT,
			type TEXT,
			medium TEXT,
			video_codec TEXT,
			audio_codec TEXT,
			resolution TEXT,
			team TEXT,
			source TEXT,
			tags TEXT,
			title_components TEXT,
			screenshot_review_status TEXT,
			is_reviewed INTEGER,
			publish_at TEXT,
			last_publish_at TEXT,
			created_at TEXT,
			updated_at TEXT
		)`,
		`CREATE TABLE torrents (
			hash TEXT,
			save_path TEXT,
			downloader_id TEXT,
			state TEXT,
			last_seen TEXT,
			size INTEGER,
			seeders INTEGER,
			sites TEXT,
			is_hidden INTEGER
		)`,
		`CREATE TABLE sites (
			nickname TEXT,
			migration INTEGER,
			sort_order INTEGER
		)`,
		`CREATE TABLE publish_logs (
			torrent_id TEXT,
			source_site TEXT,
			status TEXT,
			created_at TEXT,
			updated_at TEXT
		)`,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	return NewCrossSeedService(repository.NewCrossSeedRepository(&repository.Store{DB: db, DBType: "sqlite"}), nil)
}

func TestQueryDataIncludesLastPublishAt(t *testing.T) {
	svc := newTestCrossSeedService(t)
	db := svc.repo.DB()
	if err := db.Exec(`INSERT INTO seed_parameters
		(hash, torrent_id, site_name, nickname, name, title, tags, title_components, screenshot_review_status, is_reviewed, last_publish_at, created_at, updated_at)
		VALUES ('hash-1', '100', 'source-site', 'Source Site', 'Movie.Name', 'Movie Title', '[]', '[]', 'none', 1, '2026-08-02 10:00:00', '2026-08-01 00:00:00', '2026-08-01 00:00:00')`).Error; err != nil {
		t.Fatalf("insert seed: %v", err)
	}
	if err := db.Exec(`INSERT INTO torrents
		(hash, save_path, downloader_id, state, last_seen, size, seeders, sites, is_hidden)
		VALUES ('hash-1', '/downloads/Movie.Name', 'qb', '做种', '2026-08-01 00:00:00', 1024, 3, 'Source Site', 0)`).Error; err != nil {
		t.Fatalf("insert torrent: %v", err)
	}
	if err := db.Exec(`INSERT INTO publish_logs
		(torrent_id, source_site, status, created_at, updated_at)
		VALUES
		('100', 'source-site', 'success', '2026-08-02 10:00:00', '2026-08-02 10:00:00'),
		('100', 'source-site', 'failed', '2026-08-03 10:00:00', '2026-08-03 10:00:00')`).Error; err != nil {
		t.Fatalf("insert publish logs: %v", err)
	}

	result, err := svc.QueryData(CrossSeedQueryParams{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("QueryData() error = %v", err)
	}
	rows := result["data"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if got := toString(rows[0]["last_publish_at"], ""); got != "2026-08-02 10:00:00" {
		t.Fatalf("last_publish_at = %q", got)
	}
}

func TestDeleteCrossSeedDataRecordOnly(t *testing.T) {
	svc := newTestCrossSeedService(t)
	if err := svc.repo.DB().Exec(`INSERT INTO seed_parameters
		(hash, torrent_id, site_name, nickname, name, title, tags, title_components, screenshot_review_status, is_reviewed, created_at, updated_at)
		VALUES ('hash-1', '100', 'source-site', 'Source Site', 'Movie.Name', 'Movie Title', '[]', '[]', 'none', 1, '2026-08-01 00:00:00', '2026-08-01 00:00:00')`).Error; err != nil {
		t.Fatalf("insert seed: %v", err)
	}

	result, status := svc.DeleteCrossSeedData(map[string]any{
		"torrent_id":   "100",
		"site_name":    "source-site",
		"delete_files": false,
	})
	if status != 200 || !boolFromAny(result["success"]) {
		t.Fatalf("DeleteCrossSeedData() status=%d result=%v", status, result)
	}
	var count int64
	if err := svc.repo.DB().Raw(`SELECT COUNT(*) FROM seed_parameters`).Scan(&count).Error; err != nil {
		t.Fatalf("count seeds: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected seed row deleted, count=%d", count)
	}
}
