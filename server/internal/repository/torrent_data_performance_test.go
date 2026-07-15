package repository

import (
	"fmt"
	"testing"
)

func BenchmarkListTorrentsForBatchFetch(b *testing.B) {
	store := newTorrentDataRepositoryTestStore(b)
	repo := NewTorrentDataRepository(store)

	if err := store.DB.Exec(`CREATE INDEX idx_bench_torrents_hash ON torrents(hash)`).Error; err != nil {
		b.Fatal(err)
	}
	if err := store.DB.Exec(`CREATE INDEX idx_bench_seed_parameters_hash ON seed_parameters(hash)`).Error; err != nil {
		b.Fatal(err)
	}
	tx := store.DB.Begin()
	for group := 0; group < 5000; group++ {
		name := fmt.Sprintf("Release.%05d", group)
		size := int64(group+1) * 1_000_000
		for site := 0; site < 2; site++ {
			hash := fmt.Sprintf("hash-%05d-%d", group, site)
			if err := tx.Exec(`INSERT INTO torrents (hash, name, save_path, size, progress, state, sites, downloader_id, last_seen, is_hidden) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
				hash, name, "/downloads/movies", size, 100.0, "做种中", fmt.Sprintf("site-%d", site), "qb", "2026-07-15 00:00:00").Error; err != nil {
				b.Fatal(err)
			}
		}
		if group%3 == 0 {
			if err := tx.Exec(`INSERT INTO seed_parameters (hash, name, type) VALUES (?, ?, ?)`,
				fmt.Sprintf("hash-%05d-0", group), name, "category.movie").Error; err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := tx.Commit().Error; err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		groups, err := repo.ListTorrentGroupKeysWithFilters(TorrentListFilters{
			OnlyCompleted:   true,
			ExcludeExisting: true,
		})
		if err != nil {
			b.Fatal(err)
		}
		if len(groups) > 20 {
			groups = groups[:20]
		}
		rows, err := repo.ListTorrentsWithFilters(TorrentListFilters{Groups: groups})
		if err != nil {
			b.Fatal(err)
		}
		if len(rows) == 0 {
			b.Fatal("expected rows")
		}
	}
}
