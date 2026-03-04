package main

import (
	"archive/tar"
	"archive/zip"
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
	maxDownloadTimeout         = 2 * time.Hour
	minDownloadSpeedBytes      = int64(512 * 1024) // 512KiB/s
)

type artifactProbeResult struct {
	URL     string
	Latency time.Duration
	Err     error
}

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

func downloadWithSHA256(ctx context.Context, urlStr, dstPath, expectedSHA256 string, timeout, idleTimeout time.Duration) (string, error) {
	expected := strings.ToLower(strings.TrimSpace(expectedSHA256))
	if expected != "" {
		if _, err := os.Stat(dstPath); err == nil {
			got, err := sha256File(dstPath)
			if err == nil && strings.EqualFold(got, expected) {
				return got, nil
			}
			_ = os.Remove(dstPath)
		}
	}

	ensureDir(filepath.Dir(dstPath))
	tmpPath := dstPath + ".tmp"
	_ = os.Remove(tmpPath)

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
	defer f.Close()

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
				_ = os.Remove(tmpPath)
				return "", copyErr
			}
			goto verify
		case <-ticker.C:
			last := time.Unix(0, lastProgress.Load())
			if time.Since(last) > idleTimeout {
				cancel()
				_ = os.Remove(tmpPath)
				return "", fmt.Errorf("下载进度停滞超过 %s", idleTimeout)
			}
		case <-reqCtx.Done():
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("下载失败: %w", reqCtx.Err())
		}
	}

verify:
	got := hex.EncodeToString(h.Sum(nil))
	if expected != "" && !strings.EqualFold(got, expected) {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("SHA256 校验失败: want=%s got=%s", expected, got)
	}

	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
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
	manifest, err := getRemoteManifest()
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

func supervisorctl(args ...string) error {
	bin, err := exec.LookPath("supervisorctl")
	if err != nil {
		return err
	}

	cfg := getEnv("SUPERVISOR_CONF", "/app/supervisord.conf")
	cmdArgs := []string{"-c", cfg}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command(bin, cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func stopServerProcess() {
	name := getEnv("SUPERVISOR_SERVER_NAME", "server")
	if err := supervisorctl("stop", name); err == nil {
		return
	}

	// Fallback: best-effort kill by command path.
	baseDir := getEnv("PTNEXUS_BASE_DIR", "/app/server")
	_ = exec.Command("pkill", "-TERM", "-f", filepath.Join(baseDir, "server")).Run()
	_ = exec.Command("pkill", "-TERM", "-f", "pt-nexus-go").Run()
	time.Sleep(2 * time.Second)
}

func startServerProcess() error {
	name := getEnv("SUPERVISOR_SERVER_NAME", "server")
	if err := supervisorctl("start", name); err == nil {
		return nil
	}
	if err := supervisorctl("restart", name); err == nil {
		return nil
	}
	return fmt.Errorf("无法启动服务 %s（supervisorctl 不可用或执行失败）", name)
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
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, linkPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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
			if err := updateSymlinkAtomic(currentLink, target); err != nil {
				return "", "", "", err
			}
		} else {
			// Move image runtime aside and make baseDir a stable symlink.
			legacy := fmt.Sprintf("%s.image.%s", baseDir, time.Now().Format("20060102-150405"))
			if err := os.Rename(baseDir, legacy); err != nil {
				return "", "", "", fmt.Errorf("迁移 baseDir 失败: %w", err)
			}
			if err := updateSymlinkAtomic(baseDir, currentLink); err != nil {
				// Best-effort rollback.
				_ = os.Rename(legacy, baseDir)
				return "", "", "", err
			}
			if err := updateSymlinkAtomic(currentLink, legacy); err != nil {
				return "", "", "", err
			}
		}
	}

	// Ensure baseDir is a symlink to currentLink when currentLink exists.
	if st, err := os.Lstat(baseDir); err == nil {
		if st.Mode()&os.ModeSymlink == 0 {
			// Container recreated: baseDir is image directory while currentLink exists on volume.
			legacy := fmt.Sprintf("%s.image.%s", baseDir, time.Now().Format("20060102-150405"))
			if err := os.Rename(baseDir, legacy); err != nil {
				return "", "", "", fmt.Errorf("迁移 baseDir 失败: %w", err)
			}
			if err := updateSymlinkAtomic(baseDir, currentLink); err != nil {
				_ = os.Rename(legacy, baseDir)
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

func waitForServerHealthy(ctx context.Context) error {
	deadline := time.Now().Add(30 * time.Second)
	urlStr := fmt.Sprintf("http://127.0.0.1:%s/health", serverPort)
	client := &http.Client{Timeout: 3 * time.Second}

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("健康检查超时: %s", urlStr)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.ReadAll(io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
		}
		time.Sleep(1 * time.Second)
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

	// Prevent concurrent install.
	lockPath := filepath.Join(updateDir, "update.lock")
	return withFileLock(lockPath, func() error {
		stopServerProcess()

		_, currentLink, prevTarget, err := ensureRuntimeSymlinks()
		if err != nil {
			return err
		}

		versionToken := sanitizePathToken(version)
		releaseServerDir := filepath.Join(updateDir, "releases", versionToken, "server")
		_ = os.RemoveAll(releaseServerDir)
		ensureDir(filepath.Dir(releaseServerDir))

		if err := os.Rename(stagingServerDir, releaseServerDir); err != nil {
			return fmt.Errorf("移动新版本目录失败: %w", err)
		}

		// Ensure executable bit.
		_ = os.Chmod(filepath.Join(releaseServerDir, "server"), 0o755)

		if err := updateSymlinkAtomic(currentLink, releaseServerDir); err != nil {
			return err
		}

		if err := startServerProcess(); err != nil {
			_ = updateSymlinkAtomic(currentLink, prevTarget)
			_ = startServerProcess()
			return err
		}

		if err := waitForServerHealthy(ctx); err != nil {
			log.Printf("健康检查失败，回滚到旧版本: %s", prevTarget)
			stopServerProcess()
			_ = updateSymlinkAtomic(currentLink, prevTarget)
			_ = startServerProcess()
			return err
		}

		// Refresh local version file.
		remoteCfg, err := getRemoteConfig()
		if err == nil {
			if data, mErr := json.MarshalIndent(remoteCfg, "", "  "); mErr == nil {
				_ = os.WriteFile(localConfigFile, data, 0o644)
			} else {
				writeLocalVersionFallback(prepared.Version)
			}
		} else {
			writeLocalVersionFallback(prepared.Version)
		}

		// Cleanup staging.
		_ = os.RemoveAll(prepared.StagingDir)
		return nil
	})
}

func writeLocalVersionFallback(version string) {
	version = strings.TrimSpace(version)
	if version == "" {
		return
	}

	now := time.Now().Format("2006.01.02")

	// Best effort: update existing CHANGELOG.json structure.
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
