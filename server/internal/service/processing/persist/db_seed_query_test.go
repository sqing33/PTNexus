package persist

import (
	"errors"
	"testing"
)

type stubSeedQueryRepo struct {
	seedRow       map[string]any
	hashRow       map[string]any
	nameRow       map[string]any
	hashLookups   int
	nameLookups   int
	seedErr       error
	hashErr       error
	nameErr       error
}

func (s *stubSeedQueryRepo) GetSeedParameter(torrentID, siteName string) (map[string]any, error) {
	if s.seedErr != nil {
		return nil, s.seedErr
	}
	return s.seedRow, nil
}

func (s *stubSeedQueryRepo) GetCurrentTorrentByHash(hash string) (map[string]any, error) {
	s.hashLookups++
	if s.hashErr != nil {
		return nil, s.hashErr
	}
	return s.hashRow, nil
}

func (s *stubSeedQueryRepo) GetCurrentTorrentByName(name string) (map[string]any, error) {
	s.nameLookups++
	if s.nameErr != nil {
		return nil, s.nameErr
	}
	return s.nameRow, nil
}

func TestQueryAndNormalizeSeedPrefersHashLookup(t *testing.T) {
	repo := &stubSeedQueryRepo{
		seedRow: map[string]any{
			"hash":      "abc123",
			"torrent_id": "tid",
			"site_name": "mteam",
			"name":      "Same.Name",
			"title":     "Same.Name",
		},
		hashRow: map[string]any{
			"save_path":     "/wanted/path",
			"downloader_id": "qb",
		},
		nameRow: map[string]any{
			"save_path":     "/wrong/path",
			"downloader_id": "qb",
		},
	}

	normalized, _, err := QueryAndNormalizeSeed(repo, "tid", "mteam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := toStringSimple(normalized["save_path"]); got != "/wanted/path" {
		t.Fatalf("expected hash-matched save_path, got %q", got)
	}
	if repo.hashLookups != 1 {
		t.Fatalf("expected 1 hash lookup, got %d", repo.hashLookups)
	}
	if repo.nameLookups != 0 {
		t.Fatalf("expected 0 name lookups when hash succeeds, got %d", repo.nameLookups)
	}
}

func TestQueryAndNormalizeSeedFallsBackToNameLookup(t *testing.T) {
	repo := &stubSeedQueryRepo{
		seedRow: map[string]any{
			"hash":      "abc123",
			"torrent_id": "tid",
			"site_name": "mteam",
			"name":      "Same.Name",
			"title":     "Same.Name",
		},
		hashErr: errors.New("not found"),
		nameRow: map[string]any{
			"save_path":     "/fallback/path",
			"downloader_id": "qb",
		},
	}

	normalized, _, err := QueryAndNormalizeSeed(repo, "tid", "mteam")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := toStringSimple(normalized["save_path"]); got != "/fallback/path" {
		t.Fatalf("expected name fallback save_path, got %q", got)
	}
	if repo.hashLookups != 1 || repo.nameLookups != 1 {
		t.Fatalf("expected one hash and one name lookup, got hash=%d name=%d", repo.hashLookups, repo.nameLookups)
	}
}
