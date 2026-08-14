package autoseed

import (
	"testing"

	"github.com/pt-nexus/server/internal/repository"
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
			name:     "DStudio always refreshes",
			siteName: "屌丝",
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

func TestIsDStudioSource(t *testing.T) {
	testCases := []string{"ds", "dstudio", "Depth Studio", "dstudio.me", "屌丝"}
	for _, siteName := range testCases {
		t.Run(siteName, func(t *testing.T) {
			if !isDStudioSource(siteName) {
				t.Fatalf("expected %q to be treated as DStudio source", siteName)
			}
		})
	}
}
