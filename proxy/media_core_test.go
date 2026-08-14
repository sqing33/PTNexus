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
