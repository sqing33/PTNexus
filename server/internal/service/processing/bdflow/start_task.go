package bdflow

import (
	"errors"
	"strings"
	"time"

	"github.com/pt-nexus/server-go/internal/platform/logx"
)

var errNewTaskIDNotProvided = errors.New("未提供 NewTaskID 回调")

// StartTaskInput 定义 BDInfo 任务启动输入。
type StartTaskInput struct {
	SeedID string
	Force  bool
}

// StartTaskDeps 定义 BDInfo 任务启动依赖。
type StartTaskDeps struct {
	LogModule string

	Repo  BDInfoFlowRepo
	State *BDInfoState

	NewTaskID   func() string
	ParseSeedID func(seedID string) (hash string, torrentID string, siteName string, err error)

	RootConfig map[string]any

	ComposeSeedID func(hash, torrentID, siteName string) string
	RecomputeTags func(hash, torrentID, siteName, savePath, torrentName, reason string)
	Now           func() time.Time
}

// StartTask 启动 BDInfo 任务：入队、注册状态并异步执行。
// 参数/返回：input 为 seed_id 启动参数；deps 注入仓储与运行依赖；返回 task_id。
// 失败场景：seed_id 解析失败、数据库状态更新失败或缺少必要依赖。
// 副作用：会更新数据库任务状态，并启动异步任务执行。
func StartTask(input StartTaskInput, deps StartTaskDeps) (string, error) {
	_ = input.Force
	if deps.NewTaskID == nil {
		return "", errNewTaskIDNotProvided
	}

	nowFn := deps.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()

	taskID := strings.TrimSpace(deps.NewTaskID())
	launchResult, err := StartAndRegisterTask(
		StartAndRegisterInput{
			SeedID: input.SeedID,
			TaskID: taskID,
			Now:    now,
		},
		StartAndRegisterDeps{
			Repo:        deps.Repo,
			State:       deps.State,
			ParseSeedID: deps.ParseSeedID,
		},
	)
	if err != nil {
		return "", err
	}

	logModule := strings.TrimSpace(deps.LogModule)
	if logModule == "" {
		logModule = "BDInfo任务"
	}
	logx.Infof(
		logModule,
		"任务已入队 task_id=%s seed_id=%s hash=%s torrent_id=%s site=%s torrent_name=%s save_path=%s downloader_id=%s",
		taskID,
		input.SeedID,
		launchResult.Hash,
		launchResult.TorrentID,
		launchResult.SiteName,
		launchResult.TorrentName,
		launchResult.SeedSavePath,
		launchResult.SeedDownloaderID,
	)

	go RunTask(
		RunTaskInput{
			TaskID:           taskID,
			Hash:             launchResult.Hash,
			TorrentID:        launchResult.TorrentID,
			SiteName:         launchResult.SiteName,
			TorrentName:      launchResult.TorrentName,
			SeedSavePath:     launchResult.SeedSavePath,
			SeedDownloaderID: launchResult.SeedDownloaderID,
		},
		RunTaskDeps{
			LogModule:     logModule,
			Repo:          deps.Repo,
			State:         deps.State,
			RootConfig:    deps.RootConfig,
			ComposeSeedID: deps.ComposeSeedID,
			RecomputeTags: deps.RecomputeTags,
			Now:           nowFn,
		},
	)

	return taskID, nil
}
