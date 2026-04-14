package repository

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newStatsRepositoryTestStore(t *testing.T) *Store {
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
			"group" TEXT
		)`,
		`CREATE TABLE torrents (
			hash TEXT,
			name TEXT NOT NULL,
			size INTEGER,
			sites TEXT,
			"group" TEXT,
			downloader_id TEXT,
			is_hidden INTEGER DEFAULT 0
		)`,
	}

	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create test schema failed: %v", err)
		}
	}

	return &Store{DB: db, DBType: "sqlite"}
}

func mustExecStatsTest(t *testing.T, db *gorm.DB, query string, args ...any) {
	t.Helper()
	if err := db.Exec(query, args...).Error; err != nil {
		t.Fatalf("exec %q failed: %v", query, err)
	}
}

func TestQueryGroupStatsFiltersBySelectedSourceSite(t *testing.T) {
	store := newStatsRepositoryTestStore(t)
	repo := NewStatsRepository(store)

	mustExecStatsTest(t, store.DB, `INSERT INTO sites (site, nickname, "group") VALUES (?, ?, ?)`, "mteam", "M-Team", "-MTeam")
	mustExecStatsTest(t, store.DB, `INSERT INTO sites (site, nickname, "group") VALUES (?, ?, ?)`, "other", "Other", "-Other")

	mustExecStatsTest(t, store.DB, `INSERT INTO torrents (hash, name, size, sites, "group", downloader_id, is_hidden) VALUES (?, ?, ?, ?, ?, ?, 0)`,
		"hash-1", "Wanted", 100, "M-Team", "-MTeam", "qb-1")
	mustExecStatsTest(t, store.DB, `INSERT INTO torrents (hash, name, size, sites, "group", downloader_id, is_hidden) VALUES (?, ?, ?, ?, ?, ?, 0)`,
		"hash-2", "Leaked", 200, "Other", "-MTeam", "qb-2")

	rows, err := repo.QueryGroupStats("M-Team")
	if err != nil {
		t.Fatalf("QueryGroupStats returned error: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %#v", len(rows), rows)
	}

	row := rows[0]
	if row.SiteName != "M-Team" {
		t.Fatalf("expected site_name M-Team, got %q", row.SiteName)
	}
	if row.GroupSuffix != "MTeam" {
		t.Fatalf("expected group_suffix MTeam, got %q", row.GroupSuffix)
	}
	if row.TorrentCount != 1 {
		t.Fatalf("expected torrent_count 1, got %d", row.TorrentCount)
	}
	if row.TotalSize != 100 {
		t.Fatalf("expected total_size 100, got %d", row.TotalSize)
	}
}

func TestQueryGroupStatsDeduplicatesByNameAndSizeAndSkipsEmptyNormalizedGroup(t *testing.T) {
	store := newStatsRepositoryTestStore(t)
	repo := NewStatsRepository(store)

	mustExecStatsTest(t, store.DB, `INSERT INTO sites (site, nickname, "group") VALUES (?, ?, ?)`, "mteam", "M-Team", "-MTeam")

	mustExecStatsTest(t, store.DB, `INSERT INTO torrents (hash, name, size, sites, "group", downloader_id, is_hidden) VALUES (?, ?, ?, ?, ?, ?, 0)`,
		"hash-1", "Release", 100, "M-Team", "-MTeam", "qb-1")
	mustExecStatsTest(t, store.DB, `INSERT INTO torrents (hash, name, size, sites, "group", downloader_id, is_hidden) VALUES (?, ?, ?, ?, ?, ?, 0)`,
		"hash-2", "Release", 100, "M-Team", "-MTeam", "tr-1")
	mustExecStatsTest(t, store.DB, `INSERT INTO torrents (hash, name, size, sites, "group", downloader_id, is_hidden) VALUES (?, ?, ?, ?, ?, ?, 0)`,
		"hash-3", "Release", 200, "M-Team", "-MTeam", "qb-2")
	mustExecStatsTest(t, store.DB, `INSERT INTO torrents (hash, name, size, sites, "group", downloader_id, is_hidden) VALUES (?, ?, ?, ?, ?, ?, 0)`,
		"hash-4", "Ignored", 300, "M-Team", " - ", "qb-3")

	rows, err := repo.QueryGroupStats("M-Team")
	if err != nil {
		t.Fatalf("QueryGroupStats returned error: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %#v", len(rows), rows)
	}

	row := rows[0]
	if row.GroupSuffix != "MTeam" {
		t.Fatalf("expected group_suffix MTeam, got %q", row.GroupSuffix)
	}
	if row.TorrentCount != 2 {
		t.Fatalf("expected torrent_count 2, got %d", row.TorrentCount)
	}
	if row.TotalSize != 300 {
		t.Fatalf("expected total_size 300, got %d", row.TotalSize)
	}
}

func TestQueryGroupStatsUsesRecognizedSourceSiteWithoutSiteGroupWhitelist(t *testing.T) {
	store := newStatsRepositoryTestStore(t)
	repo := NewStatsRepository(store)

	mustExecStatsTest(t, store.DB, `INSERT INTO sites (site, nickname, "group") VALUES (?, ?, ?)`, "mteam", "M-Team", "-MTeam")

	mustExecStatsTest(t, store.DB, `INSERT INTO torrents (hash, name, size, sites, "group", downloader_id, is_hidden) VALUES (?, ?, ?, ?, ?, ?, 0)`,
		"hash-1", "Release", 100, "M-Team", "-Unexpected", "qb-1")

	rows, err := repo.QueryGroupStats("M-Team")
	if err != nil {
		t.Fatalf("QueryGroupStats returned error: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %#v", len(rows), rows)
	}

	row := rows[0]
	if row.SiteName != "M-Team" {
		t.Fatalf("expected site_name M-Team, got %q", row.SiteName)
	}
	if row.GroupSuffix != "Unexpected" {
		t.Fatalf("expected group_suffix Unexpected, got %q", row.GroupSuffix)
	}
	if row.TorrentCount != 1 || row.TotalSize != 100 {
		t.Fatalf("unexpected row: %#v", row)
	}
}
