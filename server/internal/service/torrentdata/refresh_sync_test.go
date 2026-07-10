package torrentdata

import (
	"testing"

	"github.com/pt-nexus/server/internal/repository"
)

func TestRefreshSiteMatcherPrefersExactHostOverSharedCore(t *testing.T) {
	matcher := newRefreshSiteMatcher([]repository.SiteIdentity{
		{Nickname: "馒头", Site: "m-team", BaseURL: "kp.m-team.cc", SpecialTrackerDomain: "manfuz"},
		{Nickname: "我堡", Site: "ourbits", BaseURL: "ourbits.club"},
	})

	got := matcher.Match(
		[]string{"https://tracker.m-team.cc/announce"},
		"https://ourbits.club/details.php?id=123",
		"https://ourbits.club/details.php?id=123",
	)
	if got != "我堡" {
		t.Fatalf("expected 我堡, got %q", got)
	}
}

func TestExtractCoreDomainKeepsRegistrableDomain(t *testing.T) {
	cases := map[string]string{
		"kp.m-team.cc":      "m-team.cc",
		"tracker.m-team.cc": "m-team.cc",
		"ourbits.club":      "ourbits.club",
	}
	for input, want := range cases {
		if got := extractCoreDomain(input); got != want {
			t.Fatalf("extractCoreDomain(%q) = %q, want %q", input, got, want)
		}
	}
}
