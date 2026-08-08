package tagging

import "testing"

func TestMapTagsToStandardKeepsHDR10(t *testing.T) {
	mapped, unmapped := MapTagsToStandard([]string{"HDR10", "HDR"}, "")
	if len(unmapped) > 0 {
		t.Fatalf("expected all HDR tags to map, got unmapped=%v", unmapped)
	}

	if !containsString(mapped, "tag.HDR10") {
		t.Fatalf("expected tag.HDR10 in mapped tags, got %v", mapped)
	}
	if containsString(mapped, "tag.HDR") {
		t.Fatalf("expected generic tag.HDR to be removed when tag.HDR10 is present, got %v", mapped)
	}
}

func TestMapTagsToStandardMapsHighScore(t *testing.T) {
	mapped, unmapped := MapTagsToStandard([]string{"高分"}, "")
	if len(unmapped) > 0 {
		t.Fatalf("expected high score tag to map, got unmapped=%v", unmapped)
	}
	if !containsString(mapped, "tag.高分") {
		t.Fatalf("expected tag.高分 in mapped tags, got %v", mapped)
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
