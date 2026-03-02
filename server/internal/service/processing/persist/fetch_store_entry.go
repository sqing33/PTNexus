package persist

import (
	"time"

	extract "github.com/pt-nexus/server/internal/service/acquire/extract"
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
	processingtitle "github.com/pt-nexus/server/internal/service/processing/title"
)

// FetchAndStoreEntryRepo 定义 FetchAndStore 编排流程所需仓储接口。
type FetchAndStoreEntryRepo interface {
	acquirefetch.FetchAcquireRepo
	FetchPersistPipelineRepo
}

// FetchAndStoreEntryInput 定义 FetchAndStore 编排流程输入。
type FetchAndStoreEntryInput struct {
	SourceSite   string
	SearchTerm   string
	TorrentName  string
	SavePath     string
	DownloaderID string
	TaskID       string
}

// FetchAndStoreEntryDeps 定义 FetchAndStore 编排流程依赖。
type FetchAndStoreEntryDeps struct {
	Repo            FetchAndStoreEntryRepo
	ExtractorEngine *extract.Engine

	EmitLog func(step, message, status string)

	FetchRepairDeps processingrepair.FetchRepairDeps

	TriggerMediainfoRepair func(input processingrepair.TriggerMediainfoRepairInput)
	RecomputeTags          func(hash, torrentID, siteName, savePath, torrentNameForPath, reason string)

	FetchRepairModule string
	TagMappingModule  string
	TagCompleteModule string

	Now func() time.Time

	NewContextID         func() string
	RegisterFetchContext func(contextID string, result FetchStoreProcessResult)
}

// FetchAndStoreEntryResult 定义 FetchAndStore 编排流程输出。
type FetchAndStoreEntryResult struct {
	ProcessResult FetchStoreProcessResult
	ContextID     string
}

// ExecuteFetchAndStoreEntry 执行 FetchAndStore 主流程（获取 -> 提取修复入库 -> 上下文注册）。
// 参数/返回：input 为抓取请求参数；deps 注入仓储、提取器与修复依赖；返回执行结果、状态码、是否获取阶段错误。
// 失败场景：获取阶段失败时返回获取状态码；处理入库失败返回 500。
// 副作用：会发起网络请求、触发修复流程、写入数据库并注册发布上下文。
func ExecuteFetchAndStoreEntry(input FetchAndStoreEntryInput, deps FetchAndStoreEntryDeps) (FetchAndStoreEntryResult, int, bool, error) {
	acquireResult, acquireStatus, acquireErr := acquirefetch.AcquireSeedForFetch(
		acquirefetch.FetchAcquireInput{
			SourceSite:   input.SourceSite,
			SearchTerm:   input.SearchTerm,
			TorrentName:  input.TorrentName,
			SavePath:     input.SavePath,
			DownloaderID: input.DownloaderID,
			TaskID:       input.TaskID,
		},
		acquirefetch.FetchAcquireDeps{
			Repo:    deps.Repo,
			EmitLog: deps.EmitLog,
		},
	)
	if acquireErr != nil {
		return FetchAndStoreEntryResult{}, acquireStatus, true, acquireErr
	}

	if deps.EmitLog != nil {
		deps.EmitLog("抓取修复", "正在并发校验并纠偏抓取结果...", "processing")
	}

	nowFn := deps.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	processResult, processErr := RunFetchStoreProcess(
		FetchStoreProcessInput{
			TaskID:          input.TaskID,
			SourceSite:      input.SourceSite,
			SearchTerm:      input.SearchTerm,
			AcquireResult:   acquireResult,
			ExtractorEngine: deps.ExtractorEngine,
		},
		FetchStoreProcessDeps{
			Repo:                       deps.Repo,
			EmitLog:                    deps.EmitLog,
			FetchRepairDeps:            deps.FetchRepairDeps,
			BuildSimpleTitleComponents: processingtitle.BuildSimpleTitleComponentsWithMediaInfo,
			TriggerMediainfoRepair:     deps.TriggerMediainfoRepair,
			RecomputeTags:              deps.RecomputeTags,
			FetchRepairModule:          deps.FetchRepairModule,
			TagMappingModule:           deps.TagMappingModule,
			TagCompleteModule:          deps.TagCompleteModule,
			Now:                        nowFn(),
		},
	)
	if processErr != nil {
		return FetchAndStoreEntryResult{ProcessResult: processResult}, 500, false, processErr
	}

	contextID := ""
	if deps.NewContextID != nil {
		contextID = deps.NewContextID()
	}
	if contextID != "" && deps.RegisterFetchContext != nil {
		deps.RegisterFetchContext(contextID, processResult)
	}

	return FetchAndStoreEntryResult{
		ProcessResult: processResult,
		ContextID:     contextID,
	}, 200, false, nil
}
