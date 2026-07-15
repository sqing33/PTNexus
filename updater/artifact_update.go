package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type PreparedUpdate struct {
	Version    string         `json:"version"`
	Artifact   UpdateArtifact `json:"artifact"`
	StagingDir string         `json:"staging_dir"`
	PreparedAt time.Time      `json:"prepared_at"`
}

const (
	defaultDownloadIdleTimeout = 30 * time.Second
	defaultServerHealthTimeout = 2 * time.Minute
	maxDownloadTimeout         = 2 * time.Hour
	minDownloadSpeedBytes      = int64(512 * 1024) // 512KiB/s
)

type artifactProbeResult struct {
	URL     string
	Latency time.Duration
	Err     error
}

var (
	supervisorctlRunner      = runSupervisorctl
	createSymlinkFn          = os.Symlink
	replaceSymlinkFn         = os.Rename
	removeSymlinkFn          = os.Remove
	renameFileFn             = os.Rename
	removeFileFn             = os.Remove
	renamePathFn             = os.Rename
	removePathFn             = os.Remove
	removeAllPathFn          = os.RemoveAll
	copyPathForCrossDeviceFn = copyPathForCrossDevice
	updateSymlinkFn          = updateSymlinkAtomic
	ensureRuntimeFn          = ensureRuntimeSymlinks
	stopServerFn             = stopServerProcess
	startServerFn            = startServerProcess
	rollbackRuntimeFn        = rollbackRuntime
	fileOpRetrySleepFn       = time.Sleep
	fileOpRetryEnabled       = runtime.GOOS == "windows"
)

const (
	windowsFileOpRetryMaxAttempts = 6
	windowsFileOpRetryBaseDelay   = 150 * time.Millisecond
)

func sanitizePathToken(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(v))
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '-' || r == '_' || r == 'v' || r == 'V':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "._-")
}

func inferBundleFormat(artifact UpdateArtifact, fileName string) string {
	if strings.TrimSpace(artifact.Format) != "" {
		return strings.ToLower(strings.TrimSpace(artifact.Format))
	}
	lower := strings.ToLower(fileName)
	if strings.HasSuffix(lower, ".zip") {
		return "zip"
	}
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return "tar.gz"
	}
	return "tar.gz"
}

func selectArtifact(artifacts []UpdateArtifact, goos, goarch string) (UpdateArtifact, error) {
	osWant := strings.ToLower(strings.TrimSpace(getEnv("UPDATE_OS", goos)))
	archWant := strings.ToLower(strings.TrimSpace(getEnv("UPDATE_ARCH", goarch)))

	for _, a := range artifacts {
		if strings.ToLower(strings.TrimSpace(a.OS)) == osWant && strings.ToLower(strings.TrimSpace(a.Arch)) == archWant {
			return a, nil
		}
	}

	available := make([]string, 0, len(artifacts))
	for _, a := range artifacts {
		available = append(available, fmt.Sprintf("%s/%s", strings.TrimSpace(a.OS), strings.TrimSpace(a.Arch)))
	}
	return UpdateArtifact{}, fmt.Errorf("未找到匹配当前平台的更新包 (want=%s/%s, available=%s)", osWant, archWant, strings.Join(available, ", "))
}

func resolveManifestArtifactForCurrentPlatform(manifest *UpdateManifest) (UpdateArtifact, error) {
	if manifest == nil {
		return UpdateArtifact{}, errors.New("manifest is nil")
	}
	artifact, err := selectArtifact(manifest.Latest.Artifacts, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return UpdateArtifact{}, err
	}
	candidates := artifactDownloadCandidates(artifact)
	if len(candidates) == 0 {
		return UpdateArtifact{}, errors.New("artifact.url 与 mirror_urls 均为空")
	}
	if strings.TrimSpace(artifact.SHA256) == "" && !isTruthy(getEnv("UPDATE_SKIP_VERIFY", "false")) {
		return UpdateArtifact{}, errors.New("artifact.sha256 为空")
	}
	for _, raw := range candidates {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || strings.TrimSpace(raw) == "" {
			return UpdateArtifact{}, fmt.Errorf("artifact 下载地址无效: %q", raw)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return UpdateArtifact{}, fmt.Errorf("artifact.url scheme 不支持: %q", raw)
		}
	}
	return artifact, nil
}

func artifactDownloadCandidates(artifact UpdateArtifact) []string {
	items := make([]string, 0, 1+len(artifact.MirrorURLs))
	items = append(items, artifact.URL)
	items = append(items, artifact.MirrorURLs...)
	return normalizeURLCandidates(items...)
}

func probeArtifactURL(ctx context.Context, rawURL string, timeout time.Duration) (time.Duration, error) {
	client := newUpdateHTTPClient(timeout)
	start := time.Now()

	headCtx, headCancel := context.WithTimeout(ctx, timeout)
	defer headCancel()

	headReq, err := http.NewRequestWithContext(headCtx, http.MethodHead, rawURL, nil)
	if err == nil {
		resp, doErr := client.Do(headReq)
		if doErr == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				return time.Since(start), nil
			}
			if resp.StatusCode != http.StatusMethodNotAllowed {
				return 0, fmt.Errorf("HEAD HTTP %d", resp.StatusCode)
			}
		}
	}

	getCtx, getCancel := context.WithTimeout(ctx, timeout)
	defer getCancel()

	getReq, err := http.NewRequestWithContext(getCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	getReq.Header.Set("Range", "bytes=0-0")

	resp, err := client.Do(getReq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPartialContent || (resp.StatusCode >= 200 && resp.StatusCode < 400) {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1))
		return time.Since(start), nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	return 0, fmt.Errorf("GET 探测 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func rankProbeCandidates(ctx context.Context, urls []string, timeout time.Duration) ([]artifactProbeResult, error) {
	candidates := normalizeURLCandidates(urls...)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("没有可用的产物下载地址")
	}
	if timeout <= 0 {
		timeout = 6 * time.Second
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout+2*time.Second)
	defer cancel()

	results := make(chan artifactProbeResult, len(candidates))
	for _, raw := range candidates {
		raw := raw
		go func() {
			latency, err := probeArtifactURL(probeCtx, raw, timeout)
			results <- artifactProbeResult{
				URL:     raw,
				Latency: latency,
				Err:     err,
			}
		}()
	}

	successes := make([]artifactProbeResult, 0, len(candidates))
	failures := make([]artifactProbeResult, 0, len(candidates))
	for i := 0; i < len(candidates); i++ {
		result := <-results
		if result.Err == nil {
			successes = append(successes, result)
		} else {
			failures = append(failures, result)
		}
	}

	sort.Slice(successes, func(i, j int) bool {
		return successes[i].Latency < successes[j].Latency
	})

	for _, failed := range failures {
		log.Printf("产物源探测失败: %s err=%v", failed.URL, failed.Err)
	}

	if len(successes) == 0 {
		errs := make([]string, 0, len(failures))
		for _, failed := range failures {
			errs = append(errs, fmt.Sprintf("%s -> %v", failed.URL, failed.Err))
		}
		return nil, fmt.Errorf("所有产物源探测失败: %s", strings.Join(errs, "; "))
	}
	return successes, nil
}

func computeDownloadTimeout(base time.Duration, size int64) time.Duration {
	if base <= 0 {
		base = 20 * time.Minute
	}
	timeout := base
	if size > 0 {
		autoBySize := time.Duration(size/minDownloadSpeedBytes)*time.Second + 2*time.Minute
		if autoBySize > timeout {
			timeout = autoBySize
		}
	}
	if timeout > maxDownloadTimeout {
		timeout = maxDownloadTimeout
	}
	return timeout
}

func bundleFileNameFromURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || strings.TrimSpace(rawURL) == "" {
		return "pt-nexus-update.bundle"
	}
	fileName := path.Base(u.Path)
	if strings.TrimSpace(fileName) == "" || fileName == "/" || fileName == "." {
		return "pt-nexus-update.bundle"
	}
	return fileName
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func shouldRetryWindowsFileOp(err error) bool {
	if !fileOpRetryEnabled {
		return false
	}

	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		err = linkErr.Err
	}

	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}

	errno, ok := err.(syscall.Errno)
	if !ok {
		return false
	}

	switch errno {
	case 5, 32, 33: // ERROR_ACCESS_DENIED / ERROR_SHARING_VIOLATION / ERROR_LOCK_VIOLATION
		return true
	default:
		return false
	}
}

func retryWindowsFileOp(opDesc string, fn func() error) error {
	var lastErr error
	for attempt := 1; attempt <= windowsFileOpRetryMaxAttempts; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !shouldRetryWindowsFileOp(lastErr) || attempt == windowsFileOpRetryMaxAttempts {
			break
		}
		log.Printf("Windows 文件操作失败，准备重试: op=%s attempt=%d/%d err=%v", opDesc, attempt, windowsFileOpRetryMaxAttempts, lastErr)
		fileOpRetrySleepFn(time.Duration(attempt) * windowsFileOpRetryBaseDelay)
	}

	if lastErr == nil {
		return nil
	}
	if shouldRetryWindowsFileOp(lastErr) {
		return fmt.Errorf("%s 失败，已重试 %d 次: %w", opDesc, windowsFileOpRetryMaxAttempts, lastErr)
	}
	return lastErr
}

func removeFileWithRetry(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return retryWindowsFileOp(fmt.Sprintf("remove %s", path), func() error {
		err := removeFileFn(path)
		if err != nil && os.IsNotExist(err) {
			return nil
		}
		return err
	})
}

func renameFileWithRetry(srcPath, dstPath string) error {
	return retryWindowsFileOp(fmt.Sprintf("rename %s -> %s", srcPath, dstPath), func() error {
		return renameFileFn(srcPath, dstPath)
	})
}

func downloadWithSHA256(ctx context.Context, urlStr, dstPath, expectedSHA256 string, timeout, idleTimeout time.Duration) (string, error) {
	expected := strings.ToLower(strings.TrimSpace(expectedSHA256))
	if _, err := os.Stat(dstPath); err == nil {
		if expected != "" {
			got, err := sha256File(dstPath)
			if err == nil && strings.EqualFold(got, expected) {
				return got, nil
			}
		}
		if err := removeFileWithRetry(dstPath); err != nil {
			return "", fmt.Errorf("清理旧下载文件失败: %w", err)
		}
	}

	ensureDir(filepath.Dir(dstPath))
	tmpPath := dstPath + ".tmp"
	if err := removeFileWithRetry(tmpPath); err != nil {
		return "", fmt.Errorf("清理临时下载文件失败: %w", err)
	}

	if idleTimeout <= 0 {
		idleTimeout = defaultDownloadIdleTimeout
	}

	reqCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	client := newUpdateHTTPClient(timeout)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("下载失败: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	fileClosed := false
	closeTempFile := func() error {
		if fileClosed {
			return nil
		}
		fileClosed = true
		return f.Close()
	}
	defer func() {
		_ = closeTempFile()
	}()

	var h hash.Hash = sha256.New()
	var lastProgress atomic.Int64
	lastProgress.Store(time.Now().UnixNano())

	progressWriter := io.MultiWriter(
		f,
		h,
		progressTracker(func() {
			lastProgress.Store(time.Now().UnixNano())
		}),
	)

	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(progressWriter, resp.Body)
		copyDone <- copyErr
	}()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case copyErr := <-copyDone:
			if copyErr != nil {
				if closeErr := closeTempFile(); closeErr != nil {
					log.Printf("关闭临时下载文件失败: path=%s err=%v", tmpPath, closeErr)
				}
				_ = removeFileWithRetry(tmpPath)
				return "", copyErr
			}
			goto verify
		case <-ticker.C:
			last := time.Unix(0, lastProgress.Load())
			if time.Since(last) > idleTimeout {
				cancel()
				_ = removeFileWithRetry(tmpPath)
				return "", fmt.Errorf("下载进度停滞超过 %s", idleTimeout)
			}
		case <-reqCtx.Done():
			_ = removeFileWithRetry(tmpPath)
			return "", fmt.Errorf("下载失败: %w", reqCtx.Err())
		}
	}

verify:
	if err := closeTempFile(); err != nil {
		_ = removeFileWithRetry(tmpPath)
		return "", fmt.Errorf("关闭临时下载文件失败: %w", err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	if expected != "" && !strings.EqualFold(got, expected) {
		_ = removeFileWithRetry(tmpPath)
		return "", fmt.Errorf("SHA256 校验失败: want=%s got=%s", expected, got)
	}

	if err := renameFileWithRetry(tmpPath, dstPath); err != nil {
		_ = removeFileWithRetry(tmpPath)
		return "", err
	}

	return got, nil
}

type progressTracker func()

func (t progressTracker) Write(p []byte) (int, error) {
	if len(p) > 0 {
		t()
	}
	return len(p), nil
}

func safeJoin(destDir, name string) (string, error) {
	cleaned := path.Clean(strings.TrimSpace(name))
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "." || cleaned == "" {
		return "", errors.New("empty path")
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("illegal path traversal: %q", name)
	}
	out := filepath.Join(destDir, filepath.FromSlash(cleaned))
	// Ensure final path is within destDir.
	destClean := filepath.Clean(destDir)
	outClean := filepath.Clean(out)
	if outClean != destClean && !strings.HasPrefix(outClean, destClean+string(os.PathSeparator)) {
		return "", fmt.Errorf("illegal extract path: %q", name)
	}
	return outClean, nil
}

func extractTarGz(bundlePath, destDir string) error {
	f, err := os.Open(bundlePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if strings.TrimSpace(hdr.Name) == "" {
			continue
		}

		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			return fmt.Errorf("bundle 不允许包含链接文件: %s", hdr.Name)
		}

		targetPath, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("不支持的 tar 类型: %v (%s)", hdr.Typeflag, hdr.Name)
		}
	}
	return nil
}

func extractZip(bundlePath, destDir string) error {
	r, err := zip.OpenReader(bundlePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if strings.TrimSpace(f.Name) == "" {
			continue
		}
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("bundle 不允许包含 symlink: %s", f.Name)
		}

		targetPath, err := safeJoin(destDir, f.Name)
		if err != nil {
			return err
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode()&0o777)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		if err := out.Close(); err != nil {
			rc.Close()
			return err
		}
		if err := rc.Close(); err != nil {
			return err
		}
	}

	return nil
}

func prepareUpdateBundleFromManifest(ctx context.Context, manifest *UpdateManifest) (*PreparedUpdate, error) {
	if manifest == nil {
		return nil, errors.New("manifest is nil")
	}

	remoteVersion := strings.TrimSpace(manifest.Latest.Version)
	localVersion := getLocalVersion()
	if !isNewerVersion(remoteVersion, localVersion) {
		return &PreparedUpdate{Version: remoteVersion, PreparedAt: time.Now()}, nil
	}
	if manifest.Latest.DisableUpdate {
		return nil, fmt.Errorf("版本 %s 标记为 disable_update，拒绝在线更新", remoteVersion)
	}

	artifact, err := selectArtifact(manifest.Latest.Artifacts, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(artifact.SHA256) == "" && !isTruthy(getEnv("UPDATE_SKIP_VERIFY", "false")) {
		return nil, fmt.Errorf("artifact.sha256 为空，拒绝下载（可设置 UPDATE_SKIP_VERIFY=true 跳过）")
	}

	candidates := artifactDownloadCandidates(artifact)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("未配置可用的 artifact 下载地址")
	}

	for _, raw := range candidates {
		u, parseErr := url.Parse(strings.TrimSpace(raw))
		if parseErr != nil || strings.TrimSpace(raw) == "" {
			return nil, fmt.Errorf("artifact.url 无效: %q", raw)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("artifact.url scheme 不支持: %q", raw)
		}
	}

	versionToken := sanitizePathToken(remoteVersion)
	stagingDir := filepath.Join(updateDir, "staging", versionToken)
	downloadRoot := filepath.Join(updateDir, "downloads", versionToken)

	timeoutStr := getEnv("UPDATE_DOWNLOAD_TIMEOUT", "20m")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil || timeout <= 0 {
		timeout = 20 * time.Minute
	}
	timeout = computeDownloadTimeout(timeout, artifact.Size)

	probeResults, err := rankProbeCandidates(ctx, candidates, 6*time.Second)
	if err != nil {
		return nil, err
	}
	probeURLs := make([]string, 0, len(probeResults))
	for _, probe := range probeResults {
		probeURLs = append(probeURLs, probe.URL)
	}

	var (
		downloadPath string
		fileName     string
		gotSHA       string
		downloadErrs []string
		chosenURL    string
	)

	for _, candidateURL := range probeURLs {
		fileName = bundleFileNameFromURL(candidateURL)
		downloadPath = filepath.Join(downloadRoot, fileName)
		log.Printf("开始下载更新包: version=%s os=%s arch=%s url=%s", remoteVersion, artifact.OS, artifact.Arch, candidateURL)
		gotSHA, err = downloadWithSHA256(ctx, candidateURL, downloadPath, artifact.SHA256, timeout, defaultDownloadIdleTimeout)
		if err == nil {
			chosenURL = candidateURL
			break
		}
		downloadErrs = append(downloadErrs, fmt.Sprintf("%s -> %v", candidateURL, err))
		log.Printf("更新包下载失败，准备切换下一个源: url=%s err=%v", candidateURL, err)
	}
	if chosenURL == "" {
		return nil, fmt.Errorf("所有可用产物源下载失败: %s", strings.Join(downloadErrs, "; "))
	}

	// Clean and extract.
	_ = os.RemoveAll(stagingDir)
	ensureDir(stagingDir)

	format := inferBundleFormat(artifact, fileName)
	switch format {
	case "zip":
		log.Printf("解压更新包 (zip): %s -> %s", downloadPath, stagingDir)
		if err := extractZip(downloadPath, stagingDir); err != nil {
			return nil, err
		}
	default:
		log.Printf("解压更新包 (tar.gz): %s -> %s", downloadPath, stagingDir)
		if err := extractTarGz(downloadPath, stagingDir); err != nil {
			return nil, err
		}
	}

	serverDir := filepath.Join(stagingDir, "server")
	if st, err := os.Stat(serverDir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("更新包内容不完整：缺少 server/ 目录 (staging=%s)", stagingDir)
	}
	serverBin := filepath.Join(serverDir, "server")
	if st, err := os.Stat(serverBin); err != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("更新包内容不完整：缺少 server/server 可执行文件 (staging=%s)", stagingDir)
	}

	prepared := &PreparedUpdate{
		Version:    remoteVersion,
		Artifact:   artifact,
		StagingDir: stagingDir,
		PreparedAt: time.Now(),
	}
	prepared.Artifact.URL = chosenURL
	prepared.Artifact.SHA256 = gotSHA

	markerPath := filepath.Join(stagingDir, "prepared.json")
	markerData, _ := json.MarshalIndent(prepared, "", "  ")
	_ = os.WriteFile(markerPath, markerData, 0o644)

	return prepared, nil
}

func prepareLatestUpdateBundle(ctx context.Context) (*PreparedUpdate, error) {
	manifest, err := getRemoteManifestForMode(updateModeRuntimeInstall, getLocalVersion())
	if err != nil {
		return nil, err
	}
	return prepareUpdateBundleFromManifest(ctx, manifest)
}

func readPreparedUpdate(version string) (*PreparedUpdate, error) {
	versionToken := sanitizePathToken(version)
	markerPath := filepath.Join(updateDir, "staging", versionToken, "prepared.json")
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return nil, err
	}
	var prepared PreparedUpdate
	if err := json.Unmarshal(data, &prepared); err != nil {
		return nil, err
	}
	if strings.TrimSpace(prepared.Version) == "" {
		prepared.Version = version
	}
	if strings.TrimSpace(prepared.StagingDir) == "" {
		prepared.StagingDir = filepath.Dir(markerPath)
	}
	return &prepared, nil
}

func runSupervisorctl(ctx context.Context, args ...string) (string, error) {
	bin, err := exec.LookPath("supervisorctl")
	if err != nil {
		return "", err
	}

	if ctx == nil {
		ctx = context.Background()
	}
	cfg := getEnv("SUPERVISOR_CONF", "/app/supervisord.conf")
	cmdArgs := []string{"-c", cfg}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.CommandContext(ctx, bin, cmdArgs...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	outputParts := make([]string, 0, 2)
	if trimmed := strings.TrimSpace(stdout.String()); trimmed != "" {
		outputParts = append(outputParts, trimmed)
	}
	if trimmed := strings.TrimSpace(stderr.String()); trimmed != "" {
		outputParts = append(outputParts, trimmed)
	}
	return strings.Join(outputParts, "\n"), err
}

func formatLogOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "<empty>"
	}
	return strings.ReplaceAll(output, "\n", " | ")
}

func parseSupervisorProgramState(name, output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		rawName := strings.TrimSuffix(fields[0], ":")
		if rawName == name {
			return strings.ToUpper(strings.TrimSpace(fields[1])), nil
		}
	}
	return "", fmt.Errorf("无法从 supervisor 输出解析服务状态: %s", formatLogOutput(output))
}

func waitForSupervisorState(ctx context.Context, name string, timeout time.Duration) (string, string, error) {
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastState string
	var lastOutput string
	var lastErr error

	checkStatus := func() (string, string, error) {
		output, err := supervisorctlRunner(waitCtx, "status", name)
		if strings.TrimSpace(output) != "" {
			state, parseErr := parseSupervisorProgramState(name, output)
			if parseErr != nil {
				return "", output, parseErr
			}
			return state, output, err
		}
		if err != nil {
			return "", "", err
		}
		return "", "", fmt.Errorf("supervisor status 输出为空")
	}

	for {
		state, output, err := checkStatus()
		if err == nil && state == "RUNNING" {
			return state, output, nil
		}
		if output != "" {
			lastState = state
			lastOutput = output
			switch state {
			case "BACKOFF", "FATAL", "EXITED":
				if err == nil {
					err = fmt.Errorf("supervisor 进入失败状态: %s", state)
				}
				return state, output, err
			}
		}
		if err != nil {
			lastErr = err
		}

		select {
		case <-waitCtx.Done():
			if lastErr != nil {
				return lastState, lastOutput, fmt.Errorf("等待 supervisor 进入 RUNNING 超时: %w", lastErr)
			}
			return lastState, lastOutput, fmt.Errorf("等待 supervisor 进入 RUNNING 超时")
		case <-ticker.C:
		}
	}
}

func stopServerProcess() error {
	name := getEnv("SUPERVISOR_SERVER_NAME", "server")
	log.Printf("安装更新步骤: 尝试停止服务 program=%s", name)
	commandCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	output, err := supervisorctlRunner(commandCtx, "stop", name)
	if err == nil {
		log.Printf("安装更新步骤: supervisor 停止服务完成 program=%s output=%s", name, formatLogOutput(output))
		return nil
	}

	log.Printf("安装更新步骤: supervisor 停止服务失败，准备回退到 pkill program=%s output=%s err=%v", name, formatLogOutput(output), err)

	// Fallback: best-effort kill by command path.
	baseDir := getEnv("PTNEXUS_BASE_DIR", "/app/server")
	killErrs := make([]string, 0, 2)
	if pkillErr := exec.Command("pkill", "-TERM", "-f", filepath.Join(baseDir, "server")).Run(); pkillErr != nil {
		killErrs = append(killErrs, fmt.Sprintf("pkill server=%v", pkillErr))
	}
	if pkillErr := exec.Command("pkill", "-TERM", "-f", "pt-nexus-go").Run(); pkillErr != nil {
		killErrs = append(killErrs, fmt.Sprintf("pkill pt-nexus-go=%v", pkillErr))
	}
	time.Sleep(2 * time.Second)

	if len(killErrs) > 0 {
		return fmt.Errorf("停止服务失败: supervisor_err=%w, fallback=%s", err, strings.Join(killErrs, "; "))
	}
	log.Printf("安装更新步骤: 已通过 pkill 后备停止服务 program=%s", name)
	return fmt.Errorf("supervisor 停止服务失败，已使用 pkill 后备处理: output=%s err=%w", formatLogOutput(output), err)
}

func startServerProcess(ctx context.Context) error {
	name := getEnv("SUPERVISOR_SERVER_NAME", "server")
	log.Printf("安装更新步骤: 尝试启动服务 program=%s", name)

	commandCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	startOutput, startErr := supervisorctlRunner(commandCtx, "start", name)
	if startErr != nil {
		log.Printf("安装更新步骤: supervisor start 失败 program=%s output=%s err=%v", name, formatLogOutput(startOutput), startErr)
		restartOutput, restartErr := supervisorctlRunner(commandCtx, "restart", name)
		if restartErr != nil {
			return fmt.Errorf(
				"启动服务失败: start_output=%s start_err=%v restart_output=%s restart_err=%w",
				formatLogOutput(startOutput),
				startErr,
				formatLogOutput(restartOutput),
				restartErr,
			)
		}
		log.Printf("安装更新步骤: supervisor restart 已提交 program=%s output=%s", name, formatLogOutput(restartOutput))
	} else {
		log.Printf("安装更新步骤: supervisor start 已提交 program=%s output=%s", name, formatLogOutput(startOutput))
	}

	state, output, err := waitForSupervisorState(ctx, name, 25*time.Second)
	if err != nil {
		return fmt.Errorf("等待服务启动失败: state=%s output=%s err=%w", state, formatLogOutput(output), err)
	}
	log.Printf("安装更新步骤: supervisor 状态已进入 RUNNING program=%s output=%s", name, formatLogOutput(output))
	return nil
}

func rollbackRuntime(ctx context.Context, currentLink, prevTarget string) error {
	if strings.TrimSpace(prevTarget) == "" {
		return errors.New("缺少旧版本运行时目标，无法回滚")
	}

	log.Printf("安装更新步骤: 开始回滚到旧版本 target=%s", prevTarget)
	if err := stopServerFn(); err != nil {
		log.Printf("安装更新步骤: 回滚前停止服务出现异常 err=%v", err)
	}
	if err := updateSymlinkFn(currentLink, prevTarget); err != nil {
		return fmt.Errorf("切换回旧版本失败: %w", err)
	}
	if err := startServerFn(ctx); err != nil {
		return fmt.Errorf("回滚后启动旧版本失败: %w", err)
	}
	log.Printf("安装更新步骤: 回滚完成 target=%s", prevTarget)
	return nil
}

func isCrossDeviceRenameErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EXDEV) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "cross-device link")
}

func copyPathForCrossDevice(srcPath, dstPath string) error {
	info, err := os.Lstat(srcPath)
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(srcPath)
		if err != nil {
			return err
		}
		return os.Symlink(target, dstPath)
	}

	if info.IsDir() {
		if err := os.MkdirAll(dstPath, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(srcPath)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPathForCrossDevice(filepath.Join(srcPath, entry.Name()), filepath.Join(dstPath, entry.Name())); err != nil {
				return err
			}
		}
		return os.Chmod(dstPath, info.Mode().Perm())
	}

	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	copyErr := error(nil)
	if _, err := io.Copy(out, in); err != nil {
		copyErr = err
	}
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(dstPath, info.Mode().Perm())
}

func movePathWithCrossDeviceFallback(srcPath, dstPath string) error {
	if err := renamePathFn(srcPath, dstPath); err == nil {
		return nil
	} else if !isCrossDeviceRenameErr(err) {
		return fmt.Errorf("rename %s -> %s 失败: %w", srcPath, dstPath, err)
	} else {
		log.Printf("安装更新步骤: 目录迁移遇到跨设备限制，改用复制回退 src=%s dst=%s err=%v", srcPath, dstPath, err)
	}

	if err := copyPathForCrossDeviceFn(srcPath, dstPath); err != nil {
		_ = removeAllPathFn(dstPath)
		return fmt.Errorf("跨设备复制 %s -> %s 失败: %w", srcPath, dstPath, err)
	}
	if err := removeAllPathFn(srcPath); err != nil {
		return fmt.Errorf("跨设备复制后删除源路径 %s 失败: %w", srcPath, err)
	}
	log.Printf("安装更新步骤: 目录迁移已通过跨设备复制回退完成 src=%s dst=%s", srcPath, dstPath)
	return nil
}

func updateSymlinkAtomic(linkPath, target string) error {
	if strings.TrimSpace(linkPath) == "" {
		return errors.New("empty link path")
	}
	if strings.TrimSpace(target) == "" {
		return errors.New("empty symlink target")
	}
	ensureDir(filepath.Dir(linkPath))

	// Refuse to overwrite directories.
	if st, err := os.Lstat(linkPath); err == nil {
		if st.IsDir() && st.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s 是目录，拒绝覆盖", linkPath)
		}
	}

	tmp := fmt.Sprintf("%s.tmp.%d", linkPath, time.Now().UnixNano())
	_ = removeSymlinkFn(tmp)
	if err := createSymlinkFn(target, tmp); err != nil {
		return err
	}
	if err := replaceSymlinkFn(tmp, linkPath); err != nil {
		_ = removeSymlinkFn(tmp)
		if !isCrossDeviceRenameErr(err) {
			return err
		}

		log.Printf("安装更新步骤: 软链切换遇到跨设备限制，改用删除后重建 link=%s target=%s err=%v", linkPath, target, err)
		if st, statErr := os.Lstat(linkPath); statErr == nil {
			if st.IsDir() && st.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("%s 是目录，拒绝覆盖", linkPath)
			}
			if removeErr := removeSymlinkFn(linkPath); removeErr != nil {
				return fmt.Errorf("删除旧软链失败: %w", removeErr)
			}
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		if err := createSymlinkFn(target, linkPath); err != nil {
			return fmt.Errorf("跨设备 fallback 创建软链失败: %w", err)
		}
		log.Printf("安装更新步骤: 软链切换已通过跨设备 fallback 完成 link=%s target=%s", linkPath, target)
	}
	return nil
}

func rollbackBootstrappedBaseDir(baseDir, legacy string, cause error) error {
	if removeErr := removePathFn(baseDir); removeErr != nil && !os.IsNotExist(removeErr) {
		return fmt.Errorf("%w; 删除失败的 baseDir 软链失败: %v", cause, removeErr)
	}
	if renameErr := movePathWithCrossDeviceFallback(legacy, baseDir); renameErr != nil {
		return fmt.Errorf("%w; 恢复 baseDir 失败: %v", cause, renameErr)
	}
	return cause
}

func ensureRuntimeSymlinks() (baseDir, currentLink, currentTarget string, err error) {
	baseDir = getEnv("PTNEXUS_BASE_DIR", "/app/server")
	currentLink = filepath.Join(updateDir, "current")

	// Ensure currentLink exists or bootstrap from image runtime.
	if _, err := os.Lstat(currentLink); os.IsNotExist(err) {
		// Create baseDir -> currentLink if baseDir is a directory.
		st, statErr := os.Lstat(baseDir)
		if statErr != nil {
			return "", "", "", fmt.Errorf("找不到 baseDir: %s", baseDir)
		}

		// If baseDir is already a symlink, reuse its target.
		if st.Mode()&os.ModeSymlink != 0 {
			target, rerr := os.Readlink(baseDir)
			if rerr != nil {
				return "", "", "", rerr
			}
			if err := updateSymlinkFn(currentLink, target); err != nil {
				return "", "", "", err
			}
		} else {
			// Move image runtime aside and make baseDir a stable symlink.
			legacy := fmt.Sprintf("%s.image.%s", baseDir, time.Now().Format("20060102-150405"))
			if err := movePathWithCrossDeviceFallback(baseDir, legacy); err != nil {
				return "", "", "", fmt.Errorf("迁移 baseDir 失败: %w", err)
			}
			if err := updateSymlinkFn(baseDir, currentLink); err != nil {
				// Best-effort rollback.
				_ = movePathWithCrossDeviceFallback(legacy, baseDir)
				return "", "", "", err
			}
			if err := updateSymlinkFn(currentLink, legacy); err != nil {
				return "", "", "", rollbackBootstrappedBaseDir(baseDir, legacy, err)
			}
		}
	}

	// Ensure baseDir is a symlink to currentLink when currentLink exists.
	if st, err := os.Lstat(baseDir); err == nil {
		if st.Mode()&os.ModeSymlink == 0 {
			// Container recreated: baseDir is image directory while currentLink exists on volume.
			legacy := fmt.Sprintf("%s.image.%s", baseDir, time.Now().Format("20060102-150405"))
			if err := movePathWithCrossDeviceFallback(baseDir, legacy); err != nil {
				return "", "", "", fmt.Errorf("迁移 baseDir 失败: %w", err)
			}
			if err := updateSymlinkFn(baseDir, currentLink); err != nil {
				_ = movePathWithCrossDeviceFallback(legacy, baseDir)
				return "", "", "", err
			}
		}
	}

	// Read current target for rollback.
	if st, err := os.Lstat(currentLink); err == nil && st.Mode()&os.ModeSymlink != 0 {
		currentTarget, _ = os.Readlink(currentLink)
	}
	return baseDir, currentLink, currentTarget, nil
}

func serverHealthTimeout() time.Duration {
	raw := strings.TrimSpace(getEnv("UPDATE_HEALTH_TIMEOUT", ""))
	if raw == "" {
		return defaultServerHealthTimeout
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout <= 0 {
		log.Printf("UPDATE_HEALTH_TIMEOUT 无效，使用默认值 %s: value=%q", defaultServerHealthTimeout, raw)
		return defaultServerHealthTimeout
	}
	return timeout
}

func waitForServerHealthy(ctx context.Context) error {
	healthCtx, cancel := context.WithTimeout(ctx, serverHealthTimeout())
	defer cancel()

	urlStr := fmt.Sprintf("http://127.0.0.1:%s/health", serverPort)
	client := &http.Client{Timeout: 3 * time.Second}
	var lastErr string

	for {
		req, _ := http.NewRequestWithContext(healthCtx, http.MethodGet, urlStr, nil)
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.ReadAll(io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				log.Printf("安装更新步骤: 健康检查通过 url=%s status=%d", urlStr, resp.StatusCode)
				return nil
			}
			lastErr = fmt.Sprintf("status=%d", resp.StatusCode)
		} else {
			lastErr = err.Error()
		}

		retryTimer := time.NewTimer(1 * time.Second)
		select {
		case <-healthCtx.Done():
			retryTimer.Stop()
			if strings.TrimSpace(lastErr) != "" {
				return fmt.Errorf("健康检查超时: %s (last=%s)", urlStr, lastErr)
			}
			return fmt.Errorf("健康检查超时: %s", urlStr)
		case <-retryTimer.C:
		}
	}
}

func installPreparedBundle(ctx context.Context, prepared *PreparedUpdate) error {
	if prepared == nil {
		return errors.New("prepared update is nil")
	}
	version := strings.TrimSpace(prepared.Version)
	if version == "" {
		return errors.New("empty version")
	}

	localVersion := getLocalVersion()
	if !isNewerVersion(version, localVersion) {
		log.Printf("本地版本 %s 已是最新，无需安装 %s", localVersion, version)
		return nil
	}

	stagingServerDir := filepath.Join(prepared.StagingDir, "server")
	if st, err := os.Stat(stagingServerDir); err != nil || !st.IsDir() {
		return fmt.Errorf("staging 缺少 server/ 目录: %s", stagingServerDir)
	}
	log.Printf("安装更新步骤: staging 校验通过 version=%s path=%s", version, stagingServerDir)

	// Prevent concurrent install.
	lockPath := filepath.Join(updateDir, "update.lock")
	return withFileLock(lockPath, func() error {
		log.Printf("安装更新步骤: 获取更新锁成功 lock=%s version=%s", lockPath, version)
		_, currentLink, prevTarget, err := ensureRuntimeFn()
		if err != nil {
			return fmt.Errorf("初始化运行时软链失败: %w", err)
		}
		log.Printf("安装更新步骤: 运行时软链检查完成 current=%s previous=%s", currentLink, prevTarget)
		if err := stopServerFn(); err != nil {
			log.Printf("安装更新步骤: 停止服务返回异常 err=%v", err)
		}

		versionToken := sanitizePathToken(version)
		releaseServerDir := filepath.Join(updateDir, "releases", versionToken, "server")
		_ = os.RemoveAll(releaseServerDir)
		ensureDir(filepath.Dir(releaseServerDir))
		log.Printf("安装更新步骤: 准备写入新版本目录 target=%s", releaseServerDir)

		if err := os.Rename(stagingServerDir, releaseServerDir); err != nil {
			return fmt.Errorf("移动新版本目录失败: %w", err)
		}
		log.Printf("安装更新步骤: 新版本目录已就位 target=%s", releaseServerDir)

		// Ensure executable bit.
		_ = os.Chmod(filepath.Join(releaseServerDir, "server"), 0o755)

		if err := updateSymlinkFn(currentLink, releaseServerDir); err != nil {
			return fmt.Errorf("切换 current 软链失败: %w", err)
		}
		log.Printf("安装更新步骤: current 软链切换完成 current=%s target=%s", currentLink, releaseServerDir)

		if err := startServerFn(ctx); err != nil {
			rollbackErr := rollbackRuntimeFn(ctx, currentLink, prevTarget)
			if rollbackErr != nil {
				return fmt.Errorf("启动新版本失败: %w；回滚失败: %v", err, rollbackErr)
			}
			return fmt.Errorf("启动新版本失败: %w；已回滚到旧版本", err)
		}

		log.Printf("安装更新步骤: 开始等待健康检查 version=%s url=http://127.0.0.1:%s/health", version, serverPort)
		if err := waitForServerHealthy(ctx); err != nil {
			log.Printf("安装更新步骤: 健康检查失败 version=%s previous=%s err=%v", version, prevTarget, err)
			rollbackErr := rollbackRuntimeFn(ctx, currentLink, prevTarget)
			if rollbackErr != nil {
				return fmt.Errorf("健康检查失败: %w；回滚失败: %v", err, rollbackErr)
			}
			return fmt.Errorf("健康检查失败: %w；已回滚到旧版本", err)
		}

		// Refresh local version file used by updater version detection.
		writeLocalVersionFallback(prepared.Version)
		log.Printf("安装更新步骤: 本地版本状态已更新 version=%s file=%s", prepared.Version, localConfigFile)

		// Cleanup staging.
		_ = os.RemoveAll(prepared.StagingDir)
		log.Printf("安装更新步骤: 清理 staging 完成 path=%s", prepared.StagingDir)
		log.Printf("安装更新步骤: 安装完成 version=%s runtime=%s", version, releaseServerDir)
		return nil
	})
}

func writeLocalVersionFallback(version string) {
	version = strings.TrimSpace(version)
	if version == "" {
		return
	}

	now := time.Now().Format("2006.01.02")
	ensureDir(filepath.Dir(localConfigFile))

	// Best effort: update the local version metadata file structure.
	if data, err := os.ReadFile(localConfigFile); err == nil {
		var cfg UpdateConfig
		if json.Unmarshal(data, &cfg) == nil && len(cfg.History) > 0 {
			cfg.History[0].Version = version
			if strings.TrimSpace(cfg.History[0].Date) == "" {
				cfg.History[0].Date = now
			}
			if out, mErr := json.MarshalIndent(cfg, "", "  "); mErr == nil {
				_ = os.WriteFile(localConfigFile, out, 0o644)
				return
			}
		}
	}

	cfg := UpdateConfig{
		History: []VersionInfo{
			{Version: version, Date: now, Changes: []string{}},
		},
	}
	if out, err := json.MarshalIndent(cfg, "", "  "); err == nil {
		_ = os.WriteFile(localConfigFile, out, 0o644)
	}
}

func withInMemoryUpdateFlag(fn func() error) error {
	updateMutex.Lock()
	if isSystemUpdating {
		updateMutex.Unlock()
		return fmt.Errorf("系统正在更新中，请稍后重试")
	}
	isSystemUpdating = true
	updateMutex.Unlock()

	defer func() {
		updateMutex.Lock()
		isSystemUpdating = false
		updateMutex.Unlock()
	}()

	return fn()
}

func getPreparedVersionFromDisk() (string, error) {
	// Pick newest staging prepared.json.
	stagingRoot := filepath.Join(updateDir, "staging")
	entries, err := os.ReadDir(stagingRoot)
	if err != nil {
		return "", err
	}

	var newest string
	var newestTime time.Time
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		marker := filepath.Join(stagingRoot, entry.Name(), "prepared.json")
		st, err := os.Stat(marker)
		if err != nil {
			continue
		}
		if st.ModTime().After(newestTime) {
			newestTime = st.ModTime()
			newest = entry.Name()
		}
	}
	if newest == "" {
		return "", errors.New("未找到已下载的更新包，请先执行 /update/pull")
	}

	data, err := os.ReadFile(filepath.Join(stagingRoot, newest, "prepared.json"))
	if err != nil {
		return "", err
	}
	var prepared PreparedUpdate
	if err := json.Unmarshal(data, &prepared); err != nil {
		return "", err
	}
	if strings.TrimSpace(prepared.Version) == "" {
		return "", errors.New("prepared.json 缺少 version")
	}
	return prepared.Version, nil
}

func parseInt64(v string) int64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}
