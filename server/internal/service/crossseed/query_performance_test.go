package crossseed

import (
	"fmt"
	"testing"

	"github.com/pt-nexus/server/internal/repository"
)

func BenchmarkQueryDataFirstPage(b *testing.B) {
	store := newCrossSeedTestStore(b)
	repo := repository.NewCrossSeedRepository(store)
	service := NewCrossSeedService(repo)

	indexes := []string{
		`CREATE INDEX idx_bench_torrents_hash ON torrents(hash)`,
		`CREATE INDEX idx_bench_seed_parameters_created ON seed_parameters(created_at)`,
	}
	for _, statement := range indexes {
		if err := store.DB.Exec(statement).Error; err != nil {
			b.Fatal(err)
		}
	}
	if err := store.DB.Exec(`INSERT INTO sites (site, nickname, migration) VALUES ('target', 'Target', 2)`).Error; err != nil {
		b.Fatal(err)
	}

	tx := store.DB.Begin()
	for item := 0; item < 10000; item++ {
		hash := fmt.Sprintf("hash-%05d", item)
		torrentID := fmt.Sprintf("%05d", item)
		title := fmt.Sprintf("Release.%05d", item)
		createdAt := fmt.Sprintf("2026-07-15 00:%02d:%02d", item%60, item%60)
		if err := tx.Exec(`INSERT INTO seed_parameters (
			hash, torrent_id, site_name, nickname, title, subtitle, type, medium, video_codec,
			audio_codec, resolution, team, source, tags, title_components,
			screenshot_review_status, is_reviewed, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			hash, torrentID, "site", "Site", title, "", "category.movie", "medium.bluray", "video.h264",
			"audio.dts", "resolution.r1080p", "team.other", "", "[]", "[]", "none", 1, createdAt, createdAt).Error; err != nil {
			b.Fatal(err)
		}
		for copyIndex := 0; copyIndex < 2; copyIndex++ {
			if err := tx.Exec(`INSERT INTO torrents (hash, name, size, save_path, downloader_id, state, last_seen, sites, is_hidden)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
				hash, title, item+1, "/downloads", fmt.Sprintf("qb-%d", copyIndex), "做种中", fmt.Sprintf("2026-07-15 00:00:0%d", copyIndex), "Site").Error; err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := tx.Commit().Error; err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.Run("Reviewed", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			result, err := service.QueryData(CrossSeedQueryParams{Page: 1, PageSize: 20, ReviewStatus: "reviewed"})
			if err != nil {
				b.Fatal(err)
			}
			if result["total"].(int) != 10000 {
				b.Fatalf("unexpected total: %#v", result["total"])
			}
		}
	})
	b.Run("ReviewedExcludeTarget", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			result, err := service.QueryData(CrossSeedQueryParams{
				Page:               1,
				PageSize:           20,
				ReviewStatus:       "reviewed",
				ExcludeTargetSites: "target",
			})
			if err != nil {
				b.Fatal(err)
			}
			if result["total"].(int) != 10000 {
				b.Fatalf("unexpected total: %#v", result["total"])
			}
		}
	})
}
