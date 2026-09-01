package scheduledseed

import (
	"fmt"
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
	triggerCh      chan int64
	once           sync.Once
}

// NewScheduler 创建调度器实例。
func NewScheduler(repo *repository.ScheduledSeedRepository) *Scheduler {
	return &Scheduler{
		repo:      repo,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
		triggerCh: make(chan int64, 16),
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
		case taskID := <-s.triggerCh:
			s.processTriggeredTask(taskID)
		case <-s.stopCh:
			return
		}
	}
}

// TriggerTask 手动触发指定任务立即执行一次。返回是否成功投递到触发队列。
// 批量触发场景下调用方可据此统计实际投递数量。
func (s *Scheduler) TriggerTask(taskID int64) bool {
	if s == nil {
		return false
	}
	select {
	case s.triggerCh <- taskID:
		logx.Infof(schedulerLogModule, "任务 %d 已手动触发", taskID)
		return true
	default:
		logx.Warnf(schedulerLogModule, "任务 %d 触发队列已满，丢弃", taskID)
		return false
	}
}

func (s *Scheduler) processTriggeredTask(taskID int64) {
	if s.enqueueFn == nil {
		return
	}
	task, err := s.repo.GetByID(taskID)
	if err != nil {
		logx.Errorf(schedulerLogModule, "手动触发任务 %d 查询失败: %v", taskID, err)
		return
	}
	if task.Status != repository.ScheduledSeedStatusActive {
		logx.Warnf(schedulerLogModule, "手动触发任务 %d 状态为 %s，跳过", taskID, task.Status)
		return
	}
	logx.Infof(schedulerLogModule, "手动触发任务 %d 开始执行", taskID)
	s.processTask(task, time.Now())
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

	processedSeeds := make(map[int]bool)

	for {
		// 确保种子索引在范围内
		seedIdx := task.CurrentSeedIndex
		if seedIdx >= len(seeds) {
			seedIdx = 0
		}

		// 所有种子都已处理过（循环模式下防止无限循环）
		if processedSeeds[seedIdx] {
			nextRun := now.Add(time.Duration(task.IntervalMinutes) * time.Minute).Format(repository.PublishQueueTimeLayout)
			lastRun := now.Format(repository.PublishQueueTimeLayout)
			s.repo.ClaimAndAdvance(task.ID, task.UpdatedAt, seedIdx, 0, repository.ScheduledSeedStatusActive, nextRun, lastRun, 0, 0)
			logx.Infof(schedulerLogModule, "任务 %d 所有种子均已处理，等待下一轮", task.ID)
			return
		}
		processedSeeds[seedIdx] = true

		currentSeed := seeds[seedIdx]
		totalPublished := 0
		totalSkipped := 0

		// 一个种子同时向所有目标站点发种
		var seedErr error
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					seedErr = fmt.Errorf("处理种子时发生异常: %v", recovered)
				}
			}()

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
					"publish_scene":    "scheduled_seeding",
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
		}()

		if seedErr != nil {
			totalSkipped++
			logx.Errorf(schedulerLogModule, "任务 %d 种子 %s 处理异常: %v，将继续处理下一个种子",
				task.ID, currentSeed.TorrentID, seedErr)
		}

		// 推进种子索引
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

		// 更新内存中的任务状态，供下一轮循环使用
		task.CurrentSeedIndex = newSeedIdx
		task.Status = newStatus
		task.UpdatedAt = now.Format(repository.PublishQueueTimeLayout)

		// 当前种子没有任何站点成功入队时，立即处理下一个种子，不等待下次发种时间。
		// 入队失败和处理异常都计入 totalSkipped，避免异常种子阻塞整个任务。
		if totalPublished == 0 && totalSkipped > 0 && newStatus == repository.ScheduledSeedStatusActive {
			logx.Infof(schedulerLogModule, "任务 %d 种子[%d] 未成功发种，立即处理下一个种子", task.ID, seedIdx)
			continue
		}

		// 有实际发布或任务已完成，等待下次调度
		return
	}
}
