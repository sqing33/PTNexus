package bdflow

import "time"

// BDInfoTask 表示 BDInfo 异步任务在内存中的状态快照。
type BDInfoTask struct {
	TaskID          string
	SeedID          string
	Status          string
	ProgressPercent float64
	CurrentFile     string
	ElapsedTime     string
	RemainingTime   string
	DiscSize        int64
	MediaInfo       string
	Error           string
	StartedAt       time.Time
	UpdatedAt       time.Time
	CompletedAt     *time.Time
}
