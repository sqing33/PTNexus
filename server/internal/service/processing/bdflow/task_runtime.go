package bdflow

import (
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
)

// RunTaskInput 定义 BDInfo 任务执行输入。
type RunTaskInput struct {
	TaskID           string
	Hash             string
	TorrentID        string
	SiteName         string
	TorrentName      string
	SeedSavePath     string
	SeedDownloaderID string
}

// RunTaskDeps 定义 BDInfo 任务执行依赖。
type RunTaskDeps struct {
	LogModule string

	Repo  BDInfoFlowRepo
	State *BDInfoState

	RootConfig map[string]any

	ComposeSeedID func(hash, torrentID, siteName string) string
	RecomputeTags func(hash, torrentID, siteName, savePath, torrentName, reason string)
	Now           func() time.Time
}

// RunTask 执行 BDInfo 任务运行时流程（含路径映射、标题组件回写、标签回写）。
// 参数/返回：input 为任务上下文；deps 注入仓储、状态与回调；无返回值。
// 失败场景：依赖缺失时任务内部会进入失败状态并写入失败原因。
// 副作用：会调用 BDInfo/MediaInfo 提取、更新数据库并回写标签。
func RunTask(input RunTaskInput, deps RunTaskDeps) {
	logModule := strings.TrimSpace(deps.LogModule)
	if logModule == "" {
		logModule = "BDInfo任务"
	}
	composeSeedID := deps.ComposeSeedID
	if composeSeedID == nil {
		composeSeedID = func(hash, torrentID, siteName string) string {
			return strings.TrimSpace(hash) + "_" + strings.TrimSpace(torrentID) + "_" + strings.TrimSpace(siteName)
		}
	}
	nowFn := deps.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	rootConfig := deps.RootConfig
	if rootConfig == nil {
		rootConfig = map[string]any{}
	}

	RunBDInfoTaskWithState(
		RunBDInfoTaskInput{
			TaskID:           input.TaskID,
			Hash:             input.Hash,
			TorrentID:        input.TorrentID,
			SiteName:         input.SiteName,
			TorrentName:      input.TorrentName,
			SeedSavePath:     input.SeedSavePath,
			SeedDownloaderID: input.SeedDownloaderID,
		},
		RunBDInfoTaskWithStateDeps{
			LogModule: logModule,
			Repo:      deps.Repo,
			State:     deps.State,
			TranslateDownloaderPath: func(downloaderID string, savePath string) string {
				return strings.TrimSpace(downloaderclient.TranslateDownloaderPath(rootConfig, downloaderID, savePath))
			},
			RewriteTitleComponents: func(taskHash, taskTorrentID, taskSiteName string, mediaInfo string) {
				row, rowErr := deps.Repo.GetSeedParameterByKey(taskHash, taskTorrentID, taskSiteName)
				if rowErr != nil {
					logx.Warnf(logModule, "标题组件回写跳过 task_id=%s seed_id=%s err=%v", input.TaskID, composeSeedID(taskHash, taskTorrentID, taskSiteName), rowErr)
					return
				}
				processingpersist.RewriteSeedTitleComponentsByMediaInfo(
					logModule,
					deps.Repo,
					taskHash,
					taskTorrentID,
					taskSiteName,
					nowFn(),
					row,
					mediaInfo,
				)
			},
			RecomputeTags: deps.RecomputeTags,
			ComposeSeedID: composeSeedID,
		},
	)
}
