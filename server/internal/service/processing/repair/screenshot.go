package repair

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
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

	logx.PlainInfof("开始执行截图和上传任务 (智能 HDR/SDR + 自动中文字幕)...")
	hoster := "pixhost"
	logx.PlainInfof("已选择图床服务: %s, 截图数量: %d", hoster, screenshotTotalCount)

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

	// 对齐 MediaInfo：当 downloader.use_proxy=true 且本机不挂载媒体目录时，优先通过盒子代理远程截图。
	downloader, decision, dErr := downloaderclient.DecideProxy(input.RootConfig, downloaderID)
	if decision.Enabled {
		remoteCandidates := buildRemotePathCandidatesForProxy(savePath, torrentName, contentName)
		var lastErr error
		for _, remoteCandidate := range remoteCandidates {
			bbcode, err := downloader.FetchScreenshotsByProxy(remoteCandidate, contentName)
			if err == nil && strings.TrimSpace(bbcode) != "" {
				urls := ExtractImageURLsFromText(bbcode)
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

	fullVideoPath := translatedSavePath
	if strings.TrimSpace(torrentName) != "" {
		fullVideoPath = filepath.Join(translatedSavePath, torrentName)
	}
	logx.PlainInfof("处理视频路径: %s", fullVideoPath)

	logx.PlainInfof("开始在路径 '%s' 中查找目标视频文件...", fullVideoPath)
	targetVideoFile, err := ResolveMediaTargetFile(translatedSavePath, torrentName, contentName)
	if err != nil {
		logx.PlainWarnf("错误：在指定路径中未找到视频文件。")
		return nil, err
	}
	logx.PlainInfof("找到唯一的视频文件: %s", targetVideoFile)

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
	subtitleSID := getBestChineseSubtitleSID(ffprobePath, targetVideoFile)
	if subtitleSID <= 0 {
		logx.PlainInfof("   未检测到明确的中文字幕，将截取无字幕画面。")
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
				showURL, err := UploadImageToPixhostNarrativeWithLogger(job.FilePath, logLine)
				if err != nil || strings.TrimSpace(showURL) == "" {
					results <- uploadResult{Index: job.Index, OK: false, LogBlock: buf.String()}
					continue
				}
				logLine("   🚀 上传成功: %s", showURL)

				finalURL := strings.TrimSpace(showURL)
				if direct := PixhostShowToDirectURL(showURL); strings.TrimSpace(direct) != "" {
					if normalized := NormalizePixhostDirectHost(direct); strings.TrimSpace(normalized) != "" {
						finalURL = normalized
					} else {
						finalURL = direct
					}
				}
				results <- uploadResult{Index: job.Index, OK: true, URL: finalURL, LogBlock: buf.String()}
			}
		}()
	}

	uploadLogs := make([]string, len(points))
	uploadedURLs := make([]string, len(points))
	uploadedOK := make([]bool, len(points))

	for i, point := range points {
		timeStr := formatSecondHMS(point)
		fileName := fmt.Sprintf("s%d_%s.png", i+1, timeStr)
		rawPNG := filepath.Join(tmpDir, "raw_"+fileName)
		finalPNG := filepath.Join(tmpDir, fileName)

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

		isHDR := false
		if hdr, hdrErr := detectHDRFromPNG(ffprobePath, rawPNG); hdrErr == nil {
			isHDR = hdr
		} else {
			logx.PlainInfof("   ⚠️ 检测 HDR 信息失败，假定为 SDR: %v", hdrErr)
		}

		vfFilter := "format=rgb24"
		if isHDR {
			logx.PlainInfof("   🎨 检测到 HDR 原始内容，应用 zscale 色调映射...")
			vfFilter = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=pc,format=rgb24"
		} else {
			logx.PlainInfof("   🎨 检测到 SDR 内容，应用标准 RGB 转换...")
		}

		startCompress := time.Now()
		if err := compressPNGWithFFmpeg(ffmpegPath, rawPNG, finalPNG, vfFilter); err != nil {
			logx.PlainInfof("❌ ffmpeg 压缩失败: %s", sanitizeCommandErrForLog(err))
			continue
		}
		compressTime := time.Since(startCompress).Seconds()

		srcSize := fileSizeBytes(rawPNG)
		dstSize := fileSizeBytes(finalPNG)
		ratio := 0.0
		if srcSize > 0 {
			ratio = float64(dstSize) / float64(srcSize) * 100
		}
		logx.PlainInfof("   ✅ 压缩完成: %.2f MB (压缩率 %.1f%%) | 耗时 %.2fs | HDR: %v", float64(dstSize)/1024.0/1024.0, ratio, compressTime, isHDR)

		// 上传不并发截图，但上传并发。
		jobs <- uploadJob{Index: i, TimeStr: timeStr, FilePath: finalPNG}
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

func buildRemotePathCandidatesForProxy(savePath, torrentName, contentName string) []string {
	trimmedSavePath := strings.TrimSpace(savePath)
	trimmedTorrentName := strings.TrimSpace(torrentName)
	trimmedContentName := strings.TrimSpace(contentName)

	candidates := make([]string, 0, 3)
	if trimmedSavePath != "" && trimmedTorrentName != "" {
		candidates = append(candidates, filepath.Join(trimmedSavePath, trimmedTorrentName))
	}
	if trimmedSavePath != "" && trimmedContentName != "" && !strings.EqualFold(trimmedContentName, trimmedTorrentName) {
		candidates = append(candidates, filepath.Join(trimmedSavePath, trimmedContentName))
	}
	if trimmedSavePath != "" {
		candidates = append(candidates, trimmedSavePath)
	}
	return candidates
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
	cmd := exec.Command(
		ffmpegPath,
		"-y",
		"-v", "error",
		"-i", srcPNG,
		"-frames:v", "1",
		"-vf", vfFilter,
		"-compression_level", "4",
		"-pred", "mixed",
		dstPNG,
	)
	out, err := cmd.CombinedOutput()
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

// ResolveMediaTargetFile 在保存路径候选中选取最适合的媒体文件路径。
func ResolveMediaTargetFile(savePath, torrentName, contentName string) (string, error) {
	candidates := make([]string, 0, 3)
	trimmedSavePath := strings.TrimSpace(savePath)
	trimmedTorrentName := strings.TrimSpace(torrentName)
	trimmedContentName := strings.TrimSpace(contentName)

	if trimmedSavePath != "" && trimmedTorrentName != "" {
		candidates = append(candidates, filepath.Join(trimmedSavePath, trimmedTorrentName))
	}
	if trimmedSavePath != "" && trimmedContentName != "" && !strings.EqualFold(trimmedContentName, trimmedTorrentName) {
		candidates = append(candidates, filepath.Join(trimmedSavePath, trimmedContentName))
	}
	if trimmedSavePath != "" {
		candidates = append(candidates, trimmedSavePath)
	}

	checked := map[string]struct{}{}
	errors := make([]string, 0)
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, exists := checked[candidate]; exists {
			continue
		}
		checked[candidate] = struct{}{}

		if _, err := os.Stat(candidate); err != nil {
			errors = append(errors, fmt.Sprintf("路径不存在: %s", candidate))
			continue
		}
		target, err := processingmedia.PickMediaTarget(candidate)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		if strings.TrimSpace(target) != "" {
			return target, nil
		}
	}

	if len(errors) == 0 {
		return "", fmt.Errorf("未找到可用于截图的视频文件")
	}
	return "", fmt.Errorf("未找到可用于截图的视频文件: %s", strings.Join(errors, "；"))
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
