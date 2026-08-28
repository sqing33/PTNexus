package persist

import (
	"errors"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/repository"
	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
)

// FetchRepairFinalizeInput 定义“抓取修复 + 草稿收敛”组合流程输入。
type FetchRepairFinalizeInput struct {
	TaskID               string
	SourceSite           string
	SavePath             string
	DownloaderID         string
	TorrentNameForPath   string
	ScreenshotReviewMode string
	RootConfig           map[string]any
	ReviewData           parser.ReviewExtractedData
	DetailHTML           string

	Draft          *SeedDraft
	MetaName       string
	SiteIdentifier string
}

// FetchRepairFinalizeDeps 定义“抓取修复 + 草稿收敛”组合流程依赖。
type FetchRepairFinalizeDeps struct {
	FetchRepairDeps processingrepair.FetchRepairDeps
	Now             time.Time

	BuildSimpleTitleComponents func(title string, releaseGroup string, mediaInfo string) []map[string]any

	// ResourceStore 为资源信息库仓储能力（可为 nil，nil 时跳过资源信息匹配/入库）。
	ResourceStore ResourceInfoStore
}

// FetchRepairFinalizeResult 定义“抓取修复 + 草稿收敛”组合流程输出。
type FetchRepairFinalizeResult struct {
	RepairResult          processingrepair.ParallelFetchRepairResult
	FinalizeResult        FinalizeFetchedSeedResult
	RestrictionPrecheck   FetchRestrictionPrecheckResult
	SkippedRepairByTagHit bool
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
	restrictionPrecheck := DetectFetchRestrictionPrecheck(
		input.Draft,
		input.SiteIdentifier,
		input.SavePath,
		input.TorrentNameForPath,
		input.DownloaderID,
		input.RootConfig,
	)

	// 资源信息库匹配（豆瓣ID > IMDbID > TMDbID）：命中时复用库内海报/简介并跳过对应修复。
	var matchedResource *repository.ResourceInfo
	skipPosterRepair, skipIntroRepair := false, false
	if deps.ResourceStore != nil {
		matchedResource = FindResourceInfoForDraft(deps.ResourceStore, input.Draft)
		skipPosterRepair, skipIntroRepair = ResourceInfoRepairSkips(matchedResource)
	}

	torrentName := strings.TrimSpace(input.Draft.Title)
	subtitle := strings.TrimSpace(input.Draft.Subtitle)
	imdbLink := strings.TrimSpace(input.Draft.IMDbLink)
	doubanLink := strings.TrimSpace(input.Draft.DoubanLink)
	tmdbLink := strings.TrimSpace(input.Draft.TMDbLink)

	repairResult := processingrepair.ParallelFetchRepairResult{
		ReviewData:             input.ReviewData,
		IMDbLink:               imdbLink,
		DoubanLink:             doubanLink,
		TMDbLink:               tmdbLink,
		ScreenshotReviewStatus: "none",
	}
	if restrictionPrecheck.Matched {
		if deps.FetchRepairDeps.EmitLog != nil {
			deps.FetchRepairDeps.EmitLog(
				input.TaskID,
				"标签预检",
				"检测到受限标签，已跳过海报/简介/截图修复: "+strings.Join(restrictionPrecheck.RestrictedTags, ", "),
				"warning",
			)
		}
	} else {
		repairResult = processingrepair.RunParallelFetchRepairs(
			processingrepair.ParallelFetchRepairInput{
				TaskID:               input.TaskID,
				SourceSite:           firstNonEmpty(input.SourceSite, input.SiteIdentifier),
				SavePath:             input.SavePath,
				DownloaderID:         input.DownloaderID,
				TorrentNameForPath:   input.TorrentNameForPath,
				TorrentName:          torrentName,
				ScreenshotReviewMode: input.ScreenshotReviewMode,
				Subtitle:             subtitle,
				ReviewData:           input.ReviewData,
				IMDbLink:             imdbLink,
				DoubanLink:           doubanLink,
				TMDbLink:             tmdbLink,
				SkipPosterRepair:     skipPosterRepair,
				SkipIntroRepair:      skipIntroRepair,
			},
			deps.FetchRepairDeps,
		)
		input.Draft.ApplyRepairResult(repairResult)
	}

	// 命中资源信息库时覆盖发布数据（标题/产地/海报/简介）；未命中时保存本次解析结果以便复用。
	if deps.ResourceStore != nil {
		if matchedResource != nil {
			ApplyResourceInfoToDraft(input.Draft, matchedResource)
		} else {
			SaveResourceInfoFromDraft(deps.ResourceStore, input.Draft)
		}
	}

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
		RepairResult:          repairResult,
		FinalizeResult:        finalizeResult,
		RestrictionPrecheck:   restrictionPrecheck,
		SkippedRepairByTagHit: restrictionPrecheck.Matched,
	}, nil
}
