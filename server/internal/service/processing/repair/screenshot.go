package repair

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
)

const screenshotTotalCount = 5

// GenerateAndUploadScreenshots 从目标媒体自动截帧并上传到 Pixhost，返回可用图片链接列表。
// 参数/返回：输入包含 payload/source_info/content_name/config，返回去重后的截图 URL。
// 失败场景：路径定位失败、mpv/ffmpeg/ffprobe 不可用、上传失败时返回错误。
// 副作用：读取本地媒体、执行外部命令、向 Pixhost 发起网络请求；会输出叙事式纯文本日志。
func GenerateAndUploadScreenshots(input ScreenshotGenerateInput) ([]string, error) {
	payload := input.Payload
	sourceInfo := input.SourceInfo
	selectedSubtitleSID, selectedSubtitleProvided := parseSelectedSubtitleSIDAny(payload["selected_subtitle_sid"])

	logx.PlainInfof("开始执行截图和上传任务 (智能 HDR/SDR + 自动中文字幕)...")
	uploadCtx := PrepareScreenshotUploadContext(input.RootConfig)
	logx.PlainInfof("已选择图床服务: %s, 截图数量: %d", uploadCtx.Hoster, screenshotTotalCount)

	savePath := strings.TrimSpace(toStringAny(payload["savePath"], toStringAny(payload["save_path"], "")))
	if savePath == "" {
		savePath = strings.TrimSpace(toStringAny(sourceInfo["save_path"], ""))
	}
	downloaderID := strings.TrimSpace(toStringAny(payload["downloaderId"], toStringAny(payload["downloader_id"], "")))
	torrentName := strings.TrimSpace(toStringAny(payload["torrentName"], toStringAny(payload["torrent_name"], "")))
	if torrentName == "" {
		torrentName = strings.TrimSpace(toStringAny(payload["name"], ""))
	}
	if torrentName == "" {
		torrentName = strings.TrimSpace(toStringAny(sourceInfo["main_title"], ""))
	}
	contentName := strings.TrimSpace(input.ContentName)
	preferExactRemotePath := false
	savePath, torrentName, preferExactRemotePath = enrichScreenshotSourceFromDownloader(input.RootConfig, payload, downloaderID, savePath, torrentName, contentName)

	// 对齐 MediaInfo：当 downloader.use_proxy=true 且本机不挂载媒体目录时，优先通过盒子代理远程截图。
	downloader, decision, dErr := downloaderclient.DecideProxy(input.RootConfig, downloaderID)
	logx.Infof(screenshotValidateLogModule, "截图代理判定 downloader_id=%s enabled=%t reason=%s proxy_host=%s proxy_port=%d err=%v", downloaderID, decision.Enabled, decision.Reason, downloader.Host, downloader.ProxyPort, dErr)
	if decision.Enabled {
		remoteCandidates := buildRemotePathCandidatesForProxy(savePath, torrentName, contentName, preferExactRemotePath)
		var lastErr error
		for candidateIndex, remoteCandidate := range remoteCandidates {
			logx.Infof(screenshotValidateLogModule, "截图代理尝试 scene=自动截图 candidate=%d/%d remote_path=%s", candidateIndex+1, len(remoteCandidates), remoteCandidate)
			bbcode, err := downloader.FetchScreenshotsByProxy(
				remoteCandidate,
				contentName,
				buildSelectedSubtitleSIDPointer(selectedSubtitleSID, selectedSubtitleProvided),
			)
			if err == nil && strings.TrimSpace(bbcode) != "" {
				urls := ExtractImageURLsFromText(bbcode)
				logx.Infof(screenshotValidateLogModule, "截图代理响应 scene=自动截图 remote_path=%s bbcode_len=%d image_urls=%d", remoteCandidate, len([]rune(strings.TrimSpace(bbcode))), len(urls))
				if len(urls) > 0 {
					logx.PlainInfof("已通过盒子代理生成截图 remote_path=%s count=%d", remoteCandidate, len(urls))
					return urls, nil
				}
				lastErr = fmt.Errorf("代理返回的截图 BBCode 未包含可用图片链接")
				break
			}

			if apiErr, ok := err.(*downloaderclient.ProxyAPIError); ok && apiErr != nil {
				lastErr = err
				// 400 通常表示路径不存在/未找到视频文件，继续尝试下一个候选；其他错误回退本地逻辑。
				if apiErr.StatusCode == 400 {
					continue
				}
				break
			}
			lastErr = err
			break
		}
		if lastErr != nil {
			logx.PlainWarnf("盒子代理截图失败，回退本地截图 err=%v", lastErr)
		} else {
			logx.PlainWarnf("盒子代理截图未命中有效路径，回退本地截图 remote_candidates=%v", remoteCandidates)
		}
	} else if dErr != nil && strings.TrimSpace(decision.Reason) == "config_error" {
		logx.PlainWarnf("盒子代理截图跳过：读取下载器配置失败 downloader_id=%s err=%v", downloaderID, dErr)
	}

	translatedSavePath := TranslateDownloaderPath(input.RootConfig, downloaderID, savePath)
	if translatedSavePath != savePath && savePath != "" && translatedSavePath != "" {
		logx.PlainInfof("路径映射: %s -> %s", savePath, translatedSavePath)
	}
	if shouldSkipLocalScreenshotFallback(input.RootConfig, downloaderID, savePath, translatedSavePath, decision) {
		return nil, fmt.Errorf("下载器已启用远程模式，代理未能生成截图，且未配置本地路径映射，已停止本地扫描")
	}

	fullVideoPath := translatedSavePath
	if strings.TrimSpace(torrentName) != "" {
		fullVideoPath = filepath.Join(translatedSavePath, torrentName)
	}
	logx.PlainInfof("处理视频路径: %s", fullVideoPath)

	logx.PlainInfof("开始在路径 '%s' 中查找目标视频文件...", fullVideoPath)
	targetResult, err := resolveLocalMediaTargetResult(input.RootConfig, downloaderID, savePath, torrentName, contentName, "截图生成")
	if err != nil {
		logx.PlainWarnf("错误：在指定路径中未找到视频文件: %v", err)
		return nil, err
	}
	defer func() {
		if closeErr := targetResult.Close(); closeErr != nil {
			logx.Warnf(screenshotValidateLogModule, "关闭本地媒体访问会话失败 scene=%s source_path=%s err=%v", "截图生成", targetResult.SourcePath, closeErr)
		}
	}()
	targetVideoFile := targetResult.TargetFile
	logx.PlainInfof("找到目标媒体文件: source=%s target=%s", targetResult.SourcePath, targetVideoFile)

	mpvPath, err := resolveBinary("mpv", "PTNEXUS_MPV_PATH")
	if err != nil {
		logx.PlainWarnf("错误：找不到 mpv。请安装 mpv 或设置 PTNEXUS_MPV_PATH。")
		return nil, err
	}
	ffmpegPath, err := resolveBinary("ffmpeg", "PTNEXUS_FFMPEG_PATH")
	if err != nil {
		logx.PlainWarnf("错误：找不到 ffmpeg。请安装 ffmpeg 或设置 PTNEXUS_FFMPEG_PATH。")
		return nil, err
	}
	ffprobePath, err := resolveBinary("ffprobe", "PTNEXUS_FFPROBE_PATH")
	if err != nil {
		logx.PlainWarnf("错误：找不到 ffprobe。请安装 ffprobe 或设置 PTNEXUS_FFPROBE_PATH。")
		return nil, err
	}

	// 获取截图时间点：先智能分析，失败则回退百分比。
	points := getSmartScreenshotPoints(ffprobePath, targetVideoFile, screenshotTotalCount)
	if len(points) < screenshotTotalCount {
		logx.PlainWarnf("警告: 智能分析失败，回退到按百分比截图。")
		duration, err := probeDurationSeconds(targetVideoFile)
		if err != nil || duration <= 0 {
			logx.PlainWarnf("错误: 获取视频时长失败: %v", err)
			return nil, fmt.Errorf("读取视频时长失败: %w", err)
		}
		percents := []float64{0.15, 0.30, 0.50, 0.70, 0.85}
		points = make([]float64, 0, len(percents))
		for _, p := range percents {
			points = append(points, duration*p)
		}
	}
	sort.Float64s(points)

	// 自动检测中文字幕轨道（mpv sid）。
	logx.PlainInfof("正在分析字幕流...")
	inspection, selectedCandidate, hasSelectedCandidate, err := resolveLocalSubtitleCandidate(ffprobePath, targetVideoFile, selectedSubtitleSID)
	if err != nil {
		return nil, err
	}
	subtitleSID := inspection.CurrentSubtitleSID
	if selectedSubtitleProvided {
		subtitleSID = selectedSubtitleSID
		if selectedSubtitleSID > 0 && !hasSelectedCandidate {
			subtitleSID = inspection.CurrentSubtitleSID
		}
	}
	switch {
	case subtitleSID <= 0:
		logx.PlainInfof("   当前选择为无字幕，将截取无字幕画面。")
	case selectedSubtitleProvided && hasSelectedCandidate:
		logx.PlainInfof("   已按用户选择的字幕流截图 sid=%d title=%s", subtitleSID, strings.TrimSpace(selectedCandidate.Title))
	case inspection.State == ScreenshotSubtitleStateConfirmedChinese:
		logx.PlainInfof("   已检测到明确中文字幕，将自动挂载字幕截图 sid=%d", subtitleSID)
	default:
		logx.PlainInfof("   将使用当前预览字幕流截图 sid=%d", subtitleSID)
	}

	tmpDir, err := os.MkdirTemp("", "ptnexus-screens-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	type uploadJob struct {
		Index    int
		TimeStr  string
		FilePath string
	}
	type uploadResult struct {
		Index    int
		URL      string
		OK       bool
		LogBlock string
	}

	const uploadWorkers = 5
	jobs := make(chan uploadJob, len(points))
	results := make(chan uploadResult, len(points))
	var wg sync.WaitGroup
	wg.Add(uploadWorkers)
	for w := 0; w < uploadWorkers; w++ {
		go func() {
			defer wg.Done()
			for job := range jobs {
				var buf bytes.Buffer
				logLine := func(format string, args ...any) {
					buf.WriteString(fmt.Sprintf(format, args...))
					buf.WriteByte('\n')
				}
				showURL, err := uploadCtx.UploadScreenshot(job.FilePath, logLine)
				if err != nil || strings.TrimSpace(showURL) == "" {
					results <- uploadResult{Index: job.Index, OK: false, LogBlock: buf.String()}
					continue
				}
				logLine("   🚀 上传成功: %s", showURL)

				finalURL := uploadCtx.NormalizeScreenshotURL(showURL)
				results <- uploadResult{Index: job.Index, OK: true, URL: finalURL, LogBlock: buf.String()}
			}
		}()
	}

	uploadLogs := make([]string, len(points))
	uploadedURLs := make([]string, len(points))
	uploadedOK := make([]bool, len(points))

	for i, point := range points {
		timeStr := formatSecondHMS(point)
		fileStem := fmt.Sprintf("s%d_%s", i+1, timeStr)
		rawPNG := filepath.Join(tmpDir, "raw_"+fileStem+".png")

		logx.PlainInfof("")
		logx.PlainInfof("--- 处理第 %d/%d 张截图 (%s) ---", i+1, len(points), timeStr)

		if err := captureRawPNGWithMPV(mpvPath, targetVideoFile, point, rawPNG, subtitleSID); err != nil {
			logx.PlainInfof("❌ mpv 截图失败: %s", sanitizeCommandErrForLog(err))
			continue
		}
		if stat, statErr := os.Stat(rawPNG); statErr != nil || stat.Size() == 0 {
			logx.PlainInfof("❌ mpv 未生成文件: %s", rawPNG)
			continue
		}

		keywordHDR := hasScreenshotHDRKeyword(contentName) || hasScreenshotHDRKeyword(torrentName)
		metadataHDR := false
		if hdr, hdrErr := detectHDRFromPNG(ffprobePath, rawPNG); hdrErr == nil {
			metadataHDR = hdr
		} else {
			logx.PlainInfof("   ⚠️ 检测 HDR 信息失败，假定为 SDR: %v", hdrErr)
		}
		isHDR := keywordHDR || metadataHDR
		finalExt := ".png"
		if isHDR {
			finalExt = ".jpg"
		}
		finalImagePath := filepath.Join(tmpDir, fileStem+finalExt)
		logx.Infof(screenshotValidateLogModule, "截图 HDR 判定 scene=自动截图 index=%d keyword_hdr=%t metadata_hdr=%t hdr=%t output=%s", i+1, keywordHDR, metadataHDR, isHDR, finalImagePath)

		vfFilter := "format=rgb24"
		if isHDR {
			logx.PlainInfof("   🎨 检测到 HDR 原始内容，应用 zscale 色调映射...")
			vfFilter = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=pc,format=rgb24"
		} else {
			logx.PlainInfof("   🎨 检测到 SDR 内容，应用标准 RGB 转换...")
		}

		startCompress := time.Now()
		if err := compressPNGWithFFmpeg(ffmpegPath, rawPNG, finalImagePath, vfFilter); err != nil {
			logx.PlainInfof("❌ ffmpeg 压缩失败: %s", sanitizeCommandErrForLog(err))
			continue
		}
		compressTime := time.Since(startCompress).Seconds()

		srcSize := fileSizeBytes(rawPNG)
		dstSize := fileSizeBytes(finalImagePath)
		ratio := 0.0
		if srcSize > 0 {
			ratio = float64(dstSize) / float64(srcSize) * 100
		}
		logx.PlainInfof("   ✅ 压缩完成: %.2f MB (压缩率 %.1f%%) | 耗时 %.2fs | HDR: %v", float64(dstSize)/1024.0/1024.0, ratio, compressTime, isHDR)

		// 上传不并发截图，但上传并发。
		jobs <- uploadJob{Index: i, TimeStr: timeStr, FilePath: finalImagePath}
	}

	close(jobs)
	wg.Wait()
	close(results)

	for res := range results {
		if res.Index < 0 || res.Index >= len(points) {
			continue
		}
		uploadLogs[res.Index] = res.LogBlock
		if res.OK {
			uploadedOK[res.Index] = true
			uploadedURLs[res.Index] = res.URL
		}
	}

	logx.PlainInfof("")
	logx.PlainInfof("开始并发上传图片... 并发数: %d, 总数: %d", uploadWorkers, len(points))
	successCount := 0
	for i := 0; i < len(points); i++ {
		logx.PlainInfof("")
		logx.PlainInfof("--- 上传第 %d/%d 张截图 (%s) ---", i+1, len(points), formatSecondHMS(points[i]))
		if block := strings.TrimSpace(uploadLogs[i]); block != "" {
			for _, line := range strings.Split(block, "\n") {
				line = strings.TrimRight(line, "\r")
				if strings.TrimSpace(line) == "" {
					continue
				}
				logx.PlainInfof("%s", line)
			}
		}
		if uploadedOK[i] {
			successCount++
		} else {
			logx.PlainInfof("   ❌ 第 %d 张图片上传失败", i+1)
		}
	}

	finalList := make([]string, 0, successCount)
	for i := 0; i < len(points); i++ {
		if uploadedOK[i] && strings.TrimSpace(uploadedURLs[i]) != "" {
			finalList = append(finalList, uploadedURLs[i])
		}
	}
	if len(finalList) == 0 {
		return nil, fmt.Errorf("未生成可用截图")
	}
	return finalList, nil
}

func buildRemotePathCandidatesForProxy(savePath, torrentName, contentName string, preferExactPath bool) []string {
	trimmedSavePath := strings.TrimSpace(savePath)
	trimmedTorrentName := strings.TrimSpace(torrentName)
	trimmedContentName := strings.TrimSpace(contentName)

	candidates := make([]string, 0, 3)
	appendCandidate := func(candidate string) {
		normalized := normalizeProxyRemotePath(candidate)
		if normalized == "" {
			return
		}
		for _, existing := range candidates {
			if normalizeScreenshotPathForCompare(existing) == normalizeScreenshotPathForCompare(normalized) {
				return
			}
		}
		candidates = append(candidates, normalized)
	}
	if preferExactPath && trimmedSavePath != "" {
		appendCandidate(trimmedSavePath)
	}
	if trimmedSavePath != "" && trimmedTorrentName != "" {
		appendCandidate(joinProxyRemotePath(trimmedSavePath, trimmedTorrentName))
	}
	if trimmedSavePath != "" && trimmedContentName != "" && !strings.EqualFold(trimmedContentName, trimmedTorrentName) {
		appendCandidate(joinProxyRemotePath(trimmedSavePath, trimmedContentName))
	}
	if trimmedSavePath != "" {
		appendCandidate(trimmedSavePath)
	}
	return candidates
}

func enrichScreenshotSourceFromDownloader(rootConfig map[string]any, payload map[string]any, downloaderID, savePath, torrentName, contentName string) (string, string, bool) {
	if strings.TrimSpace(downloaderID) == "" {
		return savePath, torrentName, false
	}
	seedHash := firstNonEmptyScreenshotString(
		toStringAny(payload["downloader_hash"], ""),
		toStringAny(payload["downloaderHash"], ""),
	)
	if seedHash == "" && strings.TrimSpace(torrentName) == "" && strings.TrimSpace(contentName) == "" && strings.TrimSpace(savePath) == "" {
		return savePath, torrentName, false
	}
	downloader, err := downloaderclient.FromConfig(rootConfig, downloaderID)
	if err != nil {
		logx.Warnf(screenshotValidateLogModule, "截图路径回填跳过：读取下载器失败 downloader_id=%s err=%v", downloaderID, err)
		return savePath, torrentName, false
	}
	snapshots, err := downloader.FetchTorrents()
	if err != nil {
		logx.Warnf(screenshotValidateLogModule, "截图路径回填跳过：拉取下载器任务失败 downloader_id=%s err=%v", downloaderID, err)
		return savePath, torrentName, false
	}
	for _, snapshot := range snapshots {
		bestPath := strings.TrimSpace(snapshot.ContentPath)
		preferExactPath := bestPath != ""
		if bestPath == "" {
			bestPath = strings.TrimSpace(snapshot.SavePath)
		}
		if bestPath == "" {
			continue
		}
		matched := false
		if seedHash != "" && strings.EqualFold(seedHash, strings.TrimSpace(snapshot.Hash)) {
			matched = true
		}
		if !matched && strings.TrimSpace(snapshot.Name) != "" {
			for _, candidateName := range []string{torrentName, contentName} {
				if strings.TrimSpace(candidateName) != "" && strings.TrimSpace(snapshot.Name) == strings.TrimSpace(candidateName) {
					matched = true
					break
				}
			}
		}
		if !matched && strings.TrimSpace(savePath) != "" && normalizeScreenshotPathForCompare(snapshot.ContentPath) == normalizeScreenshotPathForCompare(savePath) {
			matched = true
		}
		if !matched {
			continue
		}
		if strings.TrimSpace(snapshot.Name) != "" {
			torrentName = strings.TrimSpace(snapshot.Name)
		}
		logx.Infof(
			screenshotValidateLogModule,
			"截图路径回填完成 downloader_id=%s hash=%s torrent_name=%s save_path=%s content_path=%s used_path=%s prefer_exact=%t",
			downloaderID,
			snapshot.Hash,
			snapshot.Name,
			snapshot.SavePath,
			snapshot.ContentPath,
			bestPath,
			preferExactPath,
		)
		return bestPath, torrentName, preferExactPath
	}
	logx.Warnf(screenshotValidateLogModule, "截图路径回填未命中 downloader_id=%s seed_hash=%s torrent_name=%s", downloaderID, seedHash, torrentName)
	return savePath, torrentName, false
}

func firstNonEmptyScreenshotString(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}

func joinProxyRemotePath(base, name string) string {
	normalizedBase := normalizeProxyRemotePath(base)
	normalizedName := strings.Trim(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"), "/")
	if normalizedBase == "" {
		return normalizedName
	}
	if normalizedName == "" {
		return normalizedBase
	}
	return pathpkg.Join(normalizedBase, normalizedName)
}

func normalizeProxyRemotePath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return strings.ReplaceAll(trimmed, "\\", "/")
}

func fileSizeBytes(path string) int64 {
	stat, err := os.Stat(path)
	if err != nil || stat == nil {
		return 0
	}
	return stat.Size()
}

func detectHDRFromPNG(ffprobePath string, pngPath string) (bool, error) {
	cmd := exec.Command(ffprobePath, "-v", "error", "-show_streams", pngPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = err.Error()
		}
		return false, fmt.Errorf("ffprobe 执行失败: %s", text)
	}
	text := strings.ToLower(string(out))
	return strings.Contains(text, "smpte2084") || strings.Contains(text, "bt2020"), nil
}

// hasScreenshotHDRKeyword 根据下载任务名称补充 HDR 判断，避免截图文件缺少色彩元数据时被误判为 SDR。
func hasScreenshotHDRKeyword(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(lower, "hdr") ||
		strings.Contains(lower, "dovi") ||
		strings.Contains(lower, "dolby vision") ||
		strings.Contains(lower, "dv ")
}

func captureRawPNGWithMPV(mpvPath string, videoPath string, second float64, outputPath string, subtitleSID int) error {
	cmd := []string{
		mpvPath,
		"--no-audio",
		fmt.Sprintf("--start=%.2f", second),
		"--frames=1",
		"--screenshot-high-bit-depth=yes",
		"--screenshot-png-compression=0",
		"--screenshot-tag-colorspace=yes",
		fmt.Sprintf("--o=%s", outputPath),
	}
	if subtitleSID > 0 {
		cmd = append(cmd, fmt.Sprintf("--sid=%d", subtitleSID), "--sub-visibility=yes")
	} else {
		cmd = append(cmd, "--sid=no")
	}
	cmd = append(cmd, videoPath)

	proc := exec.Command(cmd[0], cmd[1:]...)
	out, err := proc.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("mpv 执行失败: %s", text)
	}
	return nil
}

func compressPNGWithFFmpeg(ffmpegPath string, srcPNG string, dstPNG string, vfFilter string) error {
	isJPEG := strings.EqualFold(filepath.Ext(dstPNG), ".jpg") || strings.EqualFold(filepath.Ext(dstPNG), ".jpeg")
	runFilter := func(filter string) ([]byte, error) {
		args := []string{
			"-y", "-v", "error", "-i", srcPNG, "-frames:v", "1", "-vf", filter,
		}
		if isJPEG {
			args = append(args, "-q:v", "2")
		} else {
			args = append(args, "-compression_level", "4", "-pred", "mixed")
		}
		args = append(args, dstPNG)
		return exec.Command(ffmpegPath, args...).CombinedOutput()
	}

	out, err := runFilter(vfFilter)
	if err != nil && isJPEG {
		explicitHDRFilter := "zscale=pin=bt2020:tin=smpte2084:rin=pc:t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=pc,format=yuv420p"
		logx.Infof(screenshotValidateLogModule, "HDR JPEG zscale 转换失败，尝试显式 BT.2020/PQ 输入 source=%s err=%v", srcPNG, err)
		if retryOut, retryErr := runFilter(explicitHDRFilter); retryErr == nil {
			out = retryOut
			err = nil
			logx.Infof(screenshotValidateLogModule, "HDR JPEG 显式色彩转换成功 output=%s", dstPNG)
		} else {
			out = retryOut
			err = retryErr
		}
	}
	if err != nil && isJPEG {
		fallbackFilter := "scale='min(3840,iw)':-2:flags=lanczos,unsharp=5:5:0.30:3:3:0.15,format=yuv420p"
		logx.Infof(screenshotValidateLogModule, "HDR JPEG 色调映射不可用，尝试直接 JPEG 转换 source=%s err=%v", srcPNG, err)
		if retryOut, retryErr := runFilter(fallbackFilter); retryErr == nil {
			out = retryOut
			err = nil
			logx.Infof(screenshotValidateLogModule, "HDR JPEG 直接转换成功 output=%s", dstPNG)
		} else {
			out = retryOut
			err = retryErr
		}
	}
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("ffmpeg 执行失败: %s", text)
	}
	if stat, statErr := os.Stat(dstPNG); statErr != nil || stat.Size() == 0 {
		return fmt.Errorf("输出文件未生成")
	}
	return nil
}

func resolveBinary(binName, envKey string) (string, error) {
	if envKey != "" {
		if configured := strings.TrimSpace(os.Getenv(envKey)); configured != "" {
			if _, err := os.Stat(configured); err == nil {
				return configured, nil
			}
			return "", fmt.Errorf("%s 指向的可执行文件不存在: %s", envKey, configured)
		}
	}
	if found, err := exec.LookPath(binName); err == nil {
		return found, nil
	}
	if envKey != "" {
		return "", fmt.Errorf("未找到 %s，可安装后重试，或设置 %s", binName, envKey)
	}
	return "", fmt.Errorf("未找到 %s，可安装后重试", binName)
}

func probeDurationSeconds(targetFile string) (float64, error) {
	ffprobePath, err := resolveBinary("ffprobe", "PTNEXUS_FFPROBE_PATH")
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(
		ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		targetFile,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text == "" {
			text = err.Error()
		}
		return 0, fmt.Errorf("ffprobe 执行失败: %s", text)
	}
	value := strings.TrimSpace(string(output))
	duration, parseErr := strconv.ParseFloat(value, 64)
	if parseErr != nil || duration <= 0 {
		return 0, fmt.Errorf("无法解析视频时长: %s", value)
	}
	return duration, nil
}

func formatSecondHMS(second float64) string {
	totalSeconds := int(second)
	minutes, sec := divmod(totalSeconds, 60)
	hour, min := divmod(minutes, 60)
	return fmt.Sprintf("%02dh%02dm%02ds", hour, min, sec)
}

func divmod(a, b int) (int, int) {
	if b == 0 {
		return 0, a
	}
	return a / b, a % b
}

func resolveLocalMediaTargetResult(rootConfig map[string]any, downloaderID, savePath, torrentName, contentName, scene string) (*processingmedia.ResolvedMediaTarget, error) {
	translatedSavePath := TranslateDownloaderPath(rootConfig, downloaderID, savePath)
	return processingmedia.ResolveMediaTargetByCandidates(translatedSavePath, torrentName, contentName, scene)
}

func sanitizeCommandErrForLog(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if text == "" {
		return ""
	}
	switch {
	case strings.Contains(text, "ffprobe 执行失败"):
		return "ffprobe 执行失败"
	case strings.Contains(text, "ffmpeg 执行失败"):
		return "ffmpeg 执行失败"
	case strings.Contains(text, "mpv 执行失败"):
		return "mpv 执行失败"
	default:
		return text
	}
}

// TranslateDownloaderPath 按下载器路径映射把远端保存路径转换为本地路径。
func TranslateDownloaderPath(rootConfig map[string]any, downloaderID, remotePath string) string {
	return downloaderclient.TranslateDownloaderPath(rootConfig, downloaderID, remotePath)
}

func shouldSkipLocalScreenshotFallback(rootConfig map[string]any, downloaderID, savePath, translatedSavePath string, decision downloaderclient.ProxyDecision) bool {
	if !decision.Enabled {
		return false
	}
	if strings.TrimSpace(translatedSavePath) == "" {
		translatedSavePath = TranslateDownloaderPath(rootConfig, downloaderID, savePath)
	}
	return normalizeScreenshotPathForCompare(savePath) == normalizeScreenshotPathForCompare(translatedSavePath)
}

func normalizeScreenshotPathForCompare(value string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	for strings.Contains(normalized, "//") {
		normalized = strings.ReplaceAll(normalized, "//", "/")
	}
	return strings.TrimRight(normalized, "/")
}
