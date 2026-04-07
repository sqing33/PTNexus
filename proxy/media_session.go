package main

import (
	"errors"
	"fmt"
	"os"
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
	if info.IsDir() || !isISOFileInput(trimmedPath) {
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
