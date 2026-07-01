package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildMediaPathCandidatesAllowsDirScanForNonMatchingSavePath(t *testing.T) {
	candidates := buildMediaPathCandidates(
		[]string{filepath.Join("downloads", "actual-dir")},
		"The.Fire.Raven.2025.2160p.WEB-DL.H265.HDR.DDP5.1-HHWEB",
		"The Fire Raven 2025 2160p WEB-DL H265 HDR DDP5.1-HHWEB",
	)

	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(candidates))
	}

	last := candidates[2]
	if last.Source != "save_path" {
		t.Fatalf("expected last candidate to use save_path source, got %s", last.Source)
	}
	if !last.AllowDirScan {
		t.Fatalf("expected save_path candidate to allow dir scan")
	}
}

func TestBuildMediaPathCandidatesPreservesPriorityOrder(t *testing.T) {
	savePath := filepath.Join("downloads", "root")
	torrentName := "Movie.2024.2160p"
	contentName := "Movie"

	candidates := buildMediaPathCandidates([]string{savePath}, torrentName, contentName)
	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(candidates))
	}

	if got := candidates[0].Path; got != filepath.Join(savePath, torrentName) {
		t.Fatalf("unexpected torrent candidate path: %s", got)
	}
	if got := candidates[1].Path; got != filepath.Join(savePath, contentName) {
		t.Fatalf("unexpected content candidate path: %s", got)
	}
	if got := candidates[2].Path; got != savePath {
		t.Fatalf("unexpected save_path candidate path: %s", got)
	}
}

func TestBuildMediaPathCandidatesHandlesSingleFileSavePath(t *testing.T) {
	savePath := filepath.Join("downloads", "movie.mkv")
	candidates := buildMediaPathCandidates([]string{savePath}, "", "")
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Path != savePath {
		t.Fatalf("unexpected candidate path: %s", candidates[0].Path)
	}
	if !candidates[0].AllowDirScan {
		t.Fatalf("expected save_path candidate to allow dir scan")
	}
}

func TestFindMediaSiblingByBaseNameMatchesSingleMediaFile(t *testing.T) {
	tempDir := t.TempDir()
	base := "Sleepwalkers.2021.BluRay.1080p.DD5.1.x264-BMDru"
	matchedPath := filepath.Join(tempDir, base+".mkv")
	if err := os.WriteFile(matchedPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write matched file: %v", err)
	}

	resolved, err := findMediaSiblingByBaseName(filepath.Join(tempDir, base), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != matchedPath {
		t.Fatalf("expected %s, got %s", matchedPath, resolved)
	}
}

func TestFindMediaSiblingByBaseNameRejectsMultipleMatches(t *testing.T) {
	tempDir := t.TempDir()
	base := "Sleepwalkers.2021.BluRay.1080p.DD5.1.x264-BMDru"
	for _, ext := range []string{".mkv", ".mp4"} {
		if err := os.WriteFile(filepath.Join(tempDir, base+ext), []byte("ok"), 0o644); err != nil {
			t.Fatalf("write test file: %v", err)
		}
	}

	if _, err := findMediaSiblingByBaseName(filepath.Join(tempDir, base), true); err == nil {
		t.Fatalf("expected multiple match error")
	}
}
