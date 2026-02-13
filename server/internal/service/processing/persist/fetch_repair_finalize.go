package persist

import (
	"errors"
	"strings"
	"time"

	parser "github.com/pt-nexus/server-go/internal/service/acquire/extract"
	processingrepair "github.com/pt-nexus/server-go/internal/service/processing/repair"
)

// FetchRepairFinalizeInput 定义“抓取修复 + 草稿收敛”组合流程输入。
type FetchRepairFinalizeInput struct {
	TaskID             string
	SavePath           string
	DownloaderID       string
	TorrentNameForPath string
	RootConfig         map[string]any
	ReviewData         parser.ReviewExtractedData
	DetailHTML         string

	Draft          *SeedDraft
	MetaName       string
	SiteIdentifier string
}

// FetchRepairFinalizeDeps 定义“抓取修复 + 草稿收敛”组合流程依赖。
type FetchRepairFinalizeDeps struct {
	FetchRepairDeps processingrepair.FetchRepairDeps
	Now             time.Time

	BuildSimpleTitleComponents func(title string, releaseGroup string, mediaInfo string) []map[string]any
}

// FetchRepairFinalizeResult 定义“抓取修复 + 草稿收敛”组合流程输出。
type FetchRepairFinalizeResult struct {
	RepairResult   processingrepair.ParallelFetchRepairResult
	FinalizeResult FinalizeFetchedSeedResult
}

// RunFetchRepairAndFinalize 执行抓取修复并将草稿收敛为可入库记录。
// 参数/返回：输入为草稿与修复所需上下文，依赖包含修复回调与标题组件构建函数；返回修复结果与最终收敛结果。
// 失败场景：Draft 为空或最终收敛失败时返回 error。
// 副作用：会发起网络请求/媒体分析（由 repair 依赖触发），并原地修改 Draft。
func RunFetchRepairAndFinalize(input FetchRepairFinalizeInput, deps FetchRepairFinalizeDeps) (FetchRepairFinalizeResult, error) {
	if input.Draft == nil {
		return FetchRepairFinalizeResult{}, errors.New("seed draft is nil")
	}

	input.Draft.ApplyReviewExtract(input.ReviewData, input.DetailHTML)
	torrentName := strings.TrimSpace(input.Draft.Title)
	subtitle := strings.TrimSpace(input.Draft.Subtitle)
	imdbLink := strings.TrimSpace(input.Draft.IMDbLink)
	doubanLink := strings.TrimSpace(input.Draft.DoubanLink)
	tmdbLink := strings.TrimSpace(input.Draft.TMDbLink)

	repairResult := processingrepair.RunParallelFetchRepairs(
		processingrepair.ParallelFetchRepairInput{
			TaskID:             input.TaskID,
			SavePath:           input.SavePath,
			DownloaderID:       input.DownloaderID,
			TorrentNameForPath: input.TorrentNameForPath,
			TorrentName:        torrentName,
			Subtitle:           subtitle,
			ReviewData:         input.ReviewData,
			IMDbLink:           imdbLink,
			DoubanLink:         doubanLink,
			TMDbLink:           tmdbLink,
		},
		deps.FetchRepairDeps,
	)
	input.Draft.ApplyRepairResult(repairResult)

	finalizeResult, err := FinalizeFetchedSeed(FinalizeFetchedSeedInput{
		Draft:                      input.Draft,
		MetaName:                   input.MetaName,
		SiteIdentifier:             input.SiteIdentifier,
		SavePath:                   input.SavePath,
		DownloaderID:               input.DownloaderID,
		TorrentNameForPath:         input.TorrentNameForPath,
		RootConfig:                 input.RootConfig,
		Now:                        deps.Now,
		BuildSimpleTitleComponents: deps.BuildSimpleTitleComponents,
	})
	if err != nil {
		return FetchRepairFinalizeResult{}, err
	}

	return FetchRepairFinalizeResult{
		RepairResult:   repairResult,
		FinalizeResult: finalizeResult,
	}, nil
}
