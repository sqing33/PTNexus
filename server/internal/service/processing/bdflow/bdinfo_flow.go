package bdflow

import (
	"strings"
	"time"

	"github.com/pt-nexus/server-go/internal/platform/logx"
	processingmedia "github.com/pt-nexus/server-go/internal/service/processing/media"
)

// BDInfoFlowRepo 定义 BDInfo 执行流程需要的最小仓储接口。
type BDInfoFlowRepo interface {
	GetCurrentTorrentByHash(hash string) (map[string]any, error)
	GetCurrentTorrentByName(name string) (map[string]any, error)
	UpdateSeedParameterByKey(hash, torrentID, siteName string, updates map[string]any) error
	GetSeedParameterByKey(hash, torrentID, siteName string) (map[string]any, error)
}

// RunBDInfoTaskInput 定义 BDInfo 执行流程输入。
type RunBDInfoTaskInput struct {
	TaskID           string
	Hash             string
	TorrentID        string
	SiteName         string
	TorrentName      string
	SeedSavePath     string
	SeedDownloaderID string
}

// RunBDInfoTaskDeps 定义 BDInfo 执行流程依赖注入。
type RunBDInfoTaskDeps struct {
	LogModule string

	Repo BDInfoFlowRepo

	SetTaskProgress         func(status string, progress float64, currentFile, elapsed, remaining string)
	SetTaskFailed           func(errMsg string, completedAt time.Time)
	SetTaskCompleted        func(currentFile string, mediaInfo string, completedAt time.Time, elapsed string)
	TranslateDownloaderPath func(downloaderID string, savePath string) string

	RewriteTitleComponents func(hash, torrentID, siteName string, mediaInfo string)
	RecomputeTags          func(hash, torrentID, siteName, savePath, torrentName, reason string)
	ComposeSeedID          func(hash, torrentID, siteName string) string
}

// RunBDInfoTask 执行单个 BDInfo 任务主流程：回填路径、提取媒体信息、写库、回写标题与标签。
// 参数/返回：输入包含任务与种子标识；通过依赖回调更新任务状态与后处理；无返回值。
// 失败场景：路径无法定位、提取失败、仓储未注入时，会将任务置为 failed 并写库失败状态。
// 副作用：访问数据库、读取文件系统、调用 BDInfo/MediaInfo 工具并更新任务状态。
func RunBDInfoTask(input RunBDInfoTaskInput, deps RunBDInfoTaskDeps) {
	logModule := strings.TrimSpace(deps.LogModule)
	if logModule == "" {
		logModule = bdinfoToolLogModule
	}

	begin := time.Now()
	setProgress := func(status string, progress float64, currentFile, elapsed, remaining string) {
		if deps.SetTaskProgress != nil {
			deps.SetTaskProgress(status, progress, currentFile, elapsed, remaining)
		}
	}
	failAndPersist := func(errMsg string) {
		setProgress("failed", 100, "失败", processingmedia.ElapsedString(begin), "0s")
		now := time.Now()
		if deps.SetTaskFailed != nil {
			deps.SetTaskFailed(errMsg, now)
		}
		if deps.Repo != nil {
			_ = deps.Repo.UpdateSeedParameterByKey(input.Hash, input.TorrentID, input.SiteName, map[string]any{
				"mediainfo_status":    "failed",
				"bdinfo_error":        errMsg,
				"bdinfo_completed_at": now.Format("2006-01-02 15:04:05"),
				"updated_at":          now.Format("2006-01-02 15:04:05"),
			})
		}
	}

	if deps.Repo == nil {
		failAndPersist("BDInfo流程依赖未初始化")
		return
	}

	setProgress("processing_bdinfo", 10, "准备提取媒体信息", "0s", "--")

	savePath := strings.TrimSpace(input.SeedSavePath)
	downloaderID := strings.TrimSpace(input.SeedDownloaderID)
	torrentName := strings.TrimSpace(input.TorrentName)
	logx.Infof(logModule, "开始执行任务 task_id=%s hash=%s torrent_name=%s initial_save_path=%s initial_downloader_id=%s", strings.TrimSpace(input.TaskID), strings.TrimSpace(input.Hash), torrentName, savePath, downloaderID)

	if strings.TrimSpace(input.Hash) != "" {
		if current, err := deps.Repo.GetCurrentTorrentByHash(strings.TrimSpace(input.Hash)); err == nil {
			currentSavePath := strings.TrimSpace(toStringWithFallback(current["save_path"], ""))
			currentDownloaderID := strings.TrimSpace(toStringWithFallback(current["downloader_id"], ""))
			currentName := strings.TrimSpace(toStringWithFallback(current["name"], ""))
			logx.Infof(logModule, "通过hash回填当前种子 task_id=%s name=%s save_path=%s downloader_id=%s", strings.TrimSpace(input.TaskID), currentName, currentSavePath, currentDownloaderID)
			if currentSavePath != "" {
				savePath = currentSavePath
			}
			if currentDownloaderID != "" {
				downloaderID = currentDownloaderID
			}
			if torrentName == "" && currentName != "" {
				torrentName = currentName
			}
		} else {
			logx.Warnf(logModule, "通过hash回填失败 task_id=%s hash=%s err=%v", strings.TrimSpace(input.TaskID), strings.TrimSpace(input.Hash), err)
		}
	}

	if savePath == "" && torrentName != "" {
		if current, err := deps.Repo.GetCurrentTorrentByName(torrentName); err == nil {
			savePath = strings.TrimSpace(toStringWithFallback(current["save_path"], ""))
			if downloaderID == "" {
				downloaderID = strings.TrimSpace(toStringWithFallback(current["downloader_id"], ""))
			}
			logx.Infof(logModule, "按名称回填成功 task_id=%s save_path=%s downloader_id=%s", strings.TrimSpace(input.TaskID), savePath, downloaderID)
		} else {
			logx.Warnf(logModule, "按名称回填失败 task_id=%s name=%s err=%v", strings.TrimSpace(input.TaskID), torrentName, err)
		}
	}

	if savePath == "" {
		failAndPersist("无法定位保存路径")
		return
	}

	mappedSavePath := savePath
	if deps.TranslateDownloaderPath != nil {
		mappedSavePath = strings.TrimSpace(deps.TranslateDownloaderPath(downloaderID, savePath))
	}
	if mappedSavePath == "" {
		mappedSavePath = savePath
	}
	if mappedSavePath != savePath {
		logx.Infof(logModule, "下载器路径映射完成 task_id=%s downloader_id=%s from=%s to=%s", strings.TrimSpace(input.TaskID), downloaderID, savePath, mappedSavePath)
	}

	setProgress("processing_bdinfo", 35, "定位媒体文件", processingmedia.ElapsedString(begin), "--")
	extractResult, extractErr := ResolveAndExtractForBDInfo(
		ResolveAndExtractForBDInfoInput{
			TaskID:         strings.TrimSpace(input.TaskID),
			TorrentName:    torrentName,
			SavePath:       savePath,
			MappedSavePath: mappedSavePath,
			DownloaderID:   downloaderID,
		},
	)
	if extractErr != nil {
		failAndPersist(extractErr.Error())
		return
	}

	mediaInfo := strings.TrimSpace(extractResult.MediaInfoText)
	currentFileLabel := strings.TrimSpace(extractResult.CurrentFileLabel)
	if currentFileLabel == "" {
		currentFileLabel = "媒体信息"
	}
	setProgress("processing_bdinfo", 70, currentFileLabel, processingmedia.ElapsedString(begin), "--")

	now := time.Now()
	if deps.SetTaskCompleted != nil {
		deps.SetTaskCompleted(currentFileLabel, mediaInfo, now, processingmedia.ElapsedString(begin))
	}

	_ = deps.Repo.UpdateSeedParameterByKey(input.Hash, input.TorrentID, input.SiteName, map[string]any{
		"mediainfo_status":    "completed",
		"mediainfo":           mediaInfo,
		"bdinfo_completed_at": now.Format("2006-01-02 15:04:05"),
		"bdinfo_error":        "",
		"updated_at":          now.Format("2006-01-02 15:04:05"),
	})

	seedID := strings.TrimSpace(input.Hash) + "_" + strings.TrimSpace(input.TorrentID) + "_" + strings.TrimSpace(input.SiteName)
	if deps.ComposeSeedID != nil {
		seedID = deps.ComposeSeedID(input.Hash, input.TorrentID, input.SiteName)
	}

	row, rowErr := deps.Repo.GetSeedParameterByKey(input.Hash, input.TorrentID, input.SiteName)
	if rowErr != nil {
		logx.Warnf(logModule, "标题组件回写跳过 task_id=%s seed_id=%s err=%v", strings.TrimSpace(input.TaskID), seedID, rowErr)
	} else if boolFromAny(row["is_reviewed"]) {
		logx.Infof(logModule, "标题组件回写跳过 task_id=%s seed_id=%s is_reviewed=true", strings.TrimSpace(input.TaskID), seedID)
	} else if deps.RewriteTitleComponents != nil {
		deps.RewriteTitleComponents(input.Hash, input.TorrentID, input.SiteName, mediaInfo)
	}
	logx.Infof(logModule, "任务执行完成 task_id=%s seed_id=%s final_status=completed output_bytes=%d", strings.TrimSpace(input.TaskID), seedID, len(mediaInfo))

	if deps.RecomputeTags != nil {
		deps.RecomputeTags(input.Hash, input.TorrentID, input.SiteName, input.SeedSavePath, input.TorrentName, "BDInfo完成")
	}
}

func toStringWithFallback(value any, fallback string) string {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed != "" {
			return trimmed
		}
	case []byte:
		trimmed := strings.TrimSpace(string(typed))
		if trimmed != "" {
			return trimmed
		}
	}
	return fallback
}

func boolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case string:
		lower := strings.ToLower(strings.TrimSpace(typed))
		return lower == "1" || lower == "true" || lower == "yes"
	default:
		return false
	}
}
