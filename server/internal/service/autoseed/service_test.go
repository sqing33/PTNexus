package autoseed

import (
	"strings"
	"testing"

	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

func TestRestrictedTagRejectReason(t *testing.T) {
	testCases := []struct {
		name string
		item repository.AutoSeedItem
		want string
	}{
		{
			name: "explicit restricted tag",
			item: repository.AutoSeedItem{TagsJSON: `["tag.分集"]`},
			want: "因分集标签不允许下载",
		},
		{
			name: "episode marker in title",
			item: repository.AutoSeedItem{Name: "Kamen Rider ZEZTZ 2025 S01E45 1080p WEB-DL"},
			want: "因分集标签不允许下载",
		},
		{
			name: "full season title",
			item: repository.AutoSeedItem{Name: "Kamen Rider ZEZTZ 2025 S01 1080p WEB-DL"},
			want: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := restrictedTagRejectReason(&testCase.item); got != testCase.want {
				t.Fatalf("restrictedTagRejectReason() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestShouldRetryAutoSeedItem(t *testing.T) {
	testCases := []struct {
		name string
		item repository.AutoSeedItem
		want bool
	}{
		{
			name: "pending item can retry after interrupted run",
			item: repository.AutoSeedItem{Status: repository.AutoSeedItemStatusPending},
			want: true,
		},
		{
			name: "source site fetch failure can retry",
			item: repository.AutoSeedItem{
				Status:       repository.AutoSeedItemStatusRejected,
				RejectReason: "详情页数据抓取失败: 站点配置错误",
			},
			want: true,
		},
		{
			name: "downloader push failure can retry",
			item: repository.AutoSeedItem{
				Status:       repository.AutoSeedItemStatusRejected,
				RejectReason: "推送下载器失败: connection refused",
			},
			want: true,
		},
		{
			name: "restricted tag rejection stays rejected",
			item: repository.AutoSeedItem{
				Status:       repository.AutoSeedItemStatusRejected,
				RejectReason: "因分集标签不允许下载",
			},
			want: false,
		},
		{
			name: "pushed item should not be duplicated",
			item: repository.AutoSeedItem{Status: repository.AutoSeedItemStatusPushed},
			want: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := shouldRetryAutoSeedItem(&testCase.item); got != testCase.want {
				t.Fatalf("shouldRetryAutoSeedItem() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestRejectReasonIgnoresLegacyTypeAndMediumFilters(t *testing.T) {
	rule := &repository.AutoSeedRule{
		TypesJSON: `["电影"]`,
		MediaJSON: `["Blu-ray"]`,
	}
	item := &repository.AutoSeedItem{
		TorrentURL:   "https://example.test/download.php?id=1",
		ResourceType: "category.tv_series",
		Medium:       "WEB-DL",
	}

	if got := rejectReason(rule, item); got != "" {
		t.Fatalf("rejectReason() = %q, want empty", got)
	}
}

func TestNormalizeRuleClearsLegacyTypeAndMediumFilters(t *testing.T) {
	rule := &repository.AutoSeedRule{
		Name:      "rule",
		RSSURL:    "https://example.test/rss",
		TypesJSON: `["电影"]`,
		MediaJSON: `["Blu-ray"]`,
	}

	normalizeRule(rule)

	if rule.TypesJSON != "[]" || rule.MediaJSON != "[]" {
		t.Fatalf("expected legacy filters to be cleared, got types=%q media=%q", rule.TypesJSON, rule.MediaJSON)
	}
}

func TestNeedsRefreshAutoSeedScreenshotsFromSeedRow(t *testing.T) {
	testCases := []struct {
		name     string
		siteName string
		row      map[string]any
		want     bool
	}{
		{
			name:     "NovaHD always refreshes",
			siteName: "NovaHD",
			row: map[string]any{
				"screenshots":              "[img]https://example.test/1.jpg[/img]",
				"screenshot_review_status": "none",
			},
			want: true,
		},
		{
			name:     "pending review refreshes",
			siteName: "OtherSite",
			row: map[string]any{
				"screenshots":              "[img]https://example.test/1.jpg[/img]",
				"screenshot_review_status": "pending",
			},
			want: true,
		},
		{
			name:     "empty screenshots refresh",
			siteName: "OtherSite",
			row: map[string]any{
				"screenshots":              "",
				"screenshot_review_status": "none",
			},
			want: true,
		},
		{
			name:     "normal screenshots do not refresh",
			siteName: "OtherSite",
			row: map[string]any{
				"screenshots":              "[img]https://example.test/1.jpg[/img]",
				"screenshot_review_status": "none",
			},
			want: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := needsRefreshAutoSeedScreenshotsFromSeedRow(testCase.siteName, testCase.row)
			if got != testCase.want {
				t.Fatalf("needsRefreshAutoSeedScreenshotsFromSeedRow() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestRefreshAutoSeedScreenshotsRequiresConfirmedDownloaderTask(t *testing.T) {
	svc := &Service{repo: &repository.AutoSeedRepository{}}
	err := svc.refreshAutoSeedScreenshots(repository.AutoSeedItem{
		DownloaderID: "",
		Name:         "Three Old Boys 2024 2160p WEB-DL HEVC AAC 2.0-NHDWEB",
	}, "NovaHD")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "下载器中未找到已完成任务") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMatchSnapshotByHashIgnoresNameOnlyMatch(t *testing.T) {
	snapshots := []downloaderclient.TorrentSnapshot{
		{Hash: "abc123", Name: "Same Title", SavePath: "/downloads/real"},
	}
	if _, ok := matchSnapshotByHash("", snapshots); ok {
		t.Fatal("empty hash should not match")
	}
	if _, ok := matchSnapshotByHash("missing", snapshots); ok {
		t.Fatal("different hash should not match even when names may be similar")
	}
	if snapshot, ok := matchSnapshotByHash("ABC123", snapshots); !ok || snapshot.SavePath != "/downloads/real" {
		t.Fatalf("hash match failed: ok=%v snapshot=%+v", ok, snapshot)
	}
}
