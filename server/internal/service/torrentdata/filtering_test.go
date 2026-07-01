package torrentdata

import (
	"strconv"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pt-nexus/server/internal/repository"
	"gorm.io/gorm"
)

func newTorrentDataFilterTestService(t *testing.T) (*TorrentDataService, *gorm.DB) {
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
			size INTEGER,
			is_hidden INTEGER DEFAULT 0
		)`,
		`CREATE TABLE seed_parameters (
			hash TEXT,
			name TEXT
		)`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create test schema failed: %v", err)
		}
	}

	store := &repository.Store{DB: db, DBType: "sqlite"}
	return &TorrentDataService{repo: repository.NewTorrentDataRepository(store)}, db
}

func mustExecTorrentDataFilterTest(t *testing.T, db *gorm.DB, query string, args ...any) {
	t.Helper()
	if err := db.Exec(query, args...).Error; err != nil {
		t.Fatalf("exec %q failed: %v", query, err)
	}
}

func TestApplyFiltersExcludeExistingUsesNameAndSize(t *testing.T) {
	service, db := newTorrentDataFilterTestService(t)

	mustExecTorrentDataFilterTest(t, db, `INSERT INTO torrents (hash, name, size, is_hidden) VALUES (?, ?, ?, 0)`,
		"hash-existing", "Same.Name", 1000)
	mustExecTorrentDataFilterTest(t, db, `INSERT INTO seed_parameters (hash, name) VALUES (?, ?)`,
		"hash-existing", "Same.Name")

	data := []map[string]any{
		{"name": "Same.Name", "size": int64(1000)},
		{"name": "Same.Name", "size": int64(2000)},
		{"name": "Other.Name", "size": int64(3000)},
	}

	filtered := service.applyFilters(data, TorrentsDataParams{ExcludeExisting: true}, nil)
	names := make([]string, 0, len(filtered))
	for _, item := range filtered {
		names = append(names, stringValue(item["name"], "")+"_"+strconv.FormatInt(int64Value(item["size"], 0), 10))
	}

	if len(filtered) != 2 {
		t.Fatalf("expected 2 rows after name+size exclusion, got %d: %#v", len(filtered), filtered)
	}
	if !containsString(names, "Same.Name_2000") {
		t.Fatalf("expected same name with different size to remain, got %#v", names)
	}
	if !containsString(names, "Other.Name_3000") {
		t.Fatalf("expected unrelated name to remain, got %#v", names)
	}
	if containsString(names, "Same.Name_1000") {
		t.Fatalf("expected exact existing name+size to be removed, got %#v", names)
	}
}
