package persist

import (
	"errors"
	"time"

	parser "github.com/pt-nexus/server-go/internal/service/acquire/extract"
	processingrepair "github.com/pt-nexus/server-go/internal/service/processing/repair"
)

// FetchFinalizeFlowInput 定义抓取后“修复+入库+收敛日志”流程输入。
type FetchFinalizeFlowInput struct {
	TaskID             string
	SourceSite         string
	SearchTerm         string
	Hash               string
	TorrentID          string
	SiteIdentifier     string
	SavePath           string
	DownloaderID       string
	TorrentNameForPath string
	MetaName           string
	DetailHTML         string
	ReviewData         parser.ReviewExtractedData
	Draft              *SeedDraft
}

// FetchFinalizeFlowDeps 定义抓取后“修复+入库+收敛日志”流程依赖。
type FetchFinalizeFlowDeps struct {
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

// FetchFinalizeFlowResult 定义抓取后“修复+入库+收敛日志”流程输出。
type FetchFinalizeFlowResult struct {
	PipelineResult FetchPersistPipelineResult
	LoggingResult  FetchFinalizeLoggingResult
}

// RunFetchFinalizeFlow 执行抓取后完整处理链路：修复、写库、入库后收敛、统一日志。
// 参数/返回：input 注入抓取结果上下文；deps 注入仓储与回调；返回流水线结果与日志关键状态。
// 失败场景：草稿为空、仓储缺失、流水线执行失败时返回 error。
// 副作用：可能触发修复任务、写入数据库、触发媒体刷新与标签重算。
func RunFetchFinalizeFlow(input FetchFinalizeFlowInput, deps FetchFinalizeFlowDeps) (FetchFinalizeFlowResult, error) {
	if input.Draft == nil {
		return FetchFinalizeFlowResult{}, errors.New("seed draft is nil")
	}
	if deps.Repo == nil {
		return FetchFinalizeFlowResult{}, errors.New("repo is nil")
	}

	pipelineResult, pipelineErr := RunFetchPersistPipeline(
		FetchPersistPipelineInput{
			TaskID:             input.TaskID,
			Hash:               input.Hash,
			TorrentID:          input.TorrentID,
			SiteIdentifier:     input.SiteIdentifier,
			SavePath:           input.SavePath,
			DownloaderID:       input.DownloaderID,
			TorrentNameForPath: input.TorrentNameForPath,
			MetaName:           input.MetaName,
			DetailHTML:         input.DetailHTML,
			ReviewData:         input.ReviewData,
			Draft:              input.Draft,
		},
		FetchPersistPipelineDeps{
			Repo:                       deps.Repo,
			EmitLog:                    deps.EmitLog,
			FetchRepairDeps:            deps.FetchRepairDeps,
			BuildSimpleTitleComponents: deps.BuildSimpleTitleComponents,
			Now:                        deps.Now,
			TriggerMediainfoRepair:     deps.TriggerMediainfoRepair,
			RecomputeTags:              deps.RecomputeTags,
		},
	)
	if pipelineErr != nil {
		return FetchFinalizeFlowResult{}, pipelineErr
	}

	loggingResult := LogFetchFinalizeResult(FetchFinalizeLoggingInput{
		SourceSite:        input.SourceSite,
		SearchTerm:        input.SearchTerm,
		SiteIdentifier:    input.SiteIdentifier,
		FinalizeResult:    pipelineResult.RepairFinalizeResult.FinalizeResult,
		Draft:             input.Draft,
		FetchRepairModule: deps.FetchRepairModule,
		TagMappingModule:  deps.TagMappingModule,
		TagCompleteModule: deps.TagCompleteModule,
	})

	return FetchFinalizeFlowResult{
		PipelineResult: pipelineResult,
		LoggingResult:  loggingResult,
	}, nil
}
