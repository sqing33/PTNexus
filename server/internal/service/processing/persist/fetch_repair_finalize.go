package persist

import (
	"errors"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
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
			},
			deps.FetchRepairDeps,
		)
		input.Draft.ApplyRepairResult(repairResult)
	}

	// 资源信息库匹配：先完成数据获取（含 PTGen 补全的豆瓣/IMDb/TMDb ID），再按
	// 豆瓣ID > IMDbID > TMDbID 优先级查库；命中则把海报、简介（正文）与视频截图替换为
	// 库内数据，未命中则把本次抓取到的资源信息（含截图）入库以便后续复用。
	if deps.ResourceStore != nil {
		if matchedResource := FindResourceInfoForDraft(deps.ResourceStore, input.Draft); matchedResource != nil {
			ApplyResourceInfoToDraft(input.Draft, matchedResource)
			// 命中资源信息库但库内截图缺失时，用本次抓取到的截图回补该记录的空缺字段，
			// 避免同一资源每次都重复生成截图而不入库。
			if strings.TrimSpace(matchedResource.Screenshots) == "" && strings.TrimSpace(input.Draft.Screenshots) != "" {
				if err := deps.ResourceStore.UpsertResourceInfo(&repository.ResourceInfo{
					DoubanID:    matchedResource.DoubanID,
					ImdbID:      matchedResource.ImdbID,
					TmdbID:      matchedResource.TmdbID,
					Screenshots: strings.TrimSpace(input.Draft.Screenshots),
				}); err != nil {
					logx.Warnf(resourceInfoLogModule, "回补资源信息截图失败 err=%v", err)
				}
			}
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
