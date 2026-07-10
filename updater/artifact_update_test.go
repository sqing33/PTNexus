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
	"strings"
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

func TestMovePathWithCrossDeviceFallbackUsesRenameWhenPossible(t *testing.T) {
	restore := stubRuntimePathOps(t)
	defer restore()

	tempDir := t.TempDir()
	srcPath := filepath.Join(tempDir, "server")
	dstPath := filepath.Join(tempDir, "server.image")
	if err := os.Mkdir(srcPath, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcPath, "server"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write src file: %v", err)
	}

	var renameCalls int
	renamePathFn = func(src, dst string) error {
		renameCalls++
		return os.Rename(src, dst)
	}
	copyPathForCrossDeviceFn = func(src, dst string) error {
		return errors.New("copy fallback should not be called")
	}
	removeAllPathFn = func(path string) error {
		return errors.New("removeAll should not be called")
	}

	if err := movePathWithCrossDeviceFallback(srcPath, dstPath); err != nil {
		t.Fatalf("movePathWithCrossDeviceFallback returned error: %v", err)
	}
	if renameCalls != 1 {
		t.Fatalf("unexpected rename call count: %d", renameCalls)
	}
	if _, err := os.Stat(dstPath); err != nil {
		t.Fatalf("stat dst path: %v", err)
	}
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Fatalf("src path should not exist, err=%v", err)
	}
}

func TestMovePathWithCrossDeviceFallbackCopiesOnEXDEV(t *testing.T) {
	restore := stubRuntimePathOps(t)
	defer restore()

	tempDir := t.TempDir()
	srcPath := filepath.Join(tempDir, "server")
	dstPath := filepath.Join(tempDir, "server.image")
	if err := os.MkdirAll(filepath.Join(srcPath, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcPath, "nested", "server"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	var renameCalls int
	renamePathFn = func(src, dst string) error {
		renameCalls++
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: syscall.EXDEV}
	}

	if err := movePathWithCrossDeviceFallback(srcPath, dstPath); err != nil {
		t.Fatalf("movePathWithCrossDeviceFallback returned error: %v", err)
	}
	if renameCalls != 1 {
		t.Fatalf("unexpected rename call count: %d", renameCalls)
	}
	data, err := os.ReadFile(filepath.Join(dstPath, "nested", "server"))
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(data) != "binary" {
		t.Fatalf("unexpected copied content: %q", string(data))
	}
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Fatalf("src path should be removed, err=%v", err)
	}
}

func TestMovePathWithCrossDeviceFallbackPreservesSourceOnCopyFailure(t *testing.T) {
	restore := stubRuntimePathOps(t)
	defer restore()

	tempDir := t.TempDir()
	srcPath := filepath.Join(tempDir, "server")
	dstPath := filepath.Join(tempDir, "server.image")
	if err := os.Mkdir(srcPath, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	renamePathFn = func(src, dst string) error {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: syscall.EXDEV}
	}
	copyPathForCrossDeviceFn = func(src, dst string) error {
		return errors.New("copy boom")
	}

	err := movePathWithCrossDeviceFallback(srcPath, dstPath)
	if err == nil || !strings.Contains(err.Error(), "跨设备复制") {
		t.Fatalf("expected cross-device copy error, got: %v", err)
	}
	if _, statErr := os.Stat(srcPath); statErr != nil {
		t.Fatalf("src path should remain, err=%v", statErr)
	}
	if _, statErr := os.Stat(dstPath); !os.IsNotExist(statErr) {
		t.Fatalf("dst path should be cleaned up, err=%v", statErr)
	}
}

func TestMovePathWithCrossDeviceFallbackReturnsCleanupError(t *testing.T) {
	restore := stubRuntimePathOps(t)
	defer restore()

	tempDir := t.TempDir()
	srcPath := filepath.Join(tempDir, "server")
	dstPath := filepath.Join(tempDir, "server.image")
	if err := os.Mkdir(srcPath, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcPath, "server"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write src file: %v", err)
	}

	renamePathFn = func(src, dst string) error {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: syscall.EXDEV}
	}
	removeAllPathFn = func(path string) error {
		if path == srcPath {
			return errors.New("cleanup boom")
		}
		return os.RemoveAll(path)
	}

	err := movePathWithCrossDeviceFallback(srcPath, dstPath)
	if err == nil || !strings.Contains(err.Error(), "删除源路径") || !strings.Contains(err.Error(), "cleanup boom") {
		t.Fatalf("expected cleanup error, got: %v", err)
	}
	if _, statErr := os.Stat(srcPath); statErr != nil {
		t.Fatalf("src path should still exist when cleanup fails, err=%v", statErr)
	}
	if _, statErr := os.Stat(dstPath); statErr != nil {
		t.Fatalf("dst path should exist after fallback copy, err=%v", statErr)
	}
}

func TestRollbackBootstrappedBaseDirFallsBackOnEXDEV(t *testing.T) {
	restore := stubRuntimePathOps(t)
	defer restore()

	tempDir := t.TempDir()
	baseDir := filepath.Join(tempDir, "server")
	legacy := filepath.Join(tempDir, "server.image")
	cause := errors.New("symlink switch failed")
	if err := os.WriteFile(baseDir, []byte("broken-link-placeholder"), 0o644); err != nil {
		t.Fatalf("create base placeholder: %v", err)
	}
	if err := os.Mkdir(legacy, 0o755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "server"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	renamePathFn = func(src, dst string) error {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: syscall.EXDEV}
	}

	err := rollbackBootstrappedBaseDir(baseDir, legacy, cause)
	if !errors.Is(err, cause) {
		t.Fatalf("expected original cause, got: %v", err)
	}
	if st, statErr := os.Lstat(baseDir); statErr != nil {
		t.Fatalf("stat restored baseDir: %v", statErr)
	} else if !st.IsDir() {
		t.Fatalf("restored baseDir should be directory")
	}
	if _, statErr := os.Stat(filepath.Join(baseDir, "server")); statErr != nil {
		t.Fatalf("restored runtime content missing: %v", statErr)
	}
	if _, statErr := os.Stat(legacy); !os.IsNotExist(statErr) {
		t.Fatalf("legacy path should be consumed, err=%v", statErr)
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

func stubRuntimePathOps(t *testing.T) func() {
	t.Helper()

	oldRename := renamePathFn
	oldRemove := removePathFn
	oldRemoveAll := removeAllPathFn
	oldCopy := copyPathForCrossDeviceFn

	return func() {
		renamePathFn = oldRename
		removePathFn = oldRemove
		removeAllPathFn = oldRemoveAll
		copyPathForCrossDeviceFn = oldCopy
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
