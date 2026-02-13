package bdflow

import "time"

// RunBDInfoTaskWithStateDeps 定义带状态管理的 BDInfo 执行依赖。
type RunBDInfoTaskWithStateDeps struct {
	LogModule string
	Repo      BDInfoFlowRepo
	State     *BDInfoState

	TranslateDownloaderPath func(downloaderID string, savePath string) string
	RewriteTitleComponents  func(hash, torrentID, siteName string, mediaInfo string)
	RecomputeTags           func(hash, torrentID, siteName, savePath, torrentName, reason string)
	ComposeSeedID           func(hash, torrentID, siteName string) string
}

// RunBDInfoTaskWithState 执行 BDInfo 任务并自动回写内存状态。
// 参数/返回：input 为任务输入；deps 注入仓储、状态和后处理回调；无返回值。
// 失败场景：状态管理器为空时忽略状态写入，但流程仍会尝试执行。
// 副作用：会更新 BDInfoState，并驱动底层 RunBDInfoTask 写库与文件提取。
func RunBDInfoTaskWithState(input RunBDInfoTaskInput, deps RunBDInfoTaskWithStateDeps) {
	now := time.Now
	RunBDInfoTask(input, RunBDInfoTaskDeps{
		LogModule: deps.LogModule,
		Repo:      deps.Repo,
		SetTaskProgress: func(status string, progress float64, currentFile, elapsed, remaining string) {
			if deps.State != nil {
				deps.State.SetProgress(input.TaskID, status, progress, currentFile, elapsed, remaining, now())
			}
		},
		SetTaskFailed: func(errMsg string, completedAt time.Time) {
			if deps.State != nil {
				deps.State.MarkFailed(input.TaskID, errMsg, completedAt)
			}
		},
		SetTaskCompleted: func(currentFile string, mediaInfo string, completedAt time.Time, elapsed string) {
			if deps.State != nil {
				deps.State.MarkCompleted(input.TaskID, currentFile, mediaInfo, completedAt, elapsed)
			}
		},
		TranslateDownloaderPath: deps.TranslateDownloaderPath,
		RewriteTitleComponents:  deps.RewriteTitleComponents,
		RecomputeTags:           deps.RecomputeTags,
		ComposeSeedID:           deps.ComposeSeedID,
	})
}

// RegisterBDInfoTaskInput 定义 BDInfo 任务注册参数。
type RegisterBDInfoTaskInput struct {
	TaskID           string
	SeedID           string
	InitialMediaInfo string
	Now              time.Time
}

// RegisterTask 创建并注册一个新的 BDInfo 任务快照。
// 参数/返回：input 为任务标识与初始状态；返回创建后的任务对象。
// 失败场景：state 为空时仅返回任务对象，不写入状态管理器。
// 副作用：会向 BDInfoState 写入新任务记录。
func RegisterTask(state *BDInfoState, input RegisterBDInfoTaskInput) *BDInfoTask {
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	task := &BDInfoTask{
		TaskID:          input.TaskID,
		SeedID:          input.SeedID,
		Status:          "processing_bdinfo",
		ProgressPercent: 0,
		CurrentFile:     "准备中",
		ElapsedTime:     "0s",
		RemainingTime:   "30s",
		MediaInfo:       input.InitialMediaInfo,
		Error:           "",
		StartedAt:       now,
		UpdatedAt:       now,
	}
	if state != nil {
		state.Register(task)
	}
	return task
}
