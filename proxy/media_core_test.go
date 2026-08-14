package main

import "testing"

func TestBuildRandomScreenshotPointsForDuration(t *testing.T) {
	points := buildRandomScreenshotPointsForDuration(180, 3)
	if len(points) != 3 {
		t.Fatalf("expected 3 points, got %d: %v", len(points), points)
	}
	for i, point := range points {
		if point <= 0 || point >= 180 {
			t.Fatalf("point out of range at %d: %v", i, points)
		}
		if i > 0 && points[i] < points[i-1] {
			t.Fatalf("points are not sorted: %v", points)
		}
	}
}

func TestBuildRandomScreenshotPointsForDurationClampsCount(t *testing.T) {
	points := buildRandomScreenshotPointsForDuration(180, 99)
	if len(points) != 10 {
		t.Fatalf("expected count to be clamped to 10, got %d", len(points))
	}
}

func TestPixhostUploadConfigUsesConfiguredDomain(t *testing.T) {
	cfg := newPixhostUploadConfig("https://img99.pixhost.to/path")
	if cfg.DirectHost != "img99.pixhost.to" {
		t.Fatalf("DirectHost = %q, want img99.pixhost.to", cfg.DirectHost)
	}
	if cfg.UploadAPIURL != "https://api.pixhost.to/images" {
		t.Fatalf("UploadAPIURL = %q, want https://api.pixhost.to/images", cfg.UploadAPIURL)
	}
}

func TestNormalizePixhostShowURLUsesConfiguredDomain(t *testing.T) {
	cfg := newPixhostUploadConfig("img99.pixhost.to")
	got := normalizePixhostShowURL("https://pixhost.to/show/123/example.jpg", cfg)
	want := "https://img99.pixhost.to/images/123/example.jpg"
	if got != want {
		t.Fatalf("normalizePixhostShowURL() = %q, want %q", got, want)
	}
}
