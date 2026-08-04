package repair

import "testing"

func TestBuildRandomScreenshotPointsByDuration(t *testing.T) {
	points := buildRandomScreenshotPointsByDuration(120, 3)
	if len(points) != 3 {
		t.Fatalf("expected 3 points, got %d: %v", len(points), points)
	}
	for i, point := range points {
		if point <= 0 || point >= 120 {
			t.Fatalf("point out of range at %d: %v", i, points)
		}
		if i > 0 && points[i] < points[i-1] {
			t.Fatalf("points are not sorted: %v", points)
		}
	}
}
