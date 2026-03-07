package persist

import (
	"errors"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
	processingack "github.com/pt-nexus/server/internal/service/processing/acknowledgment"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
)

const teamAckLogModule = "迁移-官组致谢"

// FetchPersistPipelineRepo 定义抓取流水线落库所需仓储接口。
type FetchPersistPipelineRepo interface {
	FetchPostPersistRepo
	UpsertSeedParameter(record map[string]any) error
	ListSitesGroupAndDescription() ([]map[string]any, error)
}

// FetchPersistPipelineInput 定义抓取流水线输入。
type FetchPersistPipelineInput struct {
	TaskID               string
	Hash                 string
	TorrentID            string
	SiteIdentifier       string
	SavePath             string
	DownloaderID         string
	TorrentNameForPath   string
	ScreenshotReviewMode string
	MetaName             string
	DetailHTML           string
	ReviewData           parser.ReviewExtractedData
	Draft                *SeedDraft
}

// FetchPersistPipelineDeps 定义抓取流水线依赖。
type FetchPersistPipelineDeps struct {
	Repo FetchPersistPipelineRepo

	EmitLog                    func(step, message, status string)
	FetchRepairDeps            processingrepair.FetchRepairDeps
	BuildSimpleTitleComponents func(title string, releaseGroup string, mediaInfo string) []map[string]any
	Now                        time.Time

	TriggerMediainfoRepair func(input processingrepair.TriggerMediainfoRepairInput)
	RecomputeTags          func(hash, torrentID, siteName, savePath, torrentNameForPath, reason string)
}

// FetchPersistPipelineResult 定义抓取流水线输出。
type FetchPersistPipelineResult struct {
	RepairFinalizeResult FetchRepairFinalizeResult
	PostPersistResult    FetchPostPersistResult
}

// RunFetchPersistPipeline 执行“抓取修复 -> 落库 -> 入库后收敛”完整流水线。
// 参数/返回：输入为抓取上下文与草稿，依赖注入修复/落库/后处理能力；返回修复与后收敛结果。
// 失败场景：草稿为空、收敛失败、写库失败会返回 error。
// 副作用：可能触发截图/海报/简介修复、写入 seed_parameters，并触发媒体修复和标签回写。
func RunFetchPersistPipeline(input FetchPersistPipelineInput, deps FetchPersistPipelineDeps) (FetchPersistPipelineResult, error) {
	if input.Draft == nil {
		return FetchPersistPipelineResult{}, errors.New("seed draft is nil")
	}
	if deps.Repo == nil {
		return FetchPersistPipelineResult{}, errors.New("repo is nil")
	}

	repairFinalizeResult, finalizeErr := RunFetchRepairAndFinalize(
		FetchRepairFinalizeInput{
			TaskID:               input.TaskID,
			SavePath:             input.SavePath,
			DownloaderID:         input.DownloaderID,
			TorrentNameForPath:   input.TorrentNameForPath,
			ScreenshotReviewMode: input.ScreenshotReviewMode,
			RootConfig:           deps.FetchRepairDeps.RootConfig,
			ReviewData:           input.ReviewData,
			DetailHTML:           input.DetailHTML,
			Draft:                input.Draft,
			MetaName:             input.MetaName,
			SiteIdentifier:       input.SiteIdentifier,
		},
		FetchRepairFinalizeDeps{
			FetchRepairDeps:            deps.FetchRepairDeps,
			Now:                        deps.Now,
			BuildSimpleTitleComponents: deps.BuildSimpleTitleComponents,
		},
	)
	if finalizeErr != nil {
		return FetchPersistPipelineResult{}, finalizeErr
	}

	finalizeResult := repairFinalizeResult.FinalizeResult
	normalizeAndApplyTeamAcknowledgment(input.Draft, input.SiteIdentifier, finalizeResult.Record, deps)

	{
		snapshot := buildPersistSnapshotLog(
			input.TaskID,
			input.SiteIdentifier,
			input.Hash,
			input.TorrentID,
			finalizeResult.Record,
		)
		logx.PlainModuleInfof("迁移-入库快照", "字段", "%s", snapshot)
	}

	if deps.EmitLog != nil {
		deps.EmitLog("写入数据库", "正在写入 seed_parameters...", "processing")
	}
	if err := deps.Repo.UpsertSeedParameter(finalizeResult.Record); err != nil {
		if deps.EmitLog != nil {
			deps.EmitLog("写入数据库", "保存失败: "+err.Error(), "error")
		}
		return FetchPersistPipelineResult{}, err
	}
	if deps.EmitLog != nil {
		deps.EmitLog("写入数据库", "数据库写入完成", "success")
	}

	postPersistResult := FinalizeFetchPostPersist(
		deps.Repo,
		FetchPostPersistInput{
			TaskID:             input.TaskID,
			Hash:               input.Hash,
			TorrentID:          input.TorrentID,
			SiteIdentifier:     input.SiteIdentifier,
			SavePath:           input.SavePath,
			ContentName:        input.Draft.Title,
			DownloaderID:       input.DownloaderID,
			TorrentNameForPath: input.TorrentNameForPath,
			CurrentMedia:       input.Draft.Mediainfo,
			MediainfoValid:     finalizeResult.MediainfoValid,
			InitialStatus:      finalizeResult.MediainfoStatus,
		},
		FetchPostPersistDeps{
			TriggerMediainfoRepair: deps.TriggerMediainfoRepair,
			RecomputeTags:          deps.RecomputeTags,
		},
	)

	return FetchPersistPipelineResult{
		RepairFinalizeResult: repairFinalizeResult,
		PostPersistResult:    postPersistResult,
	}, nil
}

func normalizeAndApplyTeamAcknowledgment(draft *SeedDraft, siteIdentifier string, record map[string]any, deps FetchPersistPipelineDeps) {
	if draft == nil || record == nil {
		return
	}

	rawReleaseGroup := extractRawReleaseGroupFromTitleComponents(draft.TitleComponents)
	teamKey := parser.NormalizeTeamKeyForSite(rawReleaseGroup, siteIdentifier)
	if teamKey == "team.other" {
		// 兜底：如果标题没能抽到制作组，则尝试使用抓取/页面字段中的制作组文本。
		teamKey = parser.NormalizeTeamKeyForSite(draft.Team, siteIdentifier)
	}
	if strings.TrimSpace(teamKey) == "" {
		teamKey = "team.other"
	}
	draft.Team = teamKey
	record["team"] = teamKey

	rawSites, err := deps.Repo.ListSitesGroupAndDescription()
	if err != nil {
		logx.Warnf(teamAckLogModule, "官组致谢读取sites失败 torrent_id=%s site=%s err=%v", draft.TorrentID, siteIdentifier, err)
		return
	}
	sites := make([]processingack.SiteRow, 0, len(rawSites))
	for _, row := range rawSites {
		sites = append(sites, processingack.SiteRow{
			Group:       strings.TrimSpace(toStringAny(row["group"], "")),
			Description: strings.TrimSpace(toStringAny(row["description"], "")),
		})
	}

	statement := strings.TrimSpace(toStringAny(record["statement"], ""))
	logx.Infof(
		teamAckLogModule,
		"官组致谢检查 torrent_id=%s site=%s raw_release_group=%s team_key=%s statement_len=%d",
		draft.TorrentID,
		siteIdentifier,
		strings.TrimSpace(rawReleaseGroup),
		teamKey,
		len([]rune(statement)),
	)
	updated, applied, reason := processingack.ApplyTeamAcknowledgmentIfNeeded(statement, teamKey, sites)
	if applied {
		draft.Statement = updated
		record["statement"] = updated
	}
	logx.Infof(teamAckLogModule, "官组致谢结果 torrent_id=%s site=%s applied=%v reason=%s", draft.TorrentID, siteIdentifier, applied, reason)
	if deps.EmitLog != nil {
		step := "检查声明感谢"
		if applied {
			deps.EmitLog(step, "成功生成声明信息", "success")
		} else {
			deps.EmitLog(step, "无需添加声明: "+reason, "success")
		}
	}
}

func extractRawReleaseGroupFromTitleComponents(components []map[string]any) string {
	if len(components) == 0 {
		return ""
	}
	for _, component := range components {
		if strings.TrimSpace(toStringAny(component["key"], "")) != "制作组" {
			continue
		}
		return strings.TrimSpace(toStringAny(component["value"], ""))
	}
	return ""
}
