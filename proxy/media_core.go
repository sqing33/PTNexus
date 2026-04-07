package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func normalizePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}

func isISOFileInput(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	return strings.EqualFold(filepath.Ext(trimmed), ".iso")
}

func withMountedISOIfNeeded(inputPath string, scene string, fn func(resolvedPath string) error) (retErr error) {
	session, err := OpenMediaSession(inputPath, scene)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := session.Close(); closeErr != nil {
			if retErr != nil {
				retErr = fmt.Errorf("%v; %v", retErr, closeErr)
			} else {
				retErr = closeErr
			}
		}
	}()
	return fn(session.ResolvedPath)
}

func executeCommand(name string, args ...string) (string, error) {
	resolvedName, err := resolveToolCommandPath(name)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(resolvedName, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("command %q failed: %v, stderr: %s", resolvedName, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func executeCommandWithTimeout(timeout time.Duration, name string, args ...string) (string, error) {
	resolvedName, err := resolveToolCommandPath(name)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(resolvedName, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start %q: %v", resolvedName, err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("command %q failed: %v, stderr: %s", resolvedName, err, strings.TrimSpace(stderr.String()))
		}
		return stdout.String(), nil
	case <-time.After(timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", fmt.Errorf("command %q timed out after %.0f seconds", resolvedName, timeout.Seconds())
	}
}

func executeCommandWithTimeoutAndStderr(timeout time.Duration, name string, args ...string) (string, string, error) {
	resolvedName, err := resolveToolCommandPath(name)
	if err != nil {
		return "", "", err
	}
	cmd := exec.Command(resolvedName, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("failed to start %q: %v", resolvedName, err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			return "", stderr.String(), fmt.Errorf("command %q failed: %v, stderr: %s", resolvedName, err, strings.TrimSpace(stderr.String()))
		}
		return stdout.String(), stderr.String(), nil
	case <-time.After(timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", "", fmt.Errorf("command %q timed out after %.0f seconds", resolvedName, timeout.Seconds())
	}
}

func getVideoDuration(videoPath string) (float64, error) {
	output, err := executeCommand("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", videoPath)
	if err != nil {
		return 0, err
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(output), 64)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("failed to parse video duration from %q", strings.TrimSpace(output))
	}
	return duration, nil
}

func takeScreenshot(videoPath, outputPath string, timePoint float64, subtitleSID int) error {
	args := []string{
		"--no-audio",
		fmt.Sprintf("--start=%.2f", timePoint),
		"--frames=1",
		"--screenshot-high-bit-depth=yes",
		"--screenshot-png-compression=0",
		"--screenshot-tag-colorspace=yes",
	}
	if subtitleSID > 0 {
		args = append(args, fmt.Sprintf("--sid=%d", subtitleSID), "--sub-visibility=yes")
	} else {
		args = append(args, "--sid=no")
	}
	args = append(args, fmt.Sprintf("--o=%s", outputPath), videoPath)

	_, err := executeCommandWithTimeout(600*time.Second, "mpv", args...)
	if err != nil {
		return fmt.Errorf("mpv screenshot failed: %v", err)
	}
	if stat, statErr := os.Stat(outputPath); statErr != nil || stat.Size() == 0 {
		return fmt.Errorf("screenshot output was not generated")
	}
	return nil
}

func detectHDRFromVideo(videoPath string) bool {
	output, err := executeCommand("ffprobe", "-v", "error", "-show_streams", videoPath)
	if err != nil {
		return false
	}
	text := strings.ToLower(output)
	return strings.Contains(text, "smpte2084") || strings.Contains(text, "bt2020")
}

func takePreviewScreenshot(videoPath, outputPath string, timePoint float64, isHDR bool) error {
	vfFilter := "scale='min(640,iw)':-2,format=yuv420p"
	if isHDR {
		vfFilter = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,scale='min(640,iw)':-2,format=yuv420p"
	}
	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", timePoint),
		"-i", videoPath,
		"-frames:v", "1",
		"-an",
		"-sn",
		"-vf", vfFilter,
		"-q:v", "14",
		outputPath,
	}
	_, err := executeCommandWithTimeout(180*time.Second, "ffmpeg", args...)
	if err != nil {
		return fmt.Errorf("ffmpeg preview capture failed: %v", err)
	}
	if stat, statErr := os.Stat(outputPath); statErr != nil || stat.Size() == 0 {
		return fmt.Errorf("preview screenshot file was not generated")
	}
	return nil
}

func takePreviewScreenshotWithSubtitle(videoPath, outputPath string, timePoint float64, subtitleSID int) error {
	tmpDir, err := os.MkdirTemp("", "ptnexus-proxy-preview-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	rawPNG := filepath.Join(tmpDir, "preview_raw.png")
	if err := takeScreenshot(videoPath, rawPNG, timePoint, subtitleSID); err != nil {
		return err
	}

	isHDR := false
	if output, probeErr := executeCommand("ffprobe", "-v", "error", "-show_streams", rawPNG); probeErr == nil {
		text := strings.ToLower(output)
		isHDR = strings.Contains(text, "smpte2084") || strings.Contains(text, "bt2020")
	}

	vfFilter := "scale='min(640,iw)':-2,format=yuv420p"
	if isHDR {
		vfFilter = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,scale='min(640,iw)':-2,format=yuv420p"
	}
	args := []string{
		"-y",
		"-v", "error",
		"-i", rawPNG,
		"-frames:v", "1",
		"-vf", vfFilter,
		"-q:v", "14",
		outputPath,
	}
	_, stderrStr, err := executeCommandWithTimeoutAndStderr(180*time.Second, "ffmpeg", args...)
	if err != nil {
		return fmt.Errorf("ffmpeg preview capture failed: %v, stderr: %s", err, stderrStr)
	}
	if stat, statErr := os.Stat(outputPath); statErr != nil || stat.Size() == 0 {
		return fmt.Errorf("preview screenshot file was not generated")
	}
	return nil
}

func convertPngToOptimizedPng(sourcePath, destPath string) error {
	const maxUploadSize = 10 * 1024 * 1024

	ffprobePath, err := resolveToolCommandPath("ffprobe")
	if err != nil {
		return err
	}
	checkCmd := exec.Command(ffprobePath, "-v", "error", "-show_streams", sourcePath)
	output, err := checkCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffprobe inspection failed: %v", err)
	}
	isHDR := strings.Contains(string(output), "smpte2084") || strings.Contains(string(output), "bt2020")

	vfFilter := "format=rgb24"
	if isHDR {
		vfFilter = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=pc,format=rgb24"
	}

	args := []string{
		"-y", "-v", "error", "-i", sourcePath, "-frames:v", "1",
		"-vf", vfFilter,
		"-compression_level", "4",
		"-pred", "mixed",
		destPath,
	}
	_, stderrStr, err := executeCommandWithTimeoutAndStderr(600*time.Second, "ffmpeg", args...)
	if err != nil {
		return fmt.Errorf("ffmpeg PNG optimization failed: %v, stderr: %s", err, stderrStr)
	}

	destInfo, err := os.Stat(destPath)
	if err != nil {
		return fmt.Errorf("failed to stat optimized PNG: %v", err)
	}

	if destInfo.Size() > maxUploadSize {
		tempRecompressPath := destPath + ".recompressed.png"
		recompressArgs := []string{
			"-y", "-v", "error", "-i", destPath,
			"-compression_level", "100",
			tempRecompressPath,
		}
		_, recompressStderrStr, err := executeCommandWithTimeoutAndStderr(600*time.Second, "ffmpeg", recompressArgs...)
		if err != nil {
			return fmt.Errorf("ffmpeg second-pass compression failed: %v, stderr: %s", err, recompressStderrStr)
		}
		if err := os.Rename(tempRecompressPath, destPath); err != nil {
			return fmt.Errorf("failed to replace optimized PNG: %v", err)
		}
	}

	return nil
}

type subtitleStreamProbe struct {
	Streams []struct {
		Index       int               `json:"index"`
		CodecName   string            `json:"codec_name"`
		Tags        map[string]string `json:"tags"`
		Disposition map[string]any    `json:"disposition"`
	} `json:"streams"`
}

func normalizeSubtitleLanguage(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func subtitleCodecPriority(codec string) int {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass":
		return 0
	case "subrip":
		return 1
	case "hdmv_pgs_subtitle":
		return 2
	default:
		return 9
	}
}

func subtitleChineseScore(language string, title string) int {
	score := 0
	lang := strings.ToLower(strings.TrimSpace(language))
	titleText := strings.ToLower(strings.TrimSpace(title))

	switch {
	case lang == "chi", lang == "zho", lang == "zh", lang == "cmn":
		score += 10
	case strings.HasPrefix(lang, "zh-"), strings.HasPrefix(lang, "zh_"):
		score += 10
	}

	switch {
	case strings.Contains(titleText, "简体"),
		strings.Contains(titleText, "简中"),
		strings.Contains(titleText, "chs"),
		strings.Contains(titleText, "sc"),
		strings.Contains(titleText, "simplified"):
		score += 5
	case strings.Contains(titleText, "繁体"),
		strings.Contains(titleText, "繁中"),
		strings.Contains(titleText, "cht"),
		strings.Contains(titleText, "tc"),
		strings.Contains(titleText, "traditional"):
		score += 3
	case strings.Contains(titleText, "中文"),
		strings.Contains(titleText, "中字"),
		strings.Contains(titleText, "chinese"):
		score += 2
	}

	if strings.Contains(titleText, "双语") || strings.Contains(titleText, "bilingual") {
		score++
	}
	return score
}

func isSupportedSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass", "subrip", "hdmv_pgs_subtitle":
		return true
	default:
		return false
	}
}

func buildSubtitleDisplayName(candidate subtitleStreamCandidate) string {
	parts := make([]string, 0, 6)
	parts = append(parts, fmt.Sprintf("Subtitle %d", candidate.SubtitleSID))
	if candidate.IsDefault {
		parts = append(parts, "default")
	}
	if candidate.IsConfidentChinese {
		parts = append(parts, "zh")
	}
	if candidate.CodecName != "" {
		parts = append(parts, strings.ToUpper(candidate.CodecName))
	}
	if candidate.Language != "" {
		parts = append(parts, candidate.Language)
	}
	if candidate.Title != "" {
		parts = append(parts, candidate.Title)
	}
	return strings.Join(parts, " / ")
}

func inspectSubtitleStreams(videoPath string) (subtitleInspectionResult, error) {
	ffprobePath, err := resolveToolCommandPath("ffprobe")
	if err != nil {
		return subtitleInspectionResult{}, err
	}

	cmd := exec.Command(ffprobePath, "-v", "quiet", "-print_format", "json", "-show_entries", "stream=index,codec_name,disposition:stream_tags=language,title", "-select_streams", "s", videoPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = err.Error()
		}
		return subtitleInspectionResult{}, fmt.Errorf("failed to inspect subtitle streams: %s", text)
	}

	var probe subtitleStreamProbe
	if err := json.Unmarshal(out, &probe); err != nil {
		return subtitleInspectionResult{}, fmt.Errorf("failed to parse subtitle stream JSON: %w", err)
	}

	candidates := make([]subtitleStreamCandidate, 0, len(probe.Streams))
	for i, stream := range probe.Streams {
		language := normalizeSubtitleLanguage(stream.Tags["language"])
		title := strings.TrimSpace(stream.Tags["title"])
		score := subtitleChineseScore(language, title)
		candidate := subtitleStreamCandidate{
			SubtitleSID:        i + 1,
			StreamIndex:        stream.Index,
			StreamOrdinal:      i,
			CodecName:          strings.ToLower(strings.TrimSpace(stream.CodecName)),
			Language:           language,
			Title:              title,
			ConfidenceScore:    score,
			IsConfidentChinese: score > 0,
			IsDefault:          toBoolAny(stream.Disposition["default"]),
			IsSupported:        isSupportedSubtitleCodec(stream.CodecName),
		}
		candidate.DisplayName = buildSubtitleDisplayName(candidate)
		candidates = append(candidates, candidate)
	}

	streams := make([]ScreenshotSubtitleStream, 0, len(candidates))
	for _, candidate := range candidates {
		streams = append(streams, ScreenshotSubtitleStream{
			SubtitleSID:        candidate.SubtitleSID,
			StreamIndex:        candidate.StreamIndex,
			CodecName:          candidate.CodecName,
			Language:           candidate.Language,
			Title:              candidate.Title,
			DisplayName:        candidate.DisplayName,
			IsConfidentChinese: candidate.IsConfidentChinese,
			IsDefault:          candidate.IsDefault,
		})
	}

	result := subtitleInspectionResult{
		State:      ScreenshotSubtitleStateNoUsableSubtitle,
		Streams:    streams,
		Candidates: candidates,
	}

	if best, ok := selectBestChineseSubtitle(candidates); ok {
		result.State = ScreenshotSubtitleStateConfirmedChinese
		result.CurrentSubtitleSID = best.SubtitleSID
		return result, nil
	}
	if best, ok := selectDefaultSubtitle(candidates); ok {
		result.State = ScreenshotSubtitleStateUsableButUnconfirmed
		result.CurrentSubtitleSID = best.SubtitleSID
	}
	return result, nil
}

func selectBestChineseSubtitle(candidates []subtitleStreamCandidate) (subtitleStreamCandidate, bool) {
	ranked := make([]subtitleStreamCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.ConfidenceScore > 0 {
			ranked = append(ranked, candidate)
		}
	}
	if len(ranked) == 0 {
		return subtitleStreamCandidate{}, false
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].ConfidenceScore != ranked[j].ConfidenceScore {
			return ranked[i].ConfidenceScore > ranked[j].ConfidenceScore
		}
		if ranked[i].IsDefault != ranked[j].IsDefault {
			return ranked[i].IsDefault
		}
		if subtitleCodecPriority(ranked[i].CodecName) != subtitleCodecPriority(ranked[j].CodecName) {
			return subtitleCodecPriority(ranked[i].CodecName) < subtitleCodecPriority(ranked[j].CodecName)
		}
		return ranked[i].SubtitleSID < ranked[j].SubtitleSID
	})
	return ranked[0], true
}

func selectDefaultSubtitle(candidates []subtitleStreamCandidate) (subtitleStreamCandidate, bool) {
	if len(candidates) == 0 {
		return subtitleStreamCandidate{}, false
	}

	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.IsDefault && !best.IsDefault {
			best = candidate
			continue
		}
		if candidate.IsDefault == best.IsDefault && subtitleCodecPriority(candidate.CodecName) < subtitleCodecPriority(best.CodecName) {
			best = candidate
			continue
		}
		if candidate.IsDefault == best.IsDefault && subtitleCodecPriority(candidate.CodecName) == subtitleCodecPriority(best.CodecName) && candidate.SubtitleSID < best.SubtitleSID {
			best = candidate
		}
	}
	return best, true
}

func resolveSubtitleCandidate(videoPath string, requestedSID *int) (subtitleInspectionResult, subtitleStreamCandidate, bool, int, error) {
	inspection, err := inspectSubtitleStreams(videoPath)
	if err != nil {
		return subtitleInspectionResult{}, subtitleStreamCandidate{}, false, 0, err
	}

	if requestedSID != nil {
		sid := *requestedSID
		inspection.CurrentSubtitleSID = sid
		if sid <= 0 {
			return inspection, subtitleStreamCandidate{}, false, 0, nil
		}
		for _, candidate := range inspection.Candidates {
			if candidate.SubtitleSID == sid {
				return inspection, candidate, true, sid, nil
			}
		}
		return inspection, subtitleStreamCandidate{}, false, 0, fmt.Errorf("selected subtitle stream does not exist: %d", sid)
	}

	if inspection.CurrentSubtitleSID <= 0 {
		return inspection, subtitleStreamCandidate{}, false, 0, nil
	}
	for _, candidate := range inspection.Candidates {
		if candidate.SubtitleSID == inspection.CurrentSubtitleSID {
			return inspection, candidate, true, inspection.CurrentSubtitleSID, nil
		}
	}
	return inspection, subtitleStreamCandidate{}, false, 0, nil
}

func buildUniformPreviewPoints(duration float64, count int) []float64 {
	if duration <= 0 || count <= 0 {
		return nil
	}
	start := duration * 0.12
	end := duration * 0.88
	if end <= start {
		start = math.Max(1, duration*0.10)
		end = math.Max(start+1, duration*0.90)
	}

	step := (end - start) / float64(count)
	if step <= 0 {
		step = duration / float64(count+1)
	}
	points := make([]float64, 0, count)
	for i := 0; i < count; i++ {
		point := start + step*(float64(i)+0.5)
		if point < 1 {
			point = 1
		}
		if point > duration-1 {
			point = math.Max(1, duration-1)
		}
		points = append(points, point)
	}
	return points
}

func buildSmartScreenshotPointsForPreview(videoPath string, duration float64, count int, currentSubtitleSID int, candidate subtitleStreamCandidate, hasCandidate bool) []float64 {
	_ = videoPath
	_ = currentSubtitleSID
	_ = candidate
	_ = hasCandidate
	return buildUniformPreviewPoints(duration, count)
}

func sanitizeSelectedScreenshotTimes(values []float64, duration float64) []float64 {
	clean := make([]float64, 0, len(values))
	for _, value := range values {
		if value <= 0 || value >= duration {
			continue
		}
		duplicate := false
		for _, existing := range clean {
			if math.Abs(existing-value) < 0.8 {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		clean = append(clean, value)
	}
	sort.Float64s(clean)
	return clean
}

func formatSecondClockValue(value float64) string {
	totalSeconds := int(math.Round(value))
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func markRecommendedPreviewCandidates(candidates []ScreenshotPreviewCandidate, want int) {
	if len(candidates) == 0 || want <= 0 {
		return
	}
	if want >= len(candidates) {
		for i := range candidates {
			candidates[i].Recommended = true
		}
		return
	}

	indices := make([]int, 0, want)
	if want == 1 {
		indices = append(indices, len(candidates)/2)
	} else {
		step := float64(len(candidates)-1) / float64(want-1)
		seen := map[int]struct{}{}
		for i := 0; i < want; i++ {
			idx := int(math.Round(float64(i) * step))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(candidates) {
				idx = len(candidates) - 1
			}
			if _, ok := seen[idx]; ok {
				continue
			}
			seen[idx] = struct{}{}
			indices = append(indices, idx)
		}
	}

	for _, idx := range indices {
		if idx >= 0 && idx < len(candidates) {
			candidates[idx].Recommended = true
		}
	}
}

func generatePreviewCandidates(videoPath string, duration float64, count int, currentSubtitleSID int, selectedCandidate subtitleStreamCandidate, hasSelectedCandidate bool) ([]ScreenshotPreviewCandidate, error) {
	const previewMinCount = 5
	if count <= 0 {
		count = 12
	}
	if count < previewMinCount {
		count = previewMinCount
	}

	points := buildSmartScreenshotPointsForPreview(videoPath, duration, count, currentSubtitleSID, selectedCandidate, hasSelectedCandidate)
	if len(points) == 0 {
		points = buildUniformPreviewPoints(duration, count)
	}
	if len(points) == 0 {
		return nil, fmt.Errorf("failed to generate preview timestamps")
	}

	tempDir, err := os.MkdirTemp("", "ptnexus-preview-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	isHDR := detectHDRFromVideo(videoPath)
	candidates := make([]ScreenshotPreviewCandidate, 0, len(points))
	for _, point := range points {
		outputPath := filepath.Join(tempDir, fmt.Sprintf("preview-%.0f.jpg", point*1000))
		if currentSubtitleSID > 0 {
			err = takePreviewScreenshotWithSubtitle(videoPath, outputPath, point, currentSubtitleSID)
		} else {
			err = takePreviewScreenshot(videoPath, outputPath, point, isHDR)
		}
		if err != nil {
			log.Printf("preview capture skipped at %.2fs: %v", point, err)
			continue
		}

		content, err := os.ReadFile(outputPath)
		if err != nil {
			continue
		}
		candidates = append(candidates, ScreenshotPreviewCandidate{
			ID:          fmt.Sprintf("candidate-%02d", len(candidates)+1),
			TimeSeconds: point,
			TimeLabel:   formatSecondClockValue(point),
			PreviewData: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(content),
		})
	}

	if len(candidates) < previewMinCount {
		return nil, fmt.Errorf("not enough preview candidates generated: %d", len(candidates))
	}

	markRecommendedPreviewCandidates(candidates, 5)
	return candidates, nil
}

var seasonEpisodePattern = regexp.MustCompile(`(?i)S\d{1,2}E\d{1,3}`)
var seasonOnlyPattern = regexp.MustCompile(`(?i)S\d{1,2}`)
var multiEpisodePattern = regexp.MustCompile(`(?i)S\d{1,2}E\d{1,3}\s*(?:[-~]\s*(?:S?\d{1,2})?E?\d{1,3}|E\d{1,3})`)

func extractSeasonEpisode(text string) string {
	if text == "" {
		return ""
	}
	if match := seasonEpisodePattern.FindString(text); match != "" {
		return strings.ToUpper(match)
	}
	if match := seasonOnlyPattern.FindString(text); match != "" {
		return strings.ToUpper(match)
	}
	return ""
}

func parseSeasonEpisodeNumbers(seasonEpisode string) (int, int, bool) {
	re := regexp.MustCompile(`(?i)^S(\d{1,2})(?:E(\d{1,3}))?$`)
	match := re.FindStringSubmatch(strings.TrimSpace(seasonEpisode))
	if match == nil {
		return 0, 0, false
	}
	season, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, 0, false
	}
	if match[2] == "" {
		return season, 0, false
	}
	episode, err := strconv.Atoi(match[2])
	if err != nil {
		return 0, 0, false
	}
	return season, episode, true
}

func findTargetVideoFile(path string, contentName string) (string, error) {
	videoExtensions := map[string]bool{
		".mkv": true, ".mp4": true, ".ts": true, ".avi": true,
		".wmv": true, ".mov": true, ".flv": true, ".m2ts": true,
	}

	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("path does not exist: %s", path)
	}
	if err != nil {
		return "", fmt.Errorf("failed to stat path: %v", err)
	}

	if !info.IsDir() {
		if videoExtensions[strings.ToLower(filepath.Ext(path))] {
			return path, nil
		}
		return "", fmt.Errorf("path is not a supported video file: %s", path)
	}

	type videoFileInfo struct {
		path string
		size int64
	}
	videoFiles := make([]videoFileInfo, 0)
	err = filepath.Walk(path, func(filePath string, fileInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			log.Printf("warning: failed to visit %s: %v", filePath, walkErr)
			return nil
		}
		if fileInfo.IsDir() {
			return nil
		}
		if videoExtensions[strings.ToLower(filepath.Ext(filePath))] {
			videoFiles = append(videoFiles, videoFileInfo{path: filePath, size: fileInfo.Size()})
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to walk directory: %v", err)
	}
	if len(videoFiles) == 0 {
		return "", fmt.Errorf("no video files found in: %s", path)
	}
	if len(videoFiles) == 1 {
		return videoFiles[0].path, nil
	}

	seasonEpisode := ""
	if strings.TrimSpace(contentName) != "" {
		seasonEpisode = extractSeasonEpisode(contentName)
	}
	if seasonEpisode == "" {
		seasonEpisode = extractSeasonEpisode(filepath.Base(path))
	}

	if seasonEpisode != "" {
		targetSeason, targetEpisode, hasEpisode := parseSeasonEpisodeNumbers(seasonEpisode)
		if targetSeason > 0 {
			if !hasEpisode {
				targetEpisode = 1
			}

			type episodeCandidate struct {
				episode int
				isMulti bool
				path    string
			}
			episodeMatches := make([]episodeCandidate, 0)
			seasonCandidates := make([]episodeCandidate, 0)

			for _, file := range videoFiles {
				baseName := filepath.Base(file.path)
				candidate := extractSeasonEpisode(baseName)
				if candidate == "" {
					continue
				}
				candSeason, candEpisode, candHasEpisode := parseSeasonEpisodeNumbers(candidate)
				if candSeason != targetSeason || !candHasEpisode {
					continue
				}
				item := episodeCandidate{
					episode: candEpisode,
					isMulti: multiEpisodePattern.MatchString(baseName),
					path:    file.path,
				}
				seasonCandidates = append(seasonCandidates, item)
				if candEpisode == targetEpisode {
					episodeMatches = append(episodeMatches, item)
				}
			}

			if len(episodeMatches) > 0 {
				sort.SliceStable(episodeMatches, func(i, j int) bool {
					if episodeMatches[i].isMulti != episodeMatches[j].isMulti {
						return !episodeMatches[i].isMulti
					}
					return episodeMatches[i].path < episodeMatches[j].path
				})
				return episodeMatches[0].path, nil
			}

			if len(seasonCandidates) > 0 {
				sort.SliceStable(seasonCandidates, func(i, j int) bool {
					if seasonCandidates[i].episode != seasonCandidates[j].episode {
						return seasonCandidates[i].episode < seasonCandidates[j].episode
					}
					if seasonCandidates[i].isMulti != seasonCandidates[j].isMulti {
						return !seasonCandidates[i].isMulti
					}
					return seasonCandidates[i].path < seasonCandidates[j].path
				})
				return seasonCandidates[0].path, nil
			}
		}
	}

	sort.SliceStable(videoFiles, func(i, j int) bool {
		if videoFiles[i].size != videoFiles[j].size {
			return videoFiles[i].size > videoFiles[j].size
		}
		return videoFiles[i].path < videoFiles[j].path
	})
	if videoFiles[0].size < 100*1024*1024 {
		log.Printf("warning: selected largest video file is smaller than 100MB and may not be the main feature")
	}
	return videoFiles[0].path, nil
}

func uploadToPixhost(imagePath string) (string, error) {
	apiURLs := []string{
		"https://api.pixhost.to/images",
		"http://pt-nexus-proxy.sqing33.dpdns.org/https://api.pixhost.to/images",
		"http://pt-nexus-proxy.1395251710.workers.dev/https://api.pixhost.to/images",
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		for _, apiURL := range apiURLs {
			showURL, statusCode, err := uploadToPixhostDirectStream(imagePath, apiURL)
			if err == nil && strings.TrimSpace(showURL) != "" {
				return strings.TrimSpace(showURL), nil
			}
			if err != nil {
				lastErr = err
			} else if statusCode > 0 {
				lastErr = fmt.Errorf("pixhost HTTP %d", statusCode)
			}
		}
		if attempt < 3 {
			time.Sleep(2 * time.Second)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("pixhost upload failed")
	}
	return "", lastErr
}

func uploadToPixhostDirectStream(imagePath string, apiURL string) (string, int, error) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	var writeErr error
	go func() {
		defer func() {
			if writeErr == nil {
				_ = pw.Close()
			}
		}()
		defer writer.Close()

		file, err := os.Open(imagePath)
		if err != nil {
			writeErr = err
			_ = pw.CloseWithError(err)
			return
		}
		defer file.Close()

		part, err := writer.CreateFormFile("img", filepath.Base(imagePath))
		if err != nil {
			writeErr = err
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, file); err != nil {
			writeErr = err
			_ = pw.CloseWithError(err)
			return
		}
		if err := writer.WriteField("content_type", "0"); err != nil {
			writeErr = err
			_ = pw.CloseWithError(err)
			return
		}
	}()

	req, err := http.NewRequest(http.MethodPost, apiURL, pr)
	if err != nil {
		_ = pr.Close()
		return "", 0, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		_ = pr.Close()
		if writeErr != nil {
			return "", 0, writeErr
		}
		return "", 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode, fmt.Errorf("pixhost HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	parsed := map[string]any{}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", resp.StatusCode, fmt.Errorf("failed to parse pixhost response: %w", err)
	}
	showURL := strings.TrimSpace(toStringAny(parsed["show_url"], ""))
	if showURL == "" {
		if dataMap, ok := parsed["data"].(map[string]any); ok {
			showURL = strings.TrimSpace(toStringAny(dataMap["show_url"], ""))
		}
	}
	if showURL == "" {
		return "", resp.StatusCode, fmt.Errorf("pixhost response did not include show_url")
	}
	return showURL, resp.StatusCode, nil
}
