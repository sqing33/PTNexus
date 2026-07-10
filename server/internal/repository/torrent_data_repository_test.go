package repository

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTorrentDataRepositoryTestStore(t *testing.T) *Store {
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
		`CREATE TABLE torrents (
			hash TEXT,
			name TEXT NOT NULL,
			save_path TEXT,
			size INTEGER,
			progress REAL,
			state TEXT,
			sites TEXT,
			"group" TEXT,
			details TEXT,
			downloader_id TEXT,
			last_seen TEXT,
			iyuu_last_check TEXT,
			seeders INTEGER DEFAULT 0,
			is_hidden INTEGER DEFAULT 0
		)`,
		`CREATE TABLE seed_parameters (
			hash TEXT,
			name TEXT,
			type TEXT
		)`,
	}

	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create test schema failed: %v", err)
		}
	}

	return &Store{DB: db, DBType: "sqlite"}
}

func mustExecTorrentDataRepoTest(t *testing.T, db *gorm.DB, query string, args ...any) {
	t.Helper()
	if err := db.Exec(query, args...).Error; err != nil {
		t.Fatalf("exec %q failed: %v", query, err)
	}
}

func TestListTorrentsOnlyCompletedKeepsWholeCompletedGroup(t *testing.T) {
	store := newTorrentDataRepositoryTestStore(t)
	repo := NewTorrentDataRepository(store)

	mustExecTorrentDataRepoTest(t, store.DB, `INSERT INTO torrents (hash, name, save_path, size, progress, state, sites, downloader_id, last_seen, is_hidden) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		"hash-completed", "Release", "/m", 1000, 100.0, "做种中", "憨憨", "qb", "2026-07-01 00:00:00")
	mustExecTorrentDataRepoTest(t, store.DB, `INSERT INTO torrents (hash, name, save_path, size, progress, state, sites, downloader_id, last_seen, is_hidden) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		"hash-iyuu-source", "Release", "/m", 1000, 0.0, "未做种", "猫站", "qb", "2026-07-01 00:00:00")
	mustExecTorrentDataRepoTest(t, store.DB, `INSERT INTO torrents (hash, name, save_path, size, progress, state, sites, downloader_id, last_seen, is_hidden) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		"hash-incomplete", "Other", "/m", 2000, 0.0, "下载中", "憨憨", "qb", "2026-07-01 00:00:00")

	rows, err := repo.ListTorrents(true, false)
	if err != nil {
		t.Fatalf("ListTorrents returned error: %v", err)
	}

	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.Hash] = true
	}

	if !seen["hash-completed"] {
		t.Fatalf("expected completed row to be returned, got %#v", rows)
	}
	if !seen["hash-iyuu-source"] {
		t.Fatalf("expected same name+size IYUU source row to be returned, got %#v", rows)
	}
	if seen["hash-incomplete"] {
		t.Fatalf("expected incomplete-only group to be filtered out, got %#v", rows)
	}
}

func TestListTorrentsVideoFilterDefaultsToAllContent(t *testing.T) {
	store := newTorrentDataRepositoryTestStore(t)
	repo := NewTorrentDataRepository(store)

	for _, row := range []struct {
		hash string
		name string
	}{
		{hash: "video-hash", name: "Video"},
		{hash: "music-hash", name: "Music"},
		{hash: "unknown-hash", name: "Unknown"},
	} {
		mustExecTorrentDataRepoTest(t, store.DB, `INSERT INTO torrents (hash, name, save_path, size, progress, state, sites, downloader_id, last_seen, is_hidden) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			row.hash, row.name, "/data", 1000, 100.0, "做种中", "站点", "qb", "2026-07-10 00:00:00")
	}
	mustExecTorrentDataRepoTest(t, store.DB, `INSERT INTO seed_parameters (hash, name, type) VALUES (?, ?, ?)`, "video-hash", "Video", "category.movie")
	mustExecTorrentDataRepoTest(t, store.DB, `INSERT INTO seed_parameters (hash, name, type) VALUES (?, ?, ?)`, "music-hash", "Music", "category.music")

	allRows, err := repo.ListTorrents(false, false)
	if err != nil {
		t.Fatalf("ListTorrents all content returned error: %v", err)
	}
	if len(allRows) != 3 {
		t.Fatalf("all content count = %d, want 3", len(allRows))
	}

	videoRows, err := repo.ListTorrents(false, true)
	if err != nil {
		t.Fatalf("ListTorrents video-only returned error: %v", err)
	}
	if len(videoRows) != 1 || videoRows[0].Hash != "video-hash" {
		t.Fatalf("unexpected video-only rows: %#v", videoRows)
	}
}

func TestExistingSeedParameterGroupsUsesNameAndSize(t *testing.T) {
	store := newTorrentDataRepositoryTestStore(t)
	repo := NewTorrentDataRepository(store)

	mustExecTorrentDataRepoTest(t, store.DB, `INSERT INTO torrents (hash, name, save_path, size, progress, state, sites, downloader_id, last_seen, is_hidden) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		"hash-existing", "Same.Name", "/m", 1000, 100.0, "做种中", "憨憨", "qb", "2026-07-01 00:00:00")
	mustExecTorrentDataRepoTest(t, store.DB, `INSERT INTO torrents (hash, name, save_path, size, progress, state, sites, downloader_id, last_seen, is_hidden) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		"hash-new-size", "Same.Name", "/m", 2000, 100.0, "做种中", "猫站", "qb", "2026-07-01 00:00:00")
	mustExecTorrentDataRepoTest(t, store.DB, `INSERT INTO seed_parameters (hash, name, type) VALUES (?, ?, ?)`,
		"hash-existing", "Same.Name", "category.movie")

	groups, err := repo.ExistingSeedParameterGroups()
	if err != nil {
		t.Fatalf("ExistingSeedParameterGroups returned error: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 existing group, got %d: %#v", len(groups), groups)
	}
	if groups[0].Name != "Same.Name" || groups[0].Size != 1000 {
		t.Fatalf("expected Same.Name size 1000, got %#v", groups[0])
	}
}
