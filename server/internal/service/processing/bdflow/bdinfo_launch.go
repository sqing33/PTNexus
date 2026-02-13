package bdflow

import (
	"strings"
	"time"
)

// StartAndRegisterInput 定义 BDInfo 启动并注册流程输入。
type StartAndRegisterInput struct {
	SeedID string
	TaskID string
	Now    time.Time
}

// StartAndRegisterDeps 定义 BDInfo 启动并注册流程依赖。
type StartAndRegisterDeps struct {
	Repo        StartBDInfoRepo
	State       *BDInfoState
	ParseSeedID func(seedID string) (hash string, torrentID string, siteName string, err error)
}

// StartAndRegisterResult 定义 BDInfo 启动并注册流程输出。
type StartAndRegisterResult struct {
	TaskID           string
	Hash             string
	TorrentID        string
	SiteName         string
	TorrentName      string
	SeedSavePath     string
	SeedDownloaderID string
}

// StartAndRegisterTask 执行 seed_id 解析、任务启动和内存状态注册。
// 参数/返回：input 为 seed/task 参数；deps 注入仓储、状态和 seed_id 解析器；返回已就绪任务信息。
// 失败场景：seed_id 解析失败或启动失败时返回 error。
// 副作用：会更新数据库中的 BDInfo 启动状态，并向 BDInfoState 注册任务。
func StartAndRegisterTask(input StartAndRegisterInput, deps StartAndRegisterDeps) (StartAndRegisterResult, error) {
	hash, torrentID, siteName, err := deps.ParseSeedID(input.SeedID)
	if err != nil {
		return StartAndRegisterResult{}, err
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}

	startResult, err := StartBDInfoTask(deps.Repo, StartBDInfoTaskInput{
		Hash:      hash,
		TorrentID: torrentID,
		SiteName:  siteName,
		TaskID:    input.TaskID,
		Now:       now,
	})
	if err != nil {
		return StartAndRegisterResult{}, err
	}

	RegisterTask(deps.State, RegisterBDInfoTaskInput{
		TaskID:           input.TaskID,
		SeedID:           input.SeedID,
		InitialMediaInfo: strings.TrimSpace(startResult.InitialMediaInfo),
		Now:              now,
	})

	return StartAndRegisterResult{
		TaskID:           input.TaskID,
		Hash:             hash,
		TorrentID:        torrentID,
		SiteName:         siteName,
		TorrentName:      strings.TrimSpace(startResult.TorrentName),
		SeedSavePath:     strings.TrimSpace(startResult.SeedSavePath),
		SeedDownloaderID: strings.TrimSpace(startResult.SeedDownloaderID),
	}, nil
}
