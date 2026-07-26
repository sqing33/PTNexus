package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type MediaSession struct {
	OriginalPath string
	ResolvedPath string
	Mounted      bool
	OwnedMount   bool

	closeFn func() error
}

func (s *MediaSession) Close() error {
	if s == nil || s.closeFn == nil {
		return nil
	}
	closeFn := s.closeFn
	s.closeFn = nil
	return closeFn()
}

func OpenMediaSession(rawPath string, scene string) (*MediaSession, error) {
	trimmedPath := normalizePath(rawPath)
	if trimmedPath == "" {
		return nil, errors.New("media path is empty")
	}

	info, err := os.Stat(trimmedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access media path: %w", err)
	}
	if info.IsDir() {
		if isoPath, found := findLargestDirectISOFile(trimmedPath); found {
			sceneName := normalizeMediaScene(scene)
			log.Printf("%s: found ISO file in directory root=%s iso=%s", sceneName, trimmedPath, isoPath)
			return openISOSession(isoPath, sceneName)
		}
		return newPassthroughMediaSession(trimmedPath), nil
	}
	if !isISOFileInput(trimmedPath) {
		return newPassthroughMediaSession(trimmedPath), nil
	}
	return openISOSession(trimmedPath, normalizeMediaScene(scene))
}

func newPassthroughMediaSession(path string) *MediaSession {
	trimmed := normalizePath(path)
	return &MediaSession{
		OriginalPath: trimmed,
		ResolvedPath: trimmed,
		Mounted:      false,
		OwnedMount:   false,
		closeFn:      func() error { return nil },
	}
}

func normalizeMediaScene(scene string) string {
	trimmed := strings.TrimSpace(scene)
	if trimmed == "" {
		return "media access"
	}
	return trimmed
}

func findLargestDirectISOFile(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}

	bestPath := ""
	var bestSize int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".iso") {
			continue
		}
		candidatePath := filepath.Join(dir, entry.Name())
		info, statErr := entry.Info()
		if statErr != nil || info == nil || info.Size() <= 0 {
			continue
		}
		if bestPath == "" || info.Size() > bestSize {
			bestPath = candidatePath
			bestSize = info.Size()
		}
	}
	return bestPath, bestPath != ""
}
