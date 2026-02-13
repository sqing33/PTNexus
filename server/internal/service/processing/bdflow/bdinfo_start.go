package bdflow

import (
	"strings"
	"time"
)

// StartBDInfoRepo 定义 BDInfo 任务启动所需的最小仓储接口。
type StartBDInfoRepo interface {
	GetSeedParameterByKey(hash, torrentID, siteName string) (map[string]any, error)
	UpdateSeedParameterByKey(hash, torrentID, siteName string, updates map[string]any) error
	GetCurrentTorrentByName(name string) (map[string]any, error)
}

// StartBDInfoTaskInput 定义 BDInfo 启动输入。
type StartBDInfoTaskInput struct {
	Hash      string
	TorrentID string
	SiteName  string
	TaskID    string
	Now       time.Time
}

// StartBDInfoTaskResult 定义 BDInfo 启动输出。
type StartBDInfoTaskResult struct {
	TorrentName      string
	SeedSavePath     string
	SeedDownloaderID string
	InitialMediaInfo string
}

// StartBDInfoTask 负责 BDInfo 任务启动前的数据库状态更新与路径回填。
func StartBDInfoTask(repo StartBDInfoRepo, input StartBDInfoTaskInput) (StartBDInfoTaskResult, error) {
	row, err := repo.GetSeedParameterByKey(input.Hash, input.TorrentID, input.SiteName)
	if err != nil {
		return StartBDInfoTaskResult{}, err
	}

	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	if err := repo.UpdateSeedParameterByKey(input.Hash, input.TorrentID, input.SiteName, map[string]any{
		"mediainfo_status":    "processing_bdinfo",
		"bdinfo_task_id":      strings.TrimSpace(input.TaskID),
		"bdinfo_started_at":   now.Format("2006-01-02 15:04:05"),
		"bdinfo_completed_at": nil,
		"bdinfo_error":        "",
		"updated_at":          now.Format("2006-01-02 15:04:05"),
	}); err != nil {
		return StartBDInfoTaskResult{}, err
	}

	torrentName := strings.TrimSpace(toStringAny(row["name"]))
	seedSavePath := strings.TrimSpace(toStringAny(row["save_path"]))
	seedDownloaderID := strings.TrimSpace(toStringAny(row["downloader_id"]))

	if (seedSavePath == "" || seedDownloaderID == "") && torrentName != "" {
		if current, currentErr := repo.GetCurrentTorrentByName(torrentName); currentErr == nil && len(current) > 0 {
			if seedSavePath == "" {
				seedSavePath = strings.TrimSpace(toStringAny(current["save_path"]))
			}
			if seedDownloaderID == "" {
				seedDownloaderID = strings.TrimSpace(toStringAny(current["downloader_id"]))
			}
		}
	}

	return StartBDInfoTaskResult{
		TorrentName:      torrentName,
		SeedSavePath:     seedSavePath,
		SeedDownloaderID: seedDownloaderID,
		InitialMediaInfo: strings.TrimSpace(toStringAny(row["mediainfo"])),
	}, nil
}
