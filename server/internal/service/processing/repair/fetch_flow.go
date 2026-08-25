package repair

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
)

const (
	fetchRepairLogModule           = "迁移-抓取修复"
	fetchRepairMediaLogModule      = "迁移-媒体修复"
	fetchRepairPosterLogModule     = "迁移-海报修复"
	fetchRepairScreenshotLogModule = "迁移-截图修复"
	fetchRepairIntroLogModule      = "迁移-简介修复"

	fetchScreenshotValidateWorker = 4
)

var fetchForbiddenMediaPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)\[b\]`),
	regexp.MustCompile(`(?is)\[color=[^\]]+\]`),
	regexp.MustCompile(`(?is)\[size=[^\]]+\]`),
	regexp.MustCompile(`(?is)\[/[^\]]+\]`),
	regexp.MustCompile(`★{2,}`),
	regexp.MustCompile(`。{3,}`),
	regexp.MustCompile(`…{2,}`),
	regexp.MustCompile(`……{2,}`),
}

// FetchRepairDeps 描述抓取修复流程需要的外部依赖。
type FetchRepairDeps struct {
	EmitLog          func(taskID, step, message, status string)
	RefreshMediainfo func(payload map[string]any) (map[string]any, int)
	CSPTToken        string
	RootConfig       map[string]any
}

// ParallelFetchRepairInput 表示并发修复输入。
type ParallelFetchRepairInput struct {
	TaskID               string
	SourceSite           string
	SavePath             string
	DownloaderID         string
	TorrentNameForPath   string
	TorrentName          string
	ScreenshotReviewMode string
	Subtitle             string
	ReviewData           parser.ReviewExtractedData
	IMDbLink             string
	DoubanLink           string
	TMDbLink             string
}

// ParallelFetchRepairResult 表示并发修复输出。
type ParallelFetchRepairResult struct {
	ReviewData                parser.ReviewExtractedData
	IMDbLink                  string
	DoubanLink                string
	TMDbLink                  string
	ScreenshotReviewStatus    string
	ScreenshotPreviewRequired bool
}

type posterRepairResult struct {
	Poster     string
	IMDbLink   string
	DoubanLink string
	TMDbLink   string
}

type introRepairResult struct {
	Body       string
	IMDbLink   string
	DoubanLink string
	TMDbLink   string
}

type screenshotsRepairResult struct {
	Screens                   string
	ScreenshotReviewStatus    string
	ScreenshotPreviewRequired bool
}

type screenshotValidateJob struct {
	index int
	url   string
}

type screenshotValidateResult struct {
	index int
	ok    bool
}

// TriggerMediainfoRepairInput 表示抓取链路中触发媒体刷新所需参数。
type TriggerMediainfoRepairInput struct {
	TaskID          string
	SeedID          string
	SavePath        string
	ContentName     string
	DownloaderID    string
	TorrentNamePath string
	CurrentMedia    string
}

// ValidateFetchedMediainfoFormat 判断抓取出的 mediainfo 文本是否满足基础规范。
func ValidateFetchedMediainfoFormat(text string) (bool, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false, "媒体信息为空"
	}
	sanitized := strings.TrimSpace(parser.SanitizeMediaTextForAnalysis(trimmed))
	if sanitized == "" {
		return false, "媒体信息为空"
	}
	if !parser.IsLikelyMediaInfoText(sanitized) && !parser.IsLikelyBDInfoText(sanitized) {
		return false, "关键字不足"
	}
	for _, pattern := range fetchForbiddenMediaPatterns {
		if pattern.MatchString(sanitized) {
			return false, "命中禁止模式"
		}
	}
	return true, ""
}

// RunParallelFetchRepairs 并发执行海报/简介/截图修复。
func RunParallelFetchRepairs(input ParallelFetchRepairInput, deps FetchRepairDeps) ParallelFetchRepairResult {
	startedAt := time.Now()
	logx.Infof(fetchRepairLogModule, "并发修复启动 task_id=%s torrent_name=%s", input.TaskID, CompactLogText(input.TorrentName, 200))

	posterResultCh := make(chan posterRepairResult, 1)
	introResultCh := make(chan introRepairResult, 1)
	screenshotResultCh := make(chan screenshotsRepairResult, 1)

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		taskStartedAt := time.Now()
		result := posterRepairResult{}
		defer func() {
			if recovered := recover(); recovered != nil {
				logx.Errorf(fetchRepairPosterLogModule, "并发任务异常 task_id=%s err=%v", input.TaskID, recovered)
			}
			logx.Infof(fetchRepairPosterLogModule, "并发任务结束 task_id=%s elapsed_ms=%d", input.TaskID, time.Since(taskStartedAt).Milliseconds())
			posterResultCh <- result
		}()

		logx.Infof(fetchRepairPosterLogModule, "并发任务开始 task_id=%s", input.TaskID)
		result = runPosterRepairTask(input, deps)
	}()

	go func() {
		defer wg.Done()
		taskStartedAt := time.Now()
		result := introRepairResult{}
		defer func() {
			if recovered := recover(); recovered != nil {
				logx.Errorf(fetchRepairIntroLogModule, "并发任务异常 task_id=%s err=%v", input.TaskID, recovered)
			}
			logx.Infof(fetchRepairIntroLogModule, "并发任务结束 task_id=%s elapsed_ms=%d", input.TaskID, time.Since(taskStartedAt).Milliseconds())
			introResultCh <- result
		}()

		logx.Infof(fetchRepairIntroLogModule, "并发任务开始 task_id=%s", input.TaskID)
		result = runIntroRepairTask(input, deps)
	}()

	go func() {
		defer wg.Done()
		taskStartedAt := time.Now()
		result := screenshotsRepairResult{}
		defer func() {
			if recovered := recover(); recovered != nil {
				logx.Errorf(fetchRepairScreenshotLogModule, "并发任务异常 task_id=%s err=%v", input.TaskID, recovered)
			}
			logx.Infof(fetchRepairScreenshotLogModule, "并发任务结束 task_id=%s elapsed_ms=%d", input.TaskID, time.Since(taskStartedAt).Milliseconds())
			screenshotResultCh <- result
		}()

		logx.Infof(fetchRepairScreenshotLogModule, "并发任务开始 task_id=%s", input.TaskID)
		result = runScreenshotsRepairTask(input, deps)
	}()

	wg.Wait()

	posterResult := <-posterResultCh
	introResult := <-introResultCh
	screenshotResult := <-screenshotResultCh

	merged := ParallelFetchRepairResult{
		ReviewData:                input.ReviewData,
		IMDbLink:                  strings.TrimSpace(input.IMDbLink),
		DoubanLink:                strings.TrimSpace(input.DoubanLink),
		TMDbLink:                  strings.TrimSpace(input.TMDbLink),
		ScreenshotReviewStatus:    processingshared.ScreenshotReviewStatusNone,
		ScreenshotPreviewRequired: false,
	}

	if strings.TrimSpace(posterResult.Poster) != "" {
		merged.ReviewData.Poster = posterResult.Poster
	}
	if strings.TrimSpace(introResult.Body) != "" {
		merged.ReviewData.Body = introResult.Body
	}
	if strings.TrimSpace(screenshotResult.Screens) != "" {
		merged.ReviewData.Screens = screenshotResult.Screens
	}
	merged.ScreenshotReviewStatus = processingshared.NormalizeScreenshotReviewStatus(screenshotResult.ScreenshotReviewStatus)
	merged.ScreenshotPreviewRequired = screenshotResult.ScreenshotPreviewRequired

	merged.IMDbLink = firstNonEmpty(merged.IMDbLink, strings.TrimSpace(posterResult.IMDbLink), strings.TrimSpace(introResult.IMDbLink))
	merged.DoubanLink = firstNonEmpty(merged.DoubanLink, strings.TrimSpace(posterResult.DoubanLink), strings.TrimSpace(introResult.DoubanLink))
	merged.TMDbLink = firstNonEmpty(merged.TMDbLink, strings.TrimSpace(posterResult.TMDbLink), strings.TrimSpace(introResult.TMDbLink))
	tmdbBackfillAttempted := false
	merged.TMDbLink = firstNonEmpty(
		merged.TMDbLink,
		backfillTMDbByIMDbIfNeeded(
			merged.TMDbLink,
			merged.IMDbLink,
			&tmdbBackfillAttempted,
			fetchRepairLogModule,
			"并发修复汇总后",
		),
	)
	if strings.TrimSpace(merged.ReviewData.Body) != "" {
		merged.ReviewData.Body = ensureTMDbLinkLineForPTGenIntro(merged.ReviewData.Body, merged.TMDbLink)
	}

	logx.Infof(
		fetchRepairLogModule,
		"并发修复结束 task_id=%s elapsed_ms=%d poster_changed=%t intro_changed=%t screens_changed=%t",
		input.TaskID,
		time.Since(startedAt).Milliseconds(),
		strings.TrimSpace(input.ReviewData.Poster) != strings.TrimSpace(merged.ReviewData.Poster),
		strings.TrimSpace(input.ReviewData.Body) != strings.TrimSpace(merged.ReviewData.Body),
		strings.TrimSpace(input.ReviewData.Screens) != strings.TrimSpace(merged.ReviewData.Screens),
	)

	return merged
}

// Summary 返回并发修复结果摘要，便于日志输出。
func (r ParallelFetchRepairResult) Summary() string {
	return "poster=" + boolText(strings.TrimSpace(r.ReviewData.Poster) != "") +
		" intro=" + boolText(strings.TrimSpace(r.ReviewData.Body) != "") +
		" screens=" + boolText(strings.TrimSpace(r.ReviewData.Screens) != "") +
		" screenshot_review=" + processingshared.NormalizeScreenshotReviewStatus(r.ScreenshotReviewStatus) +
		" screenshot_preview_required=" + boolText(r.ScreenshotPreviewRequired)
}

// TriggerMediainfoRepairDuringFetch 在抓取流程内触发媒体信息修复。
func TriggerMediainfoRepairDuringFetch(input TriggerMediainfoRepairInput, deps FetchRepairDeps) {
	logx.Warnf(fetchRepairMediaLogModule, "媒体信息不合规，开始自动修复 seed_id=%s", input.SeedID)
	emitLog(deps, input.TaskID, "修复媒体信息", "媒体信息不合规，正在自动修复...", "processing")

	if deps.RefreshMediainfo == nil {
		logx.Warnf(fetchRepairMediaLogModule, "媒体信息自动修复跳过 seed_id=%s reason=RefreshMediainfo回调为空", input.SeedID)
		emitLog(deps, input.TaskID, "修复媒体信息", "媒体信息自动修复失败：服务未注册刷新回调", "warning")
		return
	}

	resp, status := deps.RefreshMediainfo(map[string]any{
		"seed_id":           strings.TrimSpace(input.SeedID),
		"save_path":         strings.TrimSpace(input.SavePath),
		"content_name":      strings.TrimSpace(input.ContentName),
		"downloader_id":     strings.TrimSpace(input.DownloaderID),
		"torrent_name":      strings.TrimSpace(input.TorrentNamePath),
		"current_mediainfo": strings.TrimSpace(input.CurrentMedia),
		"force_refresh":     true,
		"priority":          2,
	})
	if status >= 400 {
		message := toStringAny(resp["message"], toStringAny(resp["error"], "未知错误"))
		logx.Warnf(fetchRepairMediaLogModule, "媒体信息自动修复失败 seed_id=%s status=%d message=%s", input.SeedID, status, message)
		emitLog(deps, input.TaskID, "修复媒体信息", "媒体信息自动修复失败："+message, "warning")
		return
	}

	bdinfoAsync, _ := resp["bdinfo_async"].(map[string]any)
	bdinfoStatus := strings.TrimSpace(toStringAny(bdinfoAsync["bdinfo_status"], ""))
	bdinfoTaskID := strings.TrimSpace(toStringAny(bdinfoAsync["bdinfo_task_id"], ""))
	if bdinfoStatus == "processing" && bdinfoTaskID != "" {
		logx.Infof(fetchRepairMediaLogModule, "媒体信息自动修复已转BDInfo seed_id=%s task_id=%s", input.SeedID, bdinfoTaskID)
		emitLog(deps, input.TaskID, "修复媒体信息", "检测到蓝光目录，已启动 BDInfo 后台任务", "info")
		return
	}

	if strings.TrimSpace(toStringAny(resp["mediainfo"], "")) != "" {
		logx.Infof(fetchRepairMediaLogModule, "媒体信息自动修复成功 seed_id=%s", input.SeedID)
		emitLog(deps, input.TaskID, "修复媒体信息", "媒体信息自动修复成功", "success")
		return
	}

	logx.Warnf(fetchRepairMediaLogModule, "媒体信息自动修复未产出有效内容 seed_id=%s", input.SeedID)
	emitLog(deps, input.TaskID, "修复媒体信息", "媒体信息修复未产出有效内容", "warning")
}

func runPosterRepairTask(input ParallelFetchRepairInput, deps FetchRepairDeps) posterRepairResult {
	localReview := input.ReviewData
	localIMDb := strings.TrimSpace(input.IMDbLink)
	localDouban := strings.TrimSpace(input.DoubanLink)
	localTMDb := strings.TrimSpace(input.TMDbLink)

	repairPosterDuringFetch(input.TaskID, input.TorrentName, input.Subtitle, &localReview, &localIMDb, &localDouban, &localTMDb, deps)

	return posterRepairResult{
		Poster:     localReview.Poster,
		IMDbLink:   localIMDb,
		DoubanLink: localDouban,
		TMDbLink:   localTMDb,
	}
}

func runIntroRepairTask(input ParallelFetchRepairInput, deps FetchRepairDeps) introRepairResult {
	localReview := input.ReviewData
	localIMDb := strings.TrimSpace(input.IMDbLink)
	localDouban := strings.TrimSpace(input.DoubanLink)
	localTMDb := strings.TrimSpace(input.TMDbLink)

	if isNovaHDSourceSite(input.SourceSite) {
		logx.Infof(fetchRepairIntroLogModule, "NovaHD 简介沿用源站提取结果 task_id=%s title=%s body=%t", input.TaskID, input.TorrentName, strings.TrimSpace(localReview.Body) != "")
		emitLog(deps, input.TaskID, "修复简介", "NovaHD 简介直接使用源站内容，已跳过二次补全", "info")
		return introRepairResult{
			Body:       localReview.Body,
			IMDbLink:   localIMDb,
			DoubanLink: localDouban,
			TMDbLink:   localTMDb,
		}
	}

	repairIntroBodyDuringFetch(input.TaskID, input.TorrentName, input.Subtitle, &localReview, &localIMDb, &localDouban, &localTMDb, deps)

	return introRepairResult{
		Body:       localReview.Body,
		IMDbLink:   localIMDb,
		DoubanLink: localDouban,
		TMDbLink:   localTMDb,
	}
}

func isNovaHDSourceSite(siteName string) bool {
	normalized := strings.ToLower(strings.TrimSpace(siteName))
	if normalized == "" {
		return false
	}
	compact := strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(normalized)
	return strings.Contains(compact, "novahd")
}

func runScreenshotsRepairTask(input ParallelFetchRepairInput, deps FetchRepairDeps) screenshotsRepairResult {
	localReview := input.ReviewData

	reviewStatus, previewRequired := repairScreenshotsDuringFetch(
		input.TaskID,
		input.SavePath,
		input.DownloaderID,
		input.TorrentNameForPath,
		input.TorrentName,
		input.ScreenshotReviewMode,
		&localReview,
		deps,
	)

	return screenshotsRepairResult{
		Screens:                   localReview.Screens,
		ScreenshotReviewStatus:    processingshared.NormalizeScreenshotReviewStatus(reviewStatus),
		ScreenshotPreviewRequired: previewRequired,
	}
}

func repairPosterDuringFetch(
	taskID string,
	torrentName string,
	subtitle string,
	reviewData *parser.ReviewExtractedData,
	imdbLink *string,
	doubanLink *string,
	tmdbLink *string,
	deps FetchRepairDeps,
) {
	if reviewData == nil {
		return
	}

	reviewData.Poster = NormalizePosterBBCodeWithConfig(reviewData.Poster, deps.RootConfig)
	posterURL := ""
	if urls := ExtractImageURLsFromText(reviewData.Poster); len(urls) > 0 {
		posterURL = strings.TrimSpace(urls[0])
	}
	posterReachable := false
	if posterURL != "" {
		posterReachable = IsImageURLReachable(posterURL)
	}
	logx.Infof(
		fetchRepairPosterLogModule,
		"海报初始校验 title=%s has_url=%t reachable=%t url=%s",
		CompactLogText(torrentName, 120),
		posterURL != "",
		posterReachable,
		CompactLogText(posterURL, 200),
	)

	if posterURL != "" && posterReachable {
		logx.Infof(fetchRepairPosterLogModule, "海报校验通过 url=%s", posterURL)
		emitLog(deps, taskID, "修复海报", "海报校验通过", "success")
		return
	}

	logx.Warnf(fetchRepairPosterLogModule, "海报无效或缺失，开始自动修复 title=%s", torrentName)
	emitLog(deps, taskID, "修复海报", "海报无效或缺失，正在自动修复...", "processing")

	posterSource := map[string]any{
		"imdb_link":   strings.TrimSpace(stringValue(imdbLink)),
		"douban_link": strings.TrimSpace(stringValue(doubanLink)),
		"tmdb_link":   strings.TrimSpace(stringValue(tmdbLink)),
	}
	logx.Infof(
		fetchRepairPosterLogModule,
		"开始调用媒体信息兜底获取海报 title=%s imdb=%s douban=%s tmdb=%s",
		CompactLogText(torrentName, 120),
		CompactLogText(strings.TrimSpace(stringValue(imdbLink)), 120),
		CompactLogText(strings.TrimSpace(stringValue(doubanLink)), 120),
		CompactLogText(strings.TrimSpace(stringValue(tmdbLink)), 120),
	)
	posterResult, posterErr := FetchMovieInfo("poster", torrentName, subtitle, posterSource, strings.TrimSpace(deps.CSPTToken))
	if posterErr != "" {
		logx.Warnf(fetchRepairPosterLogModule, "海报自动修复失败 title=%s err=%s", torrentName, posterErr)
		emitLog(deps, taskID, "修复海报", "海报自动修复失败："+posterErr, "warning")
		return
	}
	logx.Infof(
		fetchRepairPosterLogModule,
		"海报兜底获取返回 title=%s poster_non_empty=%t imdb=%s douban=%s tmdb=%s",
		CompactLogText(torrentName, 120),
		strings.TrimSpace(posterResult.Poster) != "",
		CompactLogText(posterResult.IMDb, 120),
		CompactLogText(posterResult.Douban, 120),
		CompactLogText(posterResult.TMDb, 120),
	)

	if strings.TrimSpace(posterResult.Poster) != "" {
		reviewData.Poster = NormalizePosterBBCodeWithConfig(posterResult.Poster, deps.RootConfig)
		normalizedPosterURLs := ExtractImageURLsFromText(reviewData.Poster)
		firstURL := ""
		if len(normalizedPosterURLs) > 0 {
			firstURL = strings.TrimSpace(normalizedPosterURLs[0])
		}
		logx.Infof(
			fetchRepairPosterLogModule,
			"海报兜底规范化完成 title=%s extracted_count=%d first_url=%s",
			CompactLogText(torrentName, 120),
			len(normalizedPosterURLs),
			CompactLogText(firstURL, 200),
		)
	}
	*imdbLink = firstNonEmpty(strings.TrimSpace(stringValue(imdbLink)), strings.TrimSpace(posterResult.IMDb))
	*doubanLink = firstNonEmpty(strings.TrimSpace(stringValue(doubanLink)), strings.TrimSpace(posterResult.Douban))
	*tmdbLink = firstNonEmpty(strings.TrimSpace(stringValue(tmdbLink)), strings.TrimSpace(posterResult.TMDb))

	if urls := ExtractImageURLsFromText(reviewData.Poster); len(urls) > 0 {
		logx.Infof(fetchRepairPosterLogModule, "海报自动修复成功 url=%s", urls[0])
		emitLog(deps, taskID, "修复海报", "海报自动修复成功", "success")
		return
	}
	logx.Warnf(
		fetchRepairPosterLogModule,
		"海报自动修复未产出可用图片 title=%s reason=poster_empty_after_fetch raw_poster=%s",
		CompactLogText(torrentName, 120),
		CompactLogText(strings.TrimSpace(posterResult.Poster), 240),
	)
	emitLog(deps, taskID, "修复海报", "海报修复未产出可用图片", "warning")
}

func repairIntroBodyDuringFetch(
	taskID string,
	torrentName string,
	subtitle string,
	reviewData *parser.ReviewExtractedData,
	imdbLink *string,
	doubanLink *string,
	tmdbLink *string,
	deps FetchRepairDeps,
) {
	if reviewData == nil {
		return
	}
	trimmedBody := strings.TrimSpace(reviewData.Body)
	introCompleteness := checkIntroBodyCompleteness(trimmedBody)
	if trimmedBody != "" && introCompleteness.IsComplete {
		logx.Infof(fetchRepairIntroLogModule, "简介正文校验通过 title=%s", torrentName)
		return
	}

	if trimmedBody == "" {
		logx.Warnf(fetchRepairIntroLogModule, "简介正文为空，开始自动补全 title=%s", torrentName)
		emitLog(deps, taskID, "修复简介", "简介正文为空，正在自动补全...", "processing")
	} else {
		missingCriticalFields := make([]string, 0, len(introCriticalFields))
		for _, field := range introCriticalFields {
			for _, missing := range introCompleteness.MissingFields {
				if field == missing {
					missingCriticalFields = append(missingCriticalFields, field)
					break
				}
			}
		}
		logx.Warnf(
			fetchRepairIntroLogModule,
			"简介正文缺少关键字段，开始自动补全 title=%s missing=%s",
			torrentName,
			strings.Join(missingCriticalFields, ","),
		)
		emitLog(
			deps,
			taskID,
			"修复简介",
			"简介正文缺少必填字段："+strings.Join(missingCriticalFields, "、")+"，正在自动补全...",
			"processing",
		)
	}

	introSource := map[string]any{
		"imdb_link":   strings.TrimSpace(stringValue(imdbLink)),
		"douban_link": strings.TrimSpace(stringValue(doubanLink)),
		"tmdb_link":   strings.TrimSpace(stringValue(tmdbLink)),
	}
	introResult, introErr := FetchMovieInfo("intro", torrentName, subtitle, introSource, strings.TrimSpace(deps.CSPTToken))
	if introErr != "" {
		logx.Warnf(fetchRepairIntroLogModule, "简介自动补全失败 title=%s err=%s", torrentName, introErr)
		emitLog(deps, taskID, "修复简介", "简介自动补全失败："+introErr, "warning")
		return
	}

	if strings.TrimSpace(introResult.Intro) != "" {
		reviewData.Body = strings.TrimSpace(introResult.Intro)
	}
	*imdbLink = firstNonEmpty(strings.TrimSpace(stringValue(imdbLink)), strings.TrimSpace(introResult.IMDb))
	*doubanLink = firstNonEmpty(strings.TrimSpace(stringValue(doubanLink)), strings.TrimSpace(introResult.Douban))
	*tmdbLink = firstNonEmpty(strings.TrimSpace(stringValue(tmdbLink)), strings.TrimSpace(introResult.TMDb))

	updatedBody := strings.TrimSpace(reviewData.Body)
	if updatedBody != "" {
		updatedCompleteness := checkIntroBodyCompleteness(updatedBody)
		if updatedCompleteness.IsComplete {
			logx.Infof(fetchRepairIntroLogModule, "简介自动补全成功 title=%s", torrentName)
			emitLog(deps, taskID, "修复简介", "简介自动补全成功", "success")
			return
		}
		missingCriticalFields := make([]string, 0, len(introCriticalFields))
		for _, field := range introCriticalFields {
			for _, missing := range updatedCompleteness.MissingFields {
				if field == missing {
					missingCriticalFields = append(missingCriticalFields, field)
					break
				}
			}
		}
		logx.Warnf(
			fetchRepairIntroLogModule,
			"简介自动补全后仍缺少关键字段 title=%s missing=%s",
			torrentName,
			strings.Join(missingCriticalFields, ","),
		)
		emitLog(
			deps,
			taskID,
			"修复简介",
			"简介补全后仍缺少必填字段："+strings.Join(missingCriticalFields, "、"),
			"warning",
		)
		return
	}

	logx.Warnf(fetchRepairIntroLogModule, "简介自动补全未产出内容 title=%s", torrentName)
	emitLog(deps, taskID, "修复简介", "简介补全未产出内容", "warning")
}

func repairScreenshotsDuringFetch(
	taskID string,
	savePath string,
	downloaderID string,
	torrentNameForPath string,
	contentName string,
	screenshotReviewMode string,
	reviewData *parser.ReviewExtractedData,
	deps FetchRepairDeps,
) (string, bool) {
	reviewStatus := processingshared.ScreenshotReviewStatusNone
	previewRequired := false
	mode := processingshared.NormalizeScreenshotReviewMode(screenshotReviewMode)
	if reviewData == nil {
		return reviewStatus, previewRequired
	}

	rawURLs := ExtractImageURLsFromText(reviewData.Screens)
	validateStartedAt := time.Now()
	logx.Infof(fetchRepairScreenshotLogModule, "截图并发校验开始 raw_count=%d workers=%d", len(rawURLs), fetchScreenshotValidateWorker)
	validURLs := filterReachableImageURLsConcurrently(rawURLs, fetchScreenshotValidateWorker)
	logx.Infof(fetchRepairScreenshotLogModule, "截图并发校验结束 raw_count=%d valid_count=%d elapsed_ms=%d", len(rawURLs), len(validURLs), time.Since(validateStartedAt).Milliseconds())

	configuredScreenshotCount := ScreenshotCountFromConfig(deps.RootConfig)
	_, isBDInfo, _ := processingmedia.ValidateMediaInfoFormat(reviewData.Mediainfo)
	hdrInfo := processingmedia.ExtractHDRInfoFromMediaText(reviewData.Mediainfo, isBDInfo)
	hasHDR := strings.TrimSpace(hdrInfo.StandardTag) != ""
	if len(validURLs) >= configuredScreenshotCount && hasHDR {
		reviewData.Screens = ToBBCodeImages(validURLs)
		logx.Infof(fetchRepairScreenshotLogModule, "截图校验通过 valid_count=%d required_count=%d hdr=%s", len(validURLs), configuredScreenshotCount, hdrInfo.StandardTag)
		emitLog(deps, taskID, "修复截图", "截图校验通过", "success")
		return reviewStatus, previewRequired
	}

	rebuildReason := "截图缺失或失效"
	if !hasHDR {
		rebuildReason = "种子不是 HDR"
	}
	logx.Warnf(
		fetchRepairScreenshotLogModule,
		"开始自动重建截图 raw_count=%d valid_count=%d required_count=%d hdr=%s reason=%s",
		len(rawURLs),
		len(validURLs),
		configuredScreenshotCount,
		hdrInfo.StandardTag,
		rebuildReason,
	)
	emitLog(deps, taskID, "修复截图", rebuildReason+"，正在自动重建...", "processing")

	payload := map[string]any{
		"save_path":      strings.TrimSpace(savePath),
		"downloader_id":  strings.TrimSpace(downloaderID),
		"torrent_name":   strings.TrimSpace(torrentNameForPath),
		"content_name":   strings.TrimSpace(contentName),
		"current_images": reviewData.Screens,
	}
	sourceInfo := map[string]any{"main_title": strings.TrimSpace(contentName)}
	usePendingReview, previewDecisionErr := ShouldUseScreenshotPreview(ScreenshotGenerateInput{
		Payload:     payload,
		SourceInfo:  sourceInfo,
		ContentName: contentName,
		RootConfig:  deps.RootConfig,
	})
	if previewDecisionErr != nil {
		logx.Warnf(fetchRepairScreenshotLogModule, "截图字幕流探测失败，继续按自动重建处理 task_id=%s err=%v", taskID, previewDecisionErr)
	} else if usePendingReview {
		reviewStatus = processingshared.ScreenshotReviewStatusPending
		if mode == processingshared.ScreenshotReviewModeInteractive {
			previewRequired = true
			if len(validURLs) > 0 {
				reviewData.Screens = ToBBCodeImages(validURLs)
			} else {
				reviewData.Screens = ""
			}
			logx.Warnf(fetchRepairScreenshotLogModule, "字幕未被明确识别为中文字幕，单条抓取改走候选截图选择 task_id=%s", taskID)
			emitLog(deps, taskID, "修复截图", "字幕未被明确识别为中文字幕，等待前端候选截图选择", "warning")
			return reviewStatus, previewRequired
		}
		logx.Warnf(fetchRepairScreenshotLogModule, "字幕未被明确识别为中文字幕，本次截图将标记为待人工确认 task_id=%s", taskID)
		emitLog(deps, taskID, "修复截图", "字幕未被明确识别为中文字幕，将自动生成截图并标记为待人工确认", "warning")
	}
	generatedURLs, err := GenerateAndUploadScreenshots(ScreenshotGenerateInput{
		Payload:     payload,
		SourceInfo:  sourceInfo,
		ContentName: contentName,
		RootConfig:  deps.RootConfig,
	})
	if err == nil && len(generatedURLs) > 0 {
		reviewData.Screens = ToBBCodeImages(generatedURLs)
		logx.Infof(fetchRepairScreenshotLogModule, "截图自动重建成功 generated_count=%d", len(generatedURLs))
		if processingshared.NeedsScreenshotManualReview(reviewStatus) {
			emitLog(deps, taskID, "修复截图", "截图自动重建成功，请在审查时人工确认字幕时间点", "warning")
		} else {
			emitLog(deps, taskID, "修复截图", "截图自动重建成功", "success")
		}
		return reviewStatus, previewRequired
	}

	if len(validURLs) > 0 {
		reviewData.Screens = ToBBCodeImages(validURLs)
		logx.Warnf(fetchRepairScreenshotLogModule, "截图自动重建失败，回退保留可用截图 valid_count=%d err=%v", len(validURLs), err)
		emitLog(deps, taskID, "修复截图", "截图重建失败，已回退保留可用截图", "warning")
		return processingshared.ScreenshotReviewStatusNone, false
	}

	logx.Warnf(fetchRepairScreenshotLogModule, "截图自动重建失败且无可用回退截图 err=%v", err)
	emitLog(deps, taskID, "修复截图", "截图自动重建失败，未获得可用截图", "warning")
	return processingshared.ScreenshotReviewStatusNone, false
}

func filterReachableImageURLsConcurrently(urls []string, workers int) []string {
	trimmedURLs := make([]string, 0, len(urls))
	for _, url := range urls {
		trimmed := strings.TrimSpace(url)
		if trimmed == "" {
			continue
		}
		trimmedURLs = append(trimmedURLs, trimmed)
	}
	if len(trimmedURLs) == 0 {
		return []string{}
	}
	if workers <= 0 {
		workers = 1
	}
	if workers > len(trimmedURLs) {
		workers = len(trimmedURLs)
	}

	jobs := make(chan screenshotValidateJob, len(trimmedURLs))
	results := make(chan screenshotValidateResult, len(trimmedURLs))

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for job := range jobs {
				results <- screenshotValidateResult{
					index: job.index,
					ok:    IsImageURLReachable(job.url),
				}
			}
		}()
	}

	for idx, url := range trimmedURLs {
		jobs <- screenshotValidateJob{index: idx, url: url}
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	okByIndex := make([]bool, len(trimmedURLs))
	for res := range results {
		if res.index >= 0 && res.index < len(okByIndex) {
			okByIndex[res.index] = res.ok
		}
	}

	reachable := make([]string, 0, len(trimmedURLs))
	for idx, ok := range okByIndex {
		if !ok {
			continue
		}
		reachable = appendUniqueURL(reachable, trimmedURLs[idx])
	}
	return reachable
}

func emitLog(deps FetchRepairDeps, taskID, step, message, status string) {
	if deps.EmitLog == nil {
		return
	}
	deps.EmitLog(taskID, step, message, status)
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func appendUniqueURL(items []string, value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return items
	}
	for _, item := range items {
		if strings.EqualFold(item, trimmed) {
			return items
		}
	}
	return append(items, trimmed)
}

func stringValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
