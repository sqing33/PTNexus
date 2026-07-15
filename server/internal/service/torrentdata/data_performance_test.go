package torrentdata

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pt-nexus/server/internal/repository"
	"gorm.io/gorm"
)

func BenchmarkBatchFetchFirstPage(b *testing.B) {
	dsn := "file:" + strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(b.Name()) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		b.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = sqlDB.Close() })

	statements := []string{
		`CREATE TABLE torrents (
			hash TEXT, name TEXT NOT NULL, save_path TEXT, size INTEGER, progress REAL,
			state TEXT, sites TEXT, "group" TEXT, details TEXT, downloader_id TEXT,
			last_seen TEXT, iyuu_last_check TEXT, seeders INTEGER DEFAULT 0, is_hidden INTEGER DEFAULT 0
		)`,
		`CREATE TABLE seed_parameters (hash TEXT, name TEXT, type TEXT)`,
		`CREATE TABLE sites (nickname TEXT, migration INTEGER, cookie TEXT, base_url TEXT)`,
		`CREATE TABLE torrent_upload_stats (hash TEXT, uploaded INTEGER, is_hidden INTEGER DEFAULT 0)`,
		`CREATE INDEX idx_bench_torrents_hash ON torrents(hash)`,
		`CREATE INDEX idx_bench_seed_parameters_hash ON seed_parameters(hash)`,
		`CREATE INDEX idx_bench_upload_hash ON torrent_upload_stats(hash)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			b.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO sites (nickname, migration, cookie, base_url) VALUES ('site-0', 1, 'cookie', 'site-0.example'), ('site-1', 3, 'cookie', 'site-1.example')`).Error; err != nil {
		b.Fatal(err)
	}

	tx := db.Begin()
	for group := 0; group < 5000; group++ {
		name := fmt.Sprintf("Release.%05d", group)
		size := int64(group+1) * 1_000_000
		for site := 0; site < 2; site++ {
			hash := fmt.Sprintf("hash-%05d-%d", group, site)
			if err := tx.Exec(`INSERT INTO torrents (hash, name, save_path, size, progress, state, sites, downloader_id, last_seen, seeders, is_hidden) VALUES (?, ?, ?, ?, 100, '做种中', ?, 'qb', '2026-07-15 00:00:00', 1, 0)`,
				hash, name, "/downloads/movies", size, fmt.Sprintf("site-%d", site)).Error; err != nil {
				b.Fatal(err)
			}
			if err := tx.Exec(`INSERT INTO torrent_upload_stats (hash, uploaded, is_hidden) VALUES (?, ?, 0)`, hash, group+site).Error; err != nil {
				b.Fatal(err)
			}
		}
		if group%3 == 0 {
			if err := tx.Exec(`INSERT INTO seed_parameters (hash, name, type) VALUES (?, ?, 'category.movie')`, fmt.Sprintf("hash-%05d-0", group), name).Error; err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := tx.Commit().Error; err != nil {
		b.Fatal(err)
	}

	service := NewTorrentDataService(repository.NewTorrentDataRepository(&repository.Store{DB: db, DBType: "sqlite"}), nil)
	b.ResetTimer()
	b.Run("List", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			result, err := service.GetData(TorrentsDataParams{
				Page:            1,
				PageSize:        20,
				ExcludeExisting: true,
				OnlyCompleted:   true,
				SkipMetadata:    true,
			})
			if err != nil {
				b.Fatal(err)
			}
			if len(result["data"].([]map[string]any)) != 20 {
				b.Fatalf("unexpected page data: %#v", result["data"])
			}
		}
	})
	b.Run("Metadata", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			result, err := service.GetData(TorrentsDataParams{
				Page:          1,
				PageSize:      1,
				OnlyCompleted: true,
				MetadataOnly:  true,
			})
			if err != nil {
				b.Fatal(err)
			}
			if len(result["unique_paths"].([]string)) != 1 {
				b.Fatalf("unexpected paths: %#v", result["unique_paths"])
			}
		}
	})
}
