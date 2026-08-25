package persist

import (
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
)

const fetchMediainfoRefreshLogModule = "迁移-媒体刷新"

// FetchPostPersistRepo 定义抓取入库后收敛所需的最小仓储接口。
type FetchPostPersistRepo interface {
	GetSeedParameterByKey(hash, torrentID, siteName string) (map[string]any, error)
}

// FetchPostPersistInput 定义抓取入库后收敛输入。
type FetchPostPersistInput struct {
	TaskID                   string
	Hash                     string
	TorrentID                string
	SiteIdentifier           string
	SavePath                 string
	ContentName              string
	DownloaderID             string
	TorrentNameForPath       string
	CurrentMedia             string
	Medium                   string
	MediainfoValid           bool
	InitialStatus            string
	SkipAutoRefresh          bool
	RefreshMediainfoForBatch bool
}

// FetchPostPersistDeps 定义抓取入库后收敛依赖。
type FetchPostPersistDeps struct {
	TriggerMediainfoRepair func(input processingrepair.TriggerMediainfoRepairInput)
	RecomputeTags          func(hash, torrentID, siteName string, savePath string, torrentNameForPath string, reason string)
}

// FetchPostPersistResult 定义抓取入库后收敛输出。
type FetchPostPersistResult struct {
	FinalMediainfoStatus string
	FinalBDInfoTaskID    string
}

// FinalizeFetchPostPersist 在 seed_parameters 落库后执行后续收敛动作（媒体修复、状态回读、标签重算）。
// 参数/返回：输入为种子标识与当前媒体状态；输出最终 mediainfo_status 与 bdinfo_task_id。
// 失败场景：数据库回读失败时回退为初始状态，不返回错误。
// 副作用：可能触发媒体修复回调，并在媒体完成后触发标签重算回调。
func FinalizeFetchPostPersist(repo FetchPostPersistRepo, input FetchPostPersistInput, deps FetchPostPersistDeps) FetchPostPersistResult {
	seedID := ComposeSeedID(strings.TrimSpace(input.Hash), strings.TrimSpace(input.TorrentID), strings.TrimSpace(input.SiteIdentifier))
	refreshForMedium := isWebDLOrEncodeMedium(input.Medium)
	shouldRefresh := !input.MediainfoValid || refreshForMedium
	if input.SkipAutoRefresh && !refreshForMedium {
		finalMediainfoStatus := strings.TrimSpace(input.InitialStatus)
		if finalMediainfoStatus == "" {
			finalMediainfoStatus = "queued"
		}
		return FetchPostPersistResult{
			FinalMediainfoStatus: finalMediainfoStatus,
			FinalBDInfoTaskID:    "",
		}
	}
	if refreshForMedium {
		logx.Infof(fetchMediainfoRefreshLogModule, "获取种子数据命中 WEB-DL/Encode，重新获取媒体信息 seed_id=%s medium=%s", seedID, strings.TrimSpace(input.Medium))
	}
	if shouldRefresh && deps.TriggerMediainfoRepair != nil {
		deps.TriggerMediainfoRepair(processingrepair.TriggerMediainfoRepairInput{
			TaskID:          strings.TrimSpace(input.TaskID),
			SeedID:          seedID,
			SavePath:        strings.TrimSpace(input.SavePath),
			ContentName:     strings.TrimSpace(input.ContentName),
			DownloaderID:    strings.TrimSpace(input.DownloaderID),
			TorrentNamePath: strings.TrimSpace(input.TorrentNameForPath),
			CurrentMedia:    strings.TrimSpace(input.CurrentMedia),
		})
	}

	finalMediainfoStatus := strings.TrimSpace(input.InitialStatus)
	if finalMediainfoStatus == "" {
		finalMediainfoStatus = "queued"
	}
	finalBDInfoTaskID := ""
	if repo != nil {
		if latestRow, latestErr := repo.GetSeedParameterByKey(input.Hash, input.TorrentID, input.SiteIdentifier); latestErr == nil {
			finalMediainfoStatus = strings.TrimSpace(toStringSimple(latestRow["mediainfo_status"]))
			if finalMediainfoStatus == "" {
				finalMediainfoStatus = strings.TrimSpace(input.InitialStatus)
				if finalMediainfoStatus == "" {
					finalMediainfoStatus = "queued"
				}
			}
			finalBDInfoTaskID = strings.TrimSpace(toStringSimple(latestRow["bdinfo_task_id"]))
		}
	}

	if finalMediainfoStatus == "completed" && deps.RecomputeTags != nil {
		deps.RecomputeTags(
			strings.TrimSpace(input.Hash),
			strings.TrimSpace(input.TorrentID),
			strings.TrimSpace(input.SiteIdentifier),
			strings.TrimSpace(input.SavePath),
			strings.TrimSpace(input.TorrentNameForPath),
			"MediaInfo修复后",
		)
	}

	return FetchPostPersistResult{
		FinalMediainfoStatus: finalMediainfoStatus,
		FinalBDInfoTaskID:    finalBDInfoTaskID,
	}
}

func isWebDLOrEncodeMedium(medium string) bool {
	normalized := strings.ToLower(strings.TrimSpace(medium))
	if normalized == "medium.webdl" || normalized == "webdl" || normalized == "web-dl" {
		return true
	}
	return normalized == "encode" ||
		strings.HasPrefix(normalized, "encode_") ||
		strings.HasPrefix(normalized, "medium.encode") ||
		strings.HasSuffix(normalized, "_encode") ||
		strings.Contains(normalized, "_encode_")
}
