package migrationflow

import (
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
)

const (
	fetchStoreLogModule    = "迁移-源站抓取"
	fetchRepairLogModule   = "迁移-抓取修复"
	tagCompletionLogModule = "迁移-标签补全"
	tagMappingLogModule    = "迁移-标签映射"
)

// FetchAndStore 从源站点抓取种子详情，并将解析后的参数写入 seed_parameters。
// 参数/返回：payload 由前端传入（sourceSite/searchTerm 等），返回写入结果与 HTTP 状态码。
// 失败场景：源站点校验失败、下载种子失败、解析失败、写库失败等会返回错误状态码与 message。
// 副作用：会发起网络请求、写入临时 torrent 文件、写入/覆盖 seed_parameters，并可能触发 MediaInfo/BDInfo 的异步修复。
func (s *MigrateService) FetchAndStore(payload map[string]any) (map[string]any, int) {
	sourceSite := strings.TrimSpace(processingshared.ToString(payload["sourceSite"], ""))
	searchTerm := strings.TrimSpace(processingshared.ToString(payload["searchTerm"], ""))
	torrentName := strings.TrimSpace(processingshared.ToString(payload["torrentName"], ""))
	savePath := strings.TrimSpace(processingshared.ToString(payload["savePath"], ""))
	downloaderID := strings.TrimSpace(processingshared.ToString(payload["downloaderId"], ""))
	screenshotReviewMode := processingshared.NormalizeScreenshotReviewMode(
		processingshared.ToString(payload["screenshotReviewMode"], processingshared.ToString(payload["screenshot_review_mode"], processingshared.ScreenshotReviewModeBackground)),
	)
	action := "开始抓取"
	// sourceSite 在不同入口可能是站点 code（ssd）或昵称（不可说），这里两者都尝试匹配。
	if s != nil && s.extractorEngine != nil {
		if _, ok := s.extractorEngine.SpecialExtractorName(sourceSite, sourceSite); ok {
			action = "使用特殊提取器抓取"
		}
	}
	logx.Infof(fetchStoreLogModule, "%s source_site=%s search_term=%s", action, sourceSite, searchTerm)

	if sourceSite == "" || searchTerm == "" {
		logx.Warnf(fetchStoreLogModule, "参数校验失败 source_site=%s search_term=%s", sourceSite, searchTerm)
		return map[string]any{"success": false, "message": "错误：源站点和搜索词不能为空。"}, 400
	}

	taskID := strings.TrimSpace(processingshared.ToString(payload["task_id"], ""))
	if taskID == "" {
		taskID = s.newID("log")
	}
	logx.Infof(fetchStoreLogModule, "抓取任务初始化 source_site=%s search_term=%s task_id=%s", sourceSite, searchTerm, taskID)
	s.ensureLogStream(taskID)
	s.emitLog(taskID, "开始抓取", "正在从源站点抓取种子信息...", "processing")

	rootConfig := map[string]any{}
	if s != nil && s.cfg != nil {
		rootConfig = s.cfg.Get()
	}
	fetchRepairDeps := processingrepair.FetchRepairDeps{
		EmitLog:          s.emitLog,
		RefreshMediainfo: s.refreshMediainfoAsync,
		CSPTToken:        s.csptToken(),
		RootConfig:       rootConfig,
	}

	entryResult, statusCode, isAcquireError, runErr := processingpersist.ExecuteFetchAndStoreEntry(
		processingpersist.FetchAndStoreEntryInput{
			SourceSite:           sourceSite,
			SearchTerm:           searchTerm,
			TorrentName:          torrentName,
			SavePath:             savePath,
			DownloaderID:         downloaderID,
			TaskID:               taskID,
			ScreenshotReviewMode: screenshotReviewMode,
		},
		processingpersist.FetchAndStoreEntryDeps{
			Repo:            s.repo,
			ExtractorEngine: s.extractorEngine,
			EmitLog: func(step, message, status string) {
				s.emitLog(taskID, step, message, status)
			},
			FetchRepairDeps: fetchRepairDeps,
			TriggerMediainfoRepair: func(input processingrepair.TriggerMediainfoRepairInput) {
				processingrepair.TriggerMediainfoRepairDuringFetch(input, fetchRepairDeps)
			},
			RecomputeTags: func(hash, torrentID, siteName, savePath, torrentNameForPath, reason string) {
				s.recomputeAndPersistTags(hash, torrentID, siteName, savePath, torrentNameForPath, reason)
			},
			FetchRepairModule: fetchRepairLogModule,
			TagMappingModule:  tagMappingLogModule,
			TagCompleteModule: tagCompletionLogModule,
			Now:               time.Now,
			NewContextID: func() string {
				return s.newID("ctx")
			},
			RegisterFetchContext: func(contextID string, result processingpersist.FetchStoreProcessResult) {
				publishworkflow.RegisterFetchContext(
					s.contextState,
					contextID,
					searchTerm,
					result.SiteIdentifier,
					result.Meta.InfoHash,
					strings.TrimSpace(result.Draft.Title),
					result.SavePath,
					result.DownloaderID,
					result.Nickname,
					result.DetailURL,
					result.TorrentPath,
				)
			},
		},
	)
	return processingpersist.BuildFetchAndStoreOutcome(
		processingpersist.FetchAndStoreOutcomeInput{
			SourceSite:        sourceSite,
			SearchTerm:        searchTerm,
			TaskID:            taskID,
			EntryResult:       entryResult,
			StatusCode:        statusCode,
			IsAcquireError:    isAcquireError,
			RunErr:            runErr,
			FetchStoreModule:  fetchStoreLogModule,
			FetchRepairModule: fetchRepairLogModule,
		},
		processingpersist.FetchAndStoreOutcomeDeps{
			EmitLog: func(step, message, status string) {
				s.emitLog(taskID, step, message, status)
			},
			CloseLog: func() {
				s.closeLogStream(taskID)
			},
			BuildSuccessResponse: publishworkflow.BuildFetchStoreSuccessResponse,
		},
	)
}
