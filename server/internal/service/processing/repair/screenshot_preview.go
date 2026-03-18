package repair

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
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
)

const (
	screenshotPreviewDefaultCount = 12
	screenshotPreviewMinCount     = 5
	screenshotPreviewSelectCount  = 5
)

const screenshotPreviewLogModule = "媒体校验-截图预览"

type localSubtitleInspection struct {
	State              ScreenshotSubtitleState
	Streams            []ScreenshotSubtitleStream
	CurrentSubtitleSID int
}

type subtitleStreamCandidate struct {
	SubtitleSID   int
	StreamIndex   int
	StreamOrdinal int
	CodecName     string
	Title         string
	IsSupported   bool
}

// ShouldUseScreenshotPreview 判断当前视频是否需要进入候选截图选择流程。
// 参数/返回：input 为截图上下文；返回 true 表示字幕未被明确识别为中文，应展示候选截图。
// 失败场景：探测字幕流过程中发生错误时返回 false 与错误，由调用方决定是否回退直出。
// 副作用：会读取视频文件；启用盒子代理时会向代理发起字幕流探测请求。
func ShouldUseScreenshotPreview(input ScreenshotGenerateInput) (bool, error) {
	inspection, err := InspectScreenshotSubtitles(input)
	if err != nil {
		return false, err
	}
	return inspection.SubtitleState != ScreenshotSubtitleStateConfirmedChinese, nil
}

func InspectScreenshotSubtitles(input ScreenshotGenerateInput) (ScreenshotSubtitleInspection, error) {
	payload := input.Payload
	sourceInfo := input.SourceInfo
	savePath, downloaderID, torrentName, contentName := parseScreenshotSourceParams(payload, sourceInfo, input.ContentName)

	downloader, decision, dErr := downloaderclient.DecideProxy(input.RootConfig, downloaderID)
	if decision.Enabled {
		remoteCandidates := buildRemotePathCandidatesForProxy(savePath, torrentName, contentName)
		var lastErr error
		for _, remoteCandidate := range remoteCandidates {
			inspection, err := downloader.InspectScreenshotByProxy(remoteCandidate, contentName)
			if err == nil {
				return ScreenshotSubtitleInspection{
					SubtitleState:      ScreenshotSubtitleState(inspection.SubtitleState),
					SubtitleStreams:    mapProxySubtitleStreams(inspection.SubtitleStreams),
					CurrentSubtitleSID: inspection.CurrentSubtitleSID,
				}, nil
			}
			if apiErr, ok := err.(*downloaderclient.ProxyAPIError); ok && apiErr != nil {
				lastErr = err
				if apiErr.StatusCode == 400 {
					continue
				}
				break
			}
			lastErr = err
			break
		}
		if lastErr != nil {
			return ScreenshotSubtitleInspection{}, lastErr
		}
		return ScreenshotSubtitleInspection{}, nil
	} else if dErr != nil && strings.TrimSpace(decision.Reason) == "config_error" {
		return ScreenshotSubtitleInspection{}, dErr
	}

	targetResult, err := resolveLocalMediaTargetResult(input.RootConfig, downloaderID, savePath, torrentName, contentName, "截图预览判定")
	if err != nil {
		return ScreenshotSubtitleInspection{}, err
	}
	defer func() {
		if closeErr := targetResult.Close(); closeErr != nil {
			logx.Warnf(screenshotPreviewLogModule, "关闭本地媒体访问会话失败 scene=%s source_path=%s err=%v", "截图预览判定", targetResult.SourcePath, closeErr)
		}
	}()

	ffprobePath, err := resolveBinary("ffprobe", "PTNEXUS_FFPROBE_PATH")
	if err != nil {
		return ScreenshotSubtitleInspection{}, err
	}
	inspection, err := inspectLocalSubtitleStreams(ffprobePath, targetResult.TargetFile)
	if err != nil {
		return ScreenshotSubtitleInspection{}, err
	}
	return ScreenshotSubtitleInspection{
		SubtitleState:      inspection.State,
		SubtitleStreams:    inspection.Streams,
		CurrentSubtitleSID: inspection.CurrentSubtitleSID,
	}, nil
}

// GenerateScreenshotPreviewCandidates 生成低清截图候选，供前端人工挑选正式截图时间点。
// 参数/返回：input 为截图上下文；previewCount 为候选数量；返回可展示的候选图片、字幕状态与可选字幕流。
// 失败场景：视频文件无法定位、ffmpeg/ffprobe 缺失、盒子代理不可用或候选不足时返回错误。
// 副作用：会读取视频文件、执行外部命令；启用盒子代理时会发起代理 HTTP 请求。
func GenerateScreenshotPreviewCandidates(input ScreenshotGenerateInput, previewCount int) (ScreenshotPreviewBundle, error) {
	payload := input.Payload
	sourceInfo := input.SourceInfo
	previewCount = normalizeScreenshotPreviewCount(previewCount)
	selectedSubtitleSID, selectedSubtitleProvided := parseSelectedSubtitleSIDAny(payload["selected_subtitle_sid"])

	savePath, downloaderID, torrentName, contentName := parseScreenshotSourceParams(payload, sourceInfo, input.ContentName)
	downloader, decision, dErr := downloaderclient.DecideProxy(input.RootConfig, downloaderID)
	if decision.Enabled {
		remoteCandidates := buildRemotePathCandidatesForProxy(savePath, torrentName, contentName)
		var lastErr error
		for _, remoteCandidate := range remoteCandidates {
			previewBundle, err := downloader.FetchScreenshotPreviewsByProxy(
				remoteCandidate,
				contentName,
				previewCount,
				buildSelectedSubtitleSIDPointer(selectedSubtitleSID, selectedSubtitleProvided),
			)
			if err == nil && len(previewBundle.Candidates) > 0 {
				if len(previewBundle.Candidates) >= screenshotPreviewMinCount {
					logx.Infof(screenshotPreviewLogModule, "盒子代理候选截图生成成功 remote_path=%s count=%d", remoteCandidate, len(previewBundle.Candidates))
					return ScreenshotPreviewBundle{
						Candidates:         mapProxyPreviewCandidates(previewBundle.Candidates),
						SelectionLimit:     screenshotPreviewSelectCount,
						SubtitleState:      ScreenshotSubtitleState(previewBundle.SubtitleState),
						SubtitleStreams:    mapProxySubtitleStreams(previewBundle.SubtitleStreams),
						CurrentSubtitleSID: previewBundle.CurrentSubtitleSID,
					}, nil
				}
				lastErr = fmt.Errorf("候选截图数量不足: %d", len(previewBundle.Candidates))
				break
			}
			if apiErr, ok := err.(*downloaderclient.ProxyAPIError); ok && apiErr != nil {
				lastErr = err
				if apiErr.StatusCode == 400 {
					continue
				}
				break
			}
			lastErr = err
			break
		}
		if lastErr != nil {
			logx.Warnf(screenshotPreviewLogModule, "盒子代理候选截图失败 downloader_id=%s err=%v", downloaderID, lastErr)
		} else {
			logx.Warnf(screenshotPreviewLogModule, "盒子代理候选截图未命中有效路径 downloader_id=%s", downloaderID)
		}
	} else if dErr != nil && strings.TrimSpace(decision.Reason) == "config_error" {
		logx.Warnf(screenshotPreviewLogModule, "盒子代理候选截图跳过：读取下载器配置失败 downloader_id=%s err=%v", downloaderID, dErr)
	}

	targetResult, err := resolveLocalMediaTargetResult(input.RootConfig, downloaderID, savePath, torrentName, contentName, "截图预览生成")
	if err != nil {
		return ScreenshotPreviewBundle{}, err
	}
	defer func() {
		if closeErr := targetResult.Close(); closeErr != nil {
			logx.Warnf(screenshotPreviewLogModule, "关闭本地媒体访问会话失败 scene=%s source_path=%s err=%v", "截图预览生成", targetResult.SourcePath, closeErr)
		}
	}()

	ffmpegPath, err := resolveBinary("ffmpeg", "PTNEXUS_FFMPEG_PATH")
	if err != nil {
		return ScreenshotPreviewBundle{}, err
	}
	ffprobePath, err := resolveBinary("ffprobe", "PTNEXUS_FFPROBE_PATH")
	if err != nil {
		return ScreenshotPreviewBundle{}, err
	}

	inspection, selectedCandidate, hasSelectedCandidate, err := resolveLocalSubtitleCandidate(ffprobePath, targetResult.TargetFile, selectedSubtitleSID)
	if err != nil {
		return ScreenshotPreviewBundle{}, err
	}
	currentSubtitleSID := inspection.CurrentSubtitleSID
	if selectedSubtitleProvided {
		currentSubtitleSID = selectedSubtitleSID
	}

	points, err := buildScreenshotPreviewPoints(
		ffprobePath,
		targetResult.TargetFile,
		previewCount,
		currentSubtitleSID,
		hasSelectedCandidate,
		selectedCandidate,
	)
	if err != nil {
		return ScreenshotPreviewBundle{}, err
	}

	isHDR := detectHDRFromVideo(ffprobePath, targetResult.TargetFile)
	needSubtitleRender := currentSubtitleSID > 0
	mpvPath := ""
	if needSubtitleRender {
		mpvPath, err = resolveBinary("mpv", "PTNEXUS_MPV_PATH")
		if err != nil {
			return ScreenshotPreviewBundle{}, err
		}
	}

	tmpDir, err := os.MkdirTemp("", "ptnexus-preview-screens-*")
	if err != nil {
		return ScreenshotPreviewBundle{}, err
	}
	defer os.RemoveAll(tmpDir)

	candidates := make([]ScreenshotPreviewCandidate, 0, len(points))
	for idx, point := range points {
		filePath := filepath.Join(tmpDir, fmt.Sprintf("preview_%02d.jpg", idx+1))
		var captureErr error
		if needSubtitleRender {
			captureErr = capturePreviewJPEGWithMPV(mpvPath, ffmpegPath, ffprobePath, targetResult.TargetFile, point, filePath, currentSubtitleSID)
		} else {
			captureErr = capturePreviewJPEGWithFFmpeg(ffmpegPath, targetResult.TargetFile, point, filePath, isHDR)
		}
		if captureErr != nil {
			logx.Warnf(screenshotPreviewLogModule, "生成候选截图失败 index=%d point=%.2f err=%v", idx, point, captureErr)
			continue
		}
		content, readErr := os.ReadFile(filePath)
		if readErr != nil {
			logx.Warnf(screenshotPreviewLogModule, "读取候选截图失败 index=%d path=%s err=%v", idx, filePath, readErr)
			continue
		}
		candidates = append(candidates, ScreenshotPreviewCandidate{
			ID:          fmt.Sprintf("candidate-%02d", len(candidates)+1),
			TimeSeconds: point,
			TimeLabel:   formatSecondClock(point),
			PreviewData: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(content),
		})
	}
	if len(candidates) < screenshotPreviewMinCount {
		return ScreenshotPreviewBundle{}, fmt.Errorf("可用候选截图不足，仅生成 %d 张", len(candidates))
	}
	markRecommendedPreviewCandidates(candidates, screenshotPreviewSelectCount)
	return ScreenshotPreviewBundle{
		Candidates:         candidates,
		SelectionLimit:     screenshotPreviewSelectCount,
		SubtitleState:      inspection.State,
		SubtitleStreams:    inspection.Streams,
		CurrentSubtitleSID: currentSubtitleSID,
	}, nil
}

// GenerateAndUploadSelectedScreenshots 按用户选择的时间点生成正式截图并上传图床。
// 参数/返回：input 为截图上下文；selectedTimes 为前端挑选的时间点；返回上传后的正式截图 URL 列表。
// 失败场景：选中时间点为空或无效、视频解析失败、截图/上传失败时返回错误。
// 副作用：会读取视频、执行 mpv/ffmpeg，并向图床或盒子代理发起请求。
func GenerateAndUploadSelectedScreenshots(input ScreenshotGenerateInput, selectedTimes []float64) ([]string, error) {
	return generateAndUploadScreenshotsWithPoints(input, selectedTimes, true)
}

func generateAndUploadScreenshotsWithPoints(input ScreenshotGenerateInput, selectedTimes []float64, requireSelected bool) ([]string, error) {
	payload := input.Payload
	sourceInfo := input.SourceInfo
	selectedSubtitleSID, selectedSubtitleProvided := parseSelectedSubtitleSIDAny(payload["selected_subtitle_sid"])

	logx.PlainInfof("开始执行截图和上传任务 (智能 HDR/SDR + 自动中文字幕)...")
	hoster := "pixhost"
	logx.PlainInfof("已选择图床服务: %s, 截图数量: %d", hoster, screenshotTotalCount)

	savePath, downloaderID, torrentName, contentName := parseScreenshotSourceParams(payload, sourceInfo, input.ContentName)

	downloader, decision, dErr := downloaderclient.DecideProxy(input.RootConfig, downloaderID)
	if decision.Enabled {
		remoteCandidates := buildRemotePathCandidatesForProxy(savePath, torrentName, contentName)
		var lastErr error
		for _, remoteCandidate := range remoteCandidates {
			var bbcode string
			var err error
			if requireSelected {
				bbcode, err = downloader.FetchSelectedScreenshotsByProxy(
					remoteCandidate,
					contentName,
					selectedTimes,
					buildSelectedSubtitleSIDPointer(selectedSubtitleSID, selectedSubtitleProvided),
				)
			} else {
				bbcode, err = downloader.FetchScreenshotsByProxy(
					remoteCandidate,
					contentName,
					buildSelectedSubtitleSIDPointer(selectedSubtitleSID, selectedSubtitleProvided),
				)
			}
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

	targetResult, err := resolveLocalMediaTargetResult(input.RootConfig, downloaderID, savePath, torrentName, contentName, "正式截图生成")
	if err != nil {
		logx.PlainWarnf("错误：在指定路径中未找到视频文件: %v", err)
		return nil, err
	}
	defer func() {
		if closeErr := targetResult.Close(); closeErr != nil {
			logx.Warnf(screenshotPreviewLogModule, "关闭本地媒体访问会话失败 scene=%s source_path=%s err=%v", "正式截图生成", targetResult.SourcePath, closeErr)
		}
	}()
	logx.PlainInfof("找到目标媒体文件: source=%s target=%s", targetResult.SourcePath, targetResult.TargetFile)

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

	logx.PlainInfof("正在分析字幕流...")
	inspection, selectedCandidate, hasSelectedCandidate, err := resolveLocalSubtitleCandidate(ffprobePath, targetResult.TargetFile, selectedSubtitleSID)
	if err != nil {
		return nil, err
	}

	subtitleSID := inspection.CurrentSubtitleSID
	if selectedSubtitleProvided {
		subtitleSID = selectedSubtitleSID
	}
	switch {
	case subtitleSID <= 0:
		logx.PlainInfof("   当前选择为无字幕，将截取无字幕画面。")
	case selectedSubtitleProvided && hasSelectedCandidate:
		logx.PlainInfof("   已按用户选择的字幕流截图 sid=%d title=%s", subtitleSID, strings.TrimSpace(selectedCandidate.Title))
	case inspection.State == ScreenshotSubtitleStateConfirmedChinese:
		logx.PlainInfof("   已检测到明确中文字幕，将自动挂载字幕截图 sid=%d", subtitleSID)
	default:
		logx.PlainInfof("   检测到可用但未确认的字幕流，将使用当前预览字幕流截图 sid=%d", subtitleSID)
	}

	points, err := resolveFormalScreenshotPoints(
		ffprobePath,
		targetResult.TargetFile,
		selectedTimes,
		screenshotTotalCount,
		requireSelected,
		subtitleSID,
		hasSelectedCandidate,
		selectedCandidate,
	)
	if err != nil {
		return nil, err
	}
	sort.Float64s(points)

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
				var buf strings.Builder
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

		if err := captureRawPNGWithMPV(mpvPath, targetResult.TargetFile, point, rawPNG, subtitleSID); err != nil {
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

func parseScreenshotSourceParams(payload map[string]any, sourceInfo map[string]any, contentName string) (string, string, string, string) {
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
	return savePath, downloaderID, torrentName, strings.TrimSpace(contentName)
}

func normalizeScreenshotPreviewCount(value int) int {
	if value <= 0 {
		return screenshotPreviewDefaultCount
	}
	if value < screenshotPreviewMinCount {
		return screenshotPreviewMinCount
	}
	if value > 20 {
		return 20
	}
	return value
}

func buildScreenshotPreviewPoints(
	ffprobePath,
	videoPath string,
	previewCount int,
	currentSubtitleSID int,
	hasSelectedCandidate bool,
	selectedCandidate subtitleStreamCandidate,
) ([]float64, error) {
	duration, err := probeDurationSeconds(videoPath)
	if err != nil || duration <= 0 {
		return nil, fmt.Errorf("读取视频时长失败: %w", err)
	}
	wantSmart := previewCount
	if wantSmart > screenshotPreviewSelectCount {
		wantSmart = screenshotPreviewSelectCount
	}
	smartPoints := buildSmartPointsForSelectedSubtitle(ffprobePath, videoPath, wantSmart, currentSubtitleSID, hasSelectedCandidate, selectedCandidate)
	fallbackPoints := buildPreviewFallbackPoints(duration, previewCount)
	merged := mergeScreenshotPointCandidates(smartPoints, fallbackPoints, previewCount, duration)
	if len(merged) == 0 {
		return nil, fmt.Errorf("未找到可用的候选截图时间点")
	}
	return merged, nil
}

func resolveFormalScreenshotPoints(
	ffprobePath,
	targetVideoFile string,
	selectedPoints []float64,
	want int,
	requireSelected bool,
	currentSubtitleSID int,
	hasSelectedCandidate bool,
	selectedCandidate subtitleStreamCandidate,
) ([]float64, error) {
	duration, err := probeDurationSeconds(targetVideoFile)
	if err != nil || duration <= 0 {
		return nil, fmt.Errorf("读取视频时长失败: %w", err)
	}
	cleanSelected := sanitizeSelectedScreenshotTimes(selectedPoints, duration)
	if len(cleanSelected) > 0 {
		return cleanSelected, nil
	}
	if requireSelected {
		return nil, fmt.Errorf("请选择 %d 张候选截图后再生成正式截图", screenshotPreviewSelectCount)
	}
	points := buildSmartPointsForSelectedSubtitle(ffprobePath, targetVideoFile, want, currentSubtitleSID, hasSelectedCandidate, selectedCandidate)
	if len(points) < want {
		logx.PlainWarnf("警告: 智能分析失败，回退到按百分比截图。")
		percents := []float64{0.15, 0.30, 0.50, 0.70, 0.85}
		points = make([]float64, 0, len(percents))
		for _, p := range percents {
			points = append(points, duration*p)
		}
	}
	return points, nil
}

func buildSmartPointsForSelectedSubtitle(
	ffprobePath,
	videoPath string,
	want int,
	currentSubtitleSID int,
	hasSelectedCandidate bool,
	selectedCandidate subtitleStreamCandidate,
) []float64 {
	if currentSubtitleSID <= 0 {
		return nil
	}
	if hasSelectedCandidate {
		return getSmartScreenshotPointsForSubtitle(ffprobePath, videoPath, want, selectedCandidate.SubtitleSID, true)
	}
	return getSmartScreenshotPointsForSubtitle(ffprobePath, videoPath, want, currentSubtitleSID, true)
}

func buildPreviewFallbackPoints(duration float64, count int) []float64 {
	if duration <= 0 || count <= 0 {
		return nil
	}
	start := duration * 0.12
	end := duration * 0.88
	if end <= start {
		start = duration * 0.10
		end = duration * 0.90
	}
	segment := (end - start) / float64(count)
	if segment <= 0 {
		segment = duration / float64(count+1)
	}
	points := make([]float64, 0, count)
	for i := 0; i < count; i++ {
		point := start + segment*(float64(i)+0.5)
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

func mergeScreenshotPointCandidates(primary []float64, secondary []float64, limit int, duration float64) []float64 {
	points := make([]float64, 0, limit)
	minSpacing := math.Max(12, duration/240)
	appendPoint := func(candidate float64) {
		if candidate <= 0 {
			return
		}
		if candidate > duration-1 {
			candidate = math.Max(1, duration-1)
		}
		for _, existing := range points {
			if math.Abs(existing-candidate) < minSpacing {
				return
			}
		}
		points = append(points, candidate)
	}
	for _, candidate := range primary {
		if len(points) >= limit {
			break
		}
		appendPoint(candidate)
	}
	for _, candidate := range secondary {
		if len(points) >= limit {
			break
		}
		appendPoint(candidate)
	}
	sort.Float64s(points)
	if len(points) > limit {
		return points[:limit]
	}
	return points
}

func capturePreviewJPEGWithFFmpeg(ffmpegPath, videoPath string, second float64, outputPath string, isHDR bool) error {
	vfFilter := "scale='min(640,iw)':-2,format=yuv420p"
	if isHDR {
		vfFilter = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,scale='min(640,iw)':-2,format=yuv420p"
	}
	cmd := exec.Command(
		ffmpegPath,
		"-y",
		"-ss", fmt.Sprintf("%.3f", second),
		"-i", videoPath,
		"-frames:v", "1",
		"-an",
		"-sn",
		"-vf", vfFilter,
		"-q:v", "14",
		outputPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("ffmpeg 预览截图失败: %s", text)
	}
	if stat, statErr := os.Stat(outputPath); statErr != nil || stat.Size() == 0 {
		return fmt.Errorf("预览截图文件未生成")
	}
	return nil
}

func capturePreviewJPEGWithMPV(mpvPath, ffmpegPath, ffprobePath, videoPath string, second float64, outputPath string, subtitleSID int) error {
	rawPNG := filepath.Join(filepath.Dir(outputPath), strings.TrimSuffix(filepath.Base(outputPath), filepath.Ext(outputPath))+"_raw.png")
	if err := captureRawPNGWithMPV(mpvPath, videoPath, second, rawPNG, subtitleSID); err != nil {
		return err
	}

	isHDR := false
	if hdr, err := detectHDRFromPNG(ffprobePath, rawPNG); err == nil {
		isHDR = hdr
	}
	return compressPreviewJPEGFromPNG(ffmpegPath, rawPNG, outputPath, isHDR)
}

func compressPreviewJPEGFromPNG(ffmpegPath, srcPNG, outputPath string, isHDR bool) error {
	vfFilter := "scale='min(640,iw)':-2,format=yuv420p"
	if isHDR {
		vfFilter = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,scale='min(640,iw)':-2,format=yuv420p"
	}
	cmd := exec.Command(
		ffmpegPath,
		"-y",
		"-v", "error",
		"-i", srcPNG,
		"-frames:v", "1",
		"-vf", vfFilter,
		"-q:v", "14",
		outputPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("ffmpeg 预览压缩失败: %s", text)
	}
	if stat, statErr := os.Stat(outputPath); statErr != nil || stat.Size() == 0 {
		return fmt.Errorf("预览截图文件未生成")
	}
	return nil
}

func detectHDRFromVideo(ffprobePath, videoPath string) bool {
	cmd := exec.Command(ffprobePath, "-v", "error", "-show_streams", "-select_streams", "v:0", videoPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	text := strings.ToLower(string(out))
	return strings.Contains(text, "smpte2084") || strings.Contains(text, "bt2020")
}

func sanitizeSelectedScreenshotTimes(values []float64, duration float64) []float64 {
	clean := make([]float64, 0, len(values))
	for _, value := range values {
		if value <= 0 || value >= duration {
			continue
		}
		duplicated := false
		for _, existing := range clean {
			if math.Abs(existing-value) < 0.8 {
				duplicated = true
				break
			}
		}
		if duplicated {
			continue
		}
		clean = append(clean, value)
	}
	sort.Float64s(clean)
	if len(clean) > screenshotPreviewSelectCount {
		clean = clean[:screenshotPreviewSelectCount]
	}
	return clean
}

func parseFloatSliceAny(value any) []float64 {
	result := make([]float64, 0)
	switch typed := value.(type) {
	case []float64:
		return sanitizeSelectedScreenshotTimes(typed, math.MaxFloat64)
	case []any:
		for _, item := range typed {
			if parsed, ok := parseFloatAny(item); ok {
				result = append(result, parsed)
			}
		}
	case []int:
		for _, item := range typed {
			result = append(result, float64(item))
		}
	case []string:
		for _, item := range typed {
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(item), 64); err == nil {
				result = append(result, parsed)
			}
		}
	}
	return result
}

func parsePreviewCountAny(value any) int {
	if parsed, ok := parseIntAny(value); ok {
		return parsed
	}
	return screenshotPreviewDefaultCount
}

func parseIntAny(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case float32:
		return int(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed), true
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func parseFloatAny(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		if err == nil {
			return parsed, true
		}
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func markRecommendedPreviewCandidates(candidates []ScreenshotPreviewCandidate, want int) {
	if len(candidates) == 0 || want <= 0 {
		return
	}
	if want > len(candidates) {
		want = len(candidates)
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

func inspectLocalSubtitleStreams(ffprobePath, videoPath string) (localSubtitleInspection, error) {
	inspection, _, err := resolveScreenshotSubtitleSelection(ffprobePath, videoPath, 0, false)
	if err != nil {
		return localSubtitleInspection{}, err
	}
	return localSubtitleInspection{
		State:              inspection.SubtitleState,
		Streams:            inspection.SubtitleStreams,
		CurrentSubtitleSID: inspection.CurrentSubtitleSID,
	}, nil
}

func resolveLocalSubtitleCandidate(ffprobePath, videoPath string, selectedSubtitleSID int) (localSubtitleInspection, subtitleStreamCandidate, bool, error) {
	inspection, selectedStream, err := resolveScreenshotSubtitleSelection(ffprobePath, videoPath, selectedSubtitleSID, selectedSubtitleSID > 0)
	if err != nil {
		return localSubtitleInspection{}, subtitleStreamCandidate{}, false, err
	}
	result := localSubtitleInspection{
		State:              inspection.SubtitleState,
		Streams:            inspection.SubtitleStreams,
		CurrentSubtitleSID: inspection.CurrentSubtitleSID,
	}
	if selectedStream == nil {
		return result, subtitleStreamCandidate{}, false, nil
	}
	return result, subtitleStreamCandidate{
		SubtitleSID:   selectedStream.SubtitleSID,
		StreamIndex:   selectedStream.StreamIndex,
		StreamOrdinal: selectedStream.StreamOrdinal,
		CodecName:     selectedStream.CodecName,
		Title:         selectedStream.Title,
		IsSupported:   selectedStream.SupportsEventExtraction,
	}, true, nil
}

func parseSelectedSubtitleSIDAny(value any) (int, bool) {
	if value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0, true
		}
		if parsed, ok := parseIntAny(typed); ok {
			return parsed, true
		}
		return 0, true
	default:
		if parsed, ok := parseIntAny(typed); ok {
			return parsed, true
		}
		return 0, true
	}
}

func buildSelectedSubtitleSIDPointer(selectedSubtitleSID int, provided bool) *int {
	if !provided {
		return nil
	}
	value := selectedSubtitleSID
	return &value
}

func mapProxySubtitleStreams(streams []downloaderclient.ProxyScreenshotSubtitleStream) []ScreenshotSubtitleStream {
	mapped := make([]ScreenshotSubtitleStream, 0, len(streams))
	for _, stream := range streams {
		mapped = append(mapped, ScreenshotSubtitleStream{
			SubtitleSID:        stream.SubtitleSID,
			StreamIndex:        stream.StreamIndex,
			CodecName:          strings.TrimSpace(stream.CodecName),
			Language:           strings.TrimSpace(stream.Language),
			Title:              strings.TrimSpace(stream.Title),
			DisplayName:        strings.TrimSpace(stream.DisplayName),
			IsConfidentChinese: stream.IsConfidentChinese,
			IsDefault:          stream.IsDefault,
		})
	}
	return mapped
}

func mapProxyPreviewCandidates(candidates []downloaderclient.ProxyScreenshotPreviewCandidate) []ScreenshotPreviewCandidate {
	mapped := make([]ScreenshotPreviewCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		mapped = append(mapped, ScreenshotPreviewCandidate{
			ID:          strings.TrimSpace(candidate.ID),
			TimeSeconds: candidate.TimeSeconds,
			TimeLabel:   strings.TrimSpace(candidate.TimeLabel),
			PreviewData: strings.TrimSpace(candidate.PreviewData),
			Recommended: candidate.Recommended,
		})
	}
	return mapped
}
