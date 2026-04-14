package crossseed

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pt-nexus/server/internal/repository"
	"gorm.io/gorm"
)

func newCrossSeedTestStore(t *testing.T) *repository.Store {
	t.Helper()

	dsn := "file:" + strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(t.Name()) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	statements := []string{
		`CREATE TABLE sites (
			site TEXT,
			nickname TEXT,
			migration INTEGER DEFAULT 1
		)`,
		`CREATE TABLE seed_parameters (
			hash TEXT NOT NULL,
			torrent_id TEXT NOT NULL,
			site_name TEXT NOT NULL,
			nickname TEXT,
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
			is_reviewed INTEGER DEFAULT 0,
			created_at TEXT,
			updated_at TEXT
		)`,
		`CREATE TABLE torrents (
			hash TEXT NOT NULL,
			name TEXT NOT NULL,
			size INTEGER,
			save_path TEXT,
			downloader_id TEXT,
			state TEXT,
			last_seen TEXT,
			sites TEXT,
			is_hidden INTEGER DEFAULT 0
		)`,
	}

	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create test schema failed: %v", err)
		}
	}

	return &repository.Store{DB: db, DBType: "sqlite"}
}

func mustExecCrossSeedTest(t *testing.T, db *gorm.DB, query string, args ...any) {
	t.Helper()
	if err := db.Exec(query, args...).Error; err != nil {
		t.Fatalf("exec %q failed: %v", query, err)
	}
}

func TestQueryDataExcludeTargetSitesMatchesSiteAliases(t *testing.T) {
	store := newCrossSeedTestStore(t)
	repo := repository.NewCrossSeedRepository(store)
	service := NewCrossSeedService(repo)

	mustExecCrossSeedTest(t, store.DB, `INSERT INTO sites (site, nickname, migration) VALUES (?, ?, ?)`, "mteam", "M-Team", 2)
	mustExecCrossSeedTest(t, store.DB, `INSERT INTO sites (site, nickname, migration) VALUES (?, ?, ?)`, "hdsky", "HDSky", 1)

	mustExecCrossSeedTest(t, store.DB, `INSERT INTO seed_parameters (
		hash, torrent_id, site_name, nickname, title, subtitle, type, medium, video_codec,
		audio_codec, resolution, team, source, tags, title_components,
		screenshot_review_status, is_reviewed, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"source-hash", "1001", "hdsky", "HDSky", "Movie", "", "movie", "bluray", "h264",
		"dts", "1080p", "", "", "[]", "[]", "none", 1, "2026-04-14 10:00:00", "2026-04-14 10:00:00")

	mustExecCrossSeedTest(t, store.DB, `INSERT INTO torrents (hash, name, size, save_path, downloader_id, state, last_seen, sites, is_hidden)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		"source-hash", "Movie", 100, "/downloads", "qb-1", "做种中", "2026-04-14 10:00:00", "HDSky")
	mustExecCrossSeedTest(t, store.DB, `INSERT INTO torrents (hash, name, size, save_path, downloader_id, state, last_seen, sites, is_hidden)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		"target-hash", "Movie", 100, "/downloads", "qb-1", "未做种", "2026-04-14 10:00:00", "M-Team")

	result, err := service.QueryData(CrossSeedQueryParams{
		Page:               1,
		PageSize:           20,
		ExcludeTargetSites: "mteam",
	})
	if err != nil {
		t.Fatalf("QueryData returned error: %v", err)
	}

	total, ok := result["total"].(int)
	if !ok {
		t.Fatalf("expected int total, got %#v", result["total"])
	}
	if total != 0 {
		t.Fatalf("expected total 0 when target alias already exists, got %d", total)
	}
}
