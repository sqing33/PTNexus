package persist

import (
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	extract "github.com/pt-nexus/server/internal/service/acquire/extract"
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
)

const iyuuPlaceholderLogModule = "迁移-IYUU占位回写"

// FetchStoreProcessInput 定义抓取保存流程中段（提取+收敛）输入。
type FetchStoreProcessInput struct {
	TaskID               string
	SourceSite           string
	SearchTerm           string
	ScreenshotReviewMode string
	AcquireResult        acquirefetch.FetchAcquireResult
	ExtractorEngine      *extract.Engine
}

// FetchStoreProcessDeps 定义抓取保存流程中段（提取+收敛）依赖。
type FetchStoreProcessDeps struct {
	Repo FetchPersistPipelineRepo

	EmitLog                    func(step, message, status string)
	FetchRepairDeps            processingrepair.FetchRepairDeps
	BuildSimpleTitleComponents func(title string, releaseGroup string, mediaInfo string) []map[string]any

	TriggerMediainfoRepair func(input processingrepair.TriggerMediainfoRepairInput)
	RecomputeTags          func(hash, torrentID, siteName, savePath, torrentNameForPath, reason string)

	FetchRepairModule string
	TagMappingModule  string
	TagCompleteModule string

	Now time.Time
}

// FetchStoreProcessResult 定义抓取保存流程中段（提取+收敛）输出。
type FetchStoreProcessResult struct {
	Meta               acquirefetch.TorrentMeta
	TorrentPath        string
	DetailURL          string
	SiteIdentifier     string
	Nickname           string
	TorrentName        string
	TorrentNameForPath string
	SavePath           string
	DownloaderID       string

	Draft          *SeedDraft
	ExtractMeta    extract.Meta
	PipelineResult FetchPersistPipelineResult
	LoggingResult  FetchFinalizeLoggingResult
}

// RunFetchStoreProcess 执行 FetchAndStore 的中段流程：提取器解析、Draft 构建、修复收敛与入库流水线。
// 参数/返回：input 包含获取阶段结果；deps 注入仓储与修复依赖；返回中段流程产物。
// 失败场景：仓储缺失、流水线失败时返回 error。
// 副作用：会执行提取逻辑、修复流程并写入 seed_parameters。
func RunFetchStoreProcess(input FetchStoreProcessInput, deps FetchStoreProcessDeps) (FetchStoreProcessResult, error) {
	acquireResult := input.AcquireResult
	meta := acquireResult.Meta
	torrentPath := acquireResult.TorrentPath
	detailURL := acquireResult.DetailURL
	siteIdentifier := acquireResult.SiteIdentifier
	nickname := acquireResult.Nickname
	torrentName := acquireResult.TorrentName
	torrentNameForPath := acquireResult.TorrentNameForPath
	savePath := acquireResult.SavePath
	downloaderID := acquireResult.DownloaderID
	html := acquireResult.DetailHTML

	reviewExtract := extract.ExtractReviewData(input.ExtractorEngine, extract.Input{
		SiteCode:      siteIdentifier,
		SiteNickname:  nickname,
		BaseURL:       strings.TrimSpace(toStringAny(acquireResult.SourceInfo["base_url"], "")),
		Cookie:        strings.TrimSpace(toStringAny(acquireResult.SourceInfo["cookie"], "")),
		TorrentID:     input.SearchTerm,
		PageHTML:      html,
		FallbackTitle: strings.TrimSpace(torrentName),
	})
	reviewData := reviewExtract.ReviewData
	extractMeta := reviewExtract.ExtractMeta
	seedData := reviewExtract.SeedData

	{
		snapshot := buildExtractSnapshotLog(
			input.TaskID,
			input.SourceSite,
			input.SearchTerm,
			siteIdentifier,
			extractMeta,
			seedData,
			reviewData,
		)
		logx.PlainModuleInfof("迁移-提取快照", "字段", "%s", snapshot)
	}

	draft := NewSeedDraft(meta.InfoHash, input.SearchTerm, siteIdentifier, nickname)
	draft.Title = strings.TrimSpace(torrentName)

	finalizeResult, finalizeErr := RunFetchFinalizeFlow(
		FetchFinalizeFlowInput{
			TaskID:               input.TaskID,
			SourceSite:           input.SourceSite,
			SearchTerm:           input.SearchTerm,
			Hash:                 meta.InfoHash,
			TorrentID:            input.SearchTerm,
			SiteIdentifier:       siteIdentifier,
			SavePath:             savePath,
			DownloaderID:         downloaderID,
			TorrentNameForPath:   torrentNameForPath,
			ScreenshotReviewMode: input.ScreenshotReviewMode,
			MetaName:             meta.Name,
			DetailHTML:           html,
			ReviewData:           reviewData,
			Draft:                draft,
		},
		FetchFinalizeFlowDeps{
			Repo:                       deps.Repo,
			EmitLog:                    deps.EmitLog,
			FetchRepairDeps:            deps.FetchRepairDeps,
			BuildSimpleTitleComponents: deps.BuildSimpleTitleComponents,
			TriggerMediainfoRepair:     deps.TriggerMediainfoRepair,
			RecomputeTags:              deps.RecomputeTags,
			FetchRepairModule:          deps.FetchRepairModule,
			TagMappingModule:           deps.TagMappingModule,
			TagCompleteModule:          deps.TagCompleteModule,
			Now:                        deps.Now,
		},
	)
	if finalizeErr != nil {
		return FetchStoreProcessResult{
			Meta:               meta,
			TorrentPath:        torrentPath,
			DetailURL:          detailURL,
			SiteIdentifier:     siteIdentifier,
			Nickname:           nickname,
			TorrentName:        torrentName,
			TorrentNameForPath: torrentNameForPath,
			SavePath:           savePath,
			DownloaderID:       downloaderID,
			Draft:              draft,
			ExtractMeta:        extractMeta,
		}, finalizeErr
	}

	if replacer, ok := any(deps.Repo).(interface {
		ReplaceUnseededPlaceholderHash(name string, size int64, downloaderID string, siteNickname string, newHash string) (string, bool, error)
	}); ok {
		oldHash, updated, err := replacer.ReplaceUnseededPlaceholderHash(
			torrentName,
			meta.Size,
			downloaderID,
			nickname,
			meta.InfoHash,
		)
		if err != nil {
			logx.Warnf(iyuuPlaceholderLogModule, "回写占位 hash 失败 name=%s size=%d site=%s downloader_id=%s old_hash=%s new_hash=%s err=%v", torrentName, meta.Size, nickname, downloaderID, oldHash, meta.InfoHash, err)
		} else if updated {
			logx.Infof(iyuuPlaceholderLogModule, "回写占位 hash 成功 name=%s size=%d site=%s downloader_id=%s old_hash=%s new_hash=%s", torrentName, meta.Size, nickname, downloaderID, oldHash, meta.InfoHash)
		}
	}

	return FetchStoreProcessResult{
		Meta:               meta,
		TorrentPath:        torrentPath,
		DetailURL:          detailURL,
		SiteIdentifier:     siteIdentifier,
		Nickname:           nickname,
		TorrentName:        torrentName,
		TorrentNameForPath: torrentNameForPath,
		SavePath:           savePath,
		DownloaderID:       downloaderID,
		Draft:              draft,
		ExtractMeta:        extractMeta,
		PipelineResult:     finalizeResult.PipelineResult,
		LoggingResult:      finalizeResult.LoggingResult,
	}, nil
}
