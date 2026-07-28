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
