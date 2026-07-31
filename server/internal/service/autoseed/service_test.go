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

func TestRejectReasonMatchesNormalizedType(t *testing.T) {
	rule := &repository.AutoSeedRule{TypesJSON: `["电影"]`}
	item := &repository.AutoSeedItem{
		TorrentURL:   "https://example.test/download.php?id=1",
		ResourceType: "category.movie",
	}

	if got := rejectReason(rule, item); got != "" {
		t.Fatalf("rejectReason() = %q, want empty", got)
	}
}
