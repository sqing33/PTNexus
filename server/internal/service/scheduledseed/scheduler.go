package scheduledseed

import (
	"sync"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
)

const schedulerLogModule = "定时发种调度"

// EnqueueFn 定义发布队列入队函数签名，由 MigrateService 注入。
type EnqueueFn func(payload map[string]any) (map[string]any, int)

// Scheduler 后台定时发种调度器，按固定间隔从任务定义中取出种子并入队发布。
type Scheduler struct {
	repo           *repository.ScheduledSeedRepository
	publishLogRepo *repository.PublishLogRepository
	enqueueFn      EnqueueFn
	stopCh         chan struct{}
	doneCh         chan struct{}
	once           sync.Once
}

// NewScheduler 创建调度器实例。
func NewScheduler(repo *repository.ScheduledSeedRepository) *Scheduler {
	return &Scheduler{
		repo:   repo,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// SetEnqueueFn 注入发布队列入队函数。
func (s *Scheduler) SetEnqueueFn(fn EnqueueFn) {
	if s == nil {
		return
	}
	s.enqueueFn = fn
}

// SetPublishLogRepo 注入发布日志仓储，用于记录入队失败。
func (s *Scheduler) SetPublishLogRepo(repo *repository.PublishLogRepository) {
	if s == nil {
		return
	}
	s.publishLogRepo = repo
}

// Start 启动后台调度协程（sync.Once 保证只启动一次）。
func (s *Scheduler) Start() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		go s.run()
		logx.Infof(schedulerLogModule, "定时发种调度器已启动")
	})
}

// Stop 停止后台调度协程。
func (s *Scheduler) Stop() {
	if s == nil {
		return
	}
	select {
	case <-s.stopCh:
		// 已关闭
	default:
		close(s.stopCh)
		logx.Infof(schedulerLogModule, "定时发种调度器正在停止...")
		<-s.doneCh
		logx.Infof(schedulerLogModule, "定时发种调度器已停止")
	}
}

func (s *Scheduler) run() {
	defer close(s.doneCh)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.processTick()
		case <-s.stopCh:
			return
		}
	}
}

func (s *Scheduler) processTick() {
	if s.enqueueFn == nil {
		return
	}

	now := time.Now()
	tasks, err := s.repo.FindDueTasks(now)
	if err != nil {
		logx.Errorf(schedulerLogModule, "查询到期任务失败: %v", err)
		return
	}

	for i := range tasks {
		s.processTask(&tasks[i], now)
	}
}

func (s *Scheduler) processTask(task *repository.ScheduledSeedTask, now time.Time) {
	seeds, err := task.ParseSeeds()
	if err != nil {
		logx.Errorf(schedulerLogModule, "任务 %d 解析种子列表失败: %v", task.ID, err)
		return
	}

	targetSites, err := task.ParseTargetSites()
	if err != nil {
		logx.Errorf(schedulerLogModule, "任务 %d 解析目标站点失败: %v", task.ID, err)
		return
	}

	if len(seeds) == 0 || len(targetSites) == 0 {
		logx.Warnf(schedulerLogModule, "任务 %d 种子或站点为空，跳过", task.ID)
		return
	}

	// 确保种子索引在范围内
	seedIdx := task.CurrentSeedIndex
	if seedIdx >= len(seeds) {
		seedIdx = 0
	}

	currentSeed := seeds[seedIdx]
	totalPublished := 0
	totalSkipped := 0

	// 一个种子同时向所有目标站点发种
	for _, site := range targetSites {
		// 查重
		isDup, dupErr := s.repo.CheckDuplicate(currentSeed.TorrentID, currentSeed.SiteName, site)
		if dupErr != nil {
			logx.Warnf(schedulerLogModule, "任务 %d 查重失败(%s→%s): %v，继续执行", task.ID, currentSeed.TorrentID, site, dupErr)
		}

		if isDup {
			totalSkipped++
			logx.Infof(schedulerLogModule, "任务 %d 种子 %s → %s 已发布过，跳过",
				task.ID, currentSeed.TorrentID, site)

			// 记录已存在的发布日志
			if s.publishLogRepo != nil {
				logEntry := &repository.PublishLogEntry{
					Trigger:    task.TriggerTag,
					Scene:      "scheduled_seeding",
					TorrentID:  currentSeed.TorrentID,
					SourceSite: currentSeed.SiteName,
					TargetSite: site,
					Title:      currentSeed.Title,
					Status:     "exists",
					Logs:       "该种子已成功发布过，跳过重复发种",
				}
				if _, insertErr := s.publishLogRepo.Insert(logEntry); insertErr != nil {
					logx.Warnf(schedulerLogModule, "任务 %d 写入跳过日志失败: %v", task.ID, insertErr)
				}
			}
			continue
		}

		// 入队发布
		payload := map[string]any{
			"target_site_name": site,
			"publish_scene":   "scheduled_seeding",
			"publish_trigger":  task.TriggerTag,
			"seeds": []any{
				map[string]any{
					"torrent_id": currentSeed.TorrentID,
					"site_name":  currentSeed.SiteName,
					"nickname":   currentSeed.SiteName,
				},
			},
		}

		result, code := s.enqueueFn(payload)
		if code != 200 {
			msg := "未知错误"
			if m, ok := result["message"].(string); ok {
				msg = m
			}
			logx.Errorf(schedulerLogModule, "任务 %d 入队失败(%s→%s): %s", task.ID, currentSeed.TorrentID, site, msg)

			// 写入失败记录到 publish_logs
			if s.publishLogRepo != nil {
				logEntry := &repository.PublishLogEntry{
					Trigger:    task.TriggerTag,
					Scene:      "scheduled_seeding",
					TorrentID:  currentSeed.TorrentID,
					SourceSite: currentSeed.SiteName,
					TargetSite: site,
					Title:      currentSeed.Title,
					Status:     "failed",
					Logs:       "入队失败: " + msg,
				}
				if _, insertErr := s.publishLogRepo.Insert(logEntry); insertErr != nil {
					logx.Warnf(schedulerLogModule, "任务 %d 写入失败日志失败: %v", task.ID, insertErr)
				}
			}
			totalSkipped++
		} else {
			totalPublished++
			logx.Infof(schedulerLogModule, "任务 %d 已入队: 种子 %s → %s",
				task.ID, currentSeed.TorrentID, site)
		}
	}

	// 推进种子索引（不再使用站点索引）
	newSeedIdx := seedIdx + 1
	newStatus := repository.ScheduledSeedStatusActive
	if newSeedIdx >= len(seeds) {
		if task.LoopEnabled {
			newSeedIdx = 0
			logx.Infof(schedulerLogModule, "任务 %d 种子已全部发布，循环模式重置", task.ID)
		} else {
			newStatus = repository.ScheduledSeedStatusCompleted
			newSeedIdx = len(seeds) // 保持在末尾
			logx.Infof(schedulerLogModule, "任务 %d 所有种子已发布完毕", task.ID)
		}
	}

	nextRun := now.Add(time.Duration(task.IntervalMinutes) * time.Minute).Format(repository.PublishQueueTimeLayout)
	lastRun := now.Format(repository.PublishQueueTimeLayout)

	ok, err := s.repo.ClaimAndAdvance(
		task.ID,
		task.UpdatedAt,
		newSeedIdx,
		0, // siteIdx 不再使用
		newStatus,
		nextRun,
		lastRun,
		totalPublished,
		totalSkipped,
	)
	if err != nil {
		logx.Errorf(schedulerLogModule, "任务 %d 更新调度状态失败: %v", task.ID, err)
		return
	}
	if !ok {
		logx.Warnf(schedulerLogModule, "任务 %d 更新调度状态未生效（任务可能已被删除）", task.ID)
		return
	}

	logx.Infof(schedulerLogModule, "任务 %d 种子[%d] 发布完成: 成功 %d 跳过 %d → 下一索引 %d",
		task.ID, seedIdx, totalPublished, totalSkipped, newSeedIdx)

	// 如果所有站点都跳过了（全部重复），立即尝试下一个种子
	if totalPublished == 0 && totalSkipped > 0 && newStatus == repository.ScheduledSeedStatusActive {
		time.Sleep(100 * time.Millisecond)
		refreshed, err := s.repo.GetByID(task.ID)
		if err == nil {
			s.processTask(refreshed, time.Now())
		}
	}
}
