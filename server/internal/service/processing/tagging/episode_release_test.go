package tagging

import "testing"

func intPtr(v int) *int {
	return &v
}

func TestDetectEpisodeTagSingleEpisodeByTitle(t *testing.T) {
	result := DetectEpisodeTag(EpisodeTagInput{
		Title: "Show.Name.S01E03.1080p.WEB-DL",
		Type:  "category.tv_series",
	})
	if !result.Matched {
		t.Fatalf("expected single-episode title to be tagged as episode")
	}
	if result.Reason == "" {
		t.Fatalf("expected reason for single-episode match")
	}
}

func TestDetectEpisodeTagSingleEpisodeBySubtitle(t *testing.T) {
	result := DetectEpisodeTag(EpisodeTagInput{
		Title:    "Show Name 2025 1080p WEB-DL",
		Subtitle: "2026年1月新番：蘑菇魔女 第11集 シャンピニオンの魔女",
		Type:     "category.animation",
	})
	if !result.Matched {
		t.Fatalf("expected subtitle episode marker to be tagged as episode")
	}
	if result.Reason != "副标题命中单集特征" {
		t.Fatalf("unexpected reason: %s", result.Reason)
	}
}

func TestDetectEpisodeTagByTorrentFilesAndIncompleteSeason(t *testing.T) {
	result := DetectEpisodeTag(EpisodeTagInput{
		Title:            "Show Name 2025 1080p WEB-DL",
		Type:             "category.tv_series",
		TorrentFileNames: []string{"Show.Name.S01E01.mkv", "Show.Name.S01E02.mkv", "Show.Name.S01E03.mkv"},
		Completion: CompletionStatus{
			TotalEpisodes: intPtr(12),
			LocalEpisodes: intPtr(3),
		},
	})
	if !result.Matched {
		t.Fatalf("expected partial season package to be tagged as episode")
	}
}

func TestDetectEpisodeTagSeasonPackShouldNotMatch(t *testing.T) {
	result := DetectEpisodeTag(EpisodeTagInput{
		Title:            "Show Name Season 1 Complete 1080p WEB-DL",
		Type:             "category.tv_series",
		TorrentFileNames: []string{"Show.Name.S01E01.mkv", "Show.Name.S01E02.mkv", "Show.Name.S01E03.mkv", "Show.Name.S01E04.mkv"},
		Completion: CompletionStatus{
			IsComplete:    true,
			Confidence:    "medium",
			TotalEpisodes: intPtr(4),
			LocalEpisodes: intPtr(4),
		},
	})
	if result.Matched {
		t.Fatalf("expected full season package not to be tagged as episode")
	}
}

func TestDetectEpisodeTagExistingTagPreserved(t *testing.T) {
	result := DetectEpisodeTag(EpisodeTagInput{
		ExistingTags: []string{"tag.分集"},
	})
	if !result.Matched {
		t.Fatalf("expected existing episode tag to be preserved")
	}
}
