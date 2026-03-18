package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestDownloadWithSHA256RetriesRenameOnWindowsStyleFileLock(t *testing.T) {
	restore := stubDownloadFileOps(t)
	fileOpRetryEnabled = true

	var renameCalls int
	renameFileFn = func(srcPath, dstPath string) error {
		renameCalls++
		if renameCalls == 1 {
			return &os.LinkError{Op: "rename", Old: srcPath, New: dstPath, Err: syscall.Errno(32)}
		}
		return os.Rename(srcPath, dstPath)
	}
	fileOpRetrySleepFn = func(time.Duration) {}

	content := []byte("patch-installer")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	dstPath := filepath.Join(tempDir, "pt-nexus-update.exe")
	gotSHA, err := downloadWithSHA256(
		context.Background(),
		server.URL,
		dstPath,
		sha256Hex(content),
		30*time.Second,
		5*time.Second,
	)
	restore()
	if err != nil {
		t.Fatalf("downloadWithSHA256 returned error: %v", err)
	}
	if renameCalls != 2 {
		t.Fatalf("unexpected rename call count: %d", renameCalls)
	}
	if gotSHA != sha256Hex(content) {
		t.Fatalf("unexpected sha256: %s", gotSHA)
	}
	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read dst file: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("unexpected dst content: %q", string(data))
	}
	if _, err := os.Stat(dstPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file should not exist, got err=%v", err)
	}
}

func TestDownloadWithSHA256RetriesDeletingStaleTarget(t *testing.T) {
	restore := stubDownloadFileOps(t)
	fileOpRetryEnabled = true

	tempDir := t.TempDir()
	dstPath := filepath.Join(tempDir, "pt-nexus-update.exe")

	var removeCallsForDst int
	removeFileFn = func(path string) error {
		if path != dstPath {
			return os.Remove(path)
		}
		removeCallsForDst++
		if removeCallsForDst == 1 {
			return &os.PathError{Op: "remove", Path: path, Err: syscall.Errno(32)}
		}
		return os.Remove(path)
	}
	fileOpRetrySleepFn = func(time.Duration) {}

	content := []byte("fresh-installer")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	if err := os.WriteFile(dstPath, []byte("stale"), 0o644); err != nil {
		restore()
		t.Fatalf("write stale target: %v", err)
	}

	gotSHA, err := downloadWithSHA256(
		context.Background(),
		server.URL,
		dstPath,
		sha256Hex(content),
		30*time.Second,
		5*time.Second,
	)
	restore()
	if err != nil {
		t.Fatalf("downloadWithSHA256 returned error: %v", err)
	}
	if removeCallsForDst != 2 {
		t.Fatalf("unexpected dst remove call count: %d", removeCallsForDst)
	}
	if gotSHA != sha256Hex(content) {
		t.Fatalf("unexpected sha256: %s", gotSHA)
	}
	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read dst file: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("unexpected dst content: %q", string(data))
	}
}

func TestDownloadWithSHA256ReusesMatchingExistingFile(t *testing.T) {
	restore := stubDownloadFileOps(t)

	removeFileFn = func(path string) error {
		return errors.New("remove should not be called")
	}
	renameFileFn = func(srcPath, dstPath string) error {
		return errors.New("rename should not be called")
	}

	content := []byte("existing-installer")
	tempDir := t.TempDir()
	dstPath := filepath.Join(tempDir, "pt-nexus-update.exe")
	if err := os.WriteFile(dstPath, content, 0o644); err != nil {
		restore()
		t.Fatalf("write existing target: %v", err)
	}

	gotSHA, err := downloadWithSHA256(
		context.Background(),
		"http://127.0.0.1:1/should-not-be-requested",
		dstPath,
		sha256Hex(content),
		30*time.Second,
		5*time.Second,
	)
	restore()
	if err != nil {
		t.Fatalf("downloadWithSHA256 returned error: %v", err)
	}
	if gotSHA != sha256Hex(content) {
		t.Fatalf("unexpected sha256: %s", gotSHA)
	}
}

func stubDownloadFileOps(t *testing.T) func() {
	t.Helper()

	oldRename := renameFileFn
	oldRemove := removeFileFn
	oldSleep := fileOpRetrySleepFn
	oldRetryEnabled := fileOpRetryEnabled

	return func() {
		renameFileFn = oldRename
		removeFileFn = oldRemove
		fileOpRetrySleepFn = oldSleep
		fileOpRetryEnabled = oldRetryEnabled
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
