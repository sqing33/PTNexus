package torrentdata

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
)

type IYUUBatchTask struct {
	TaskID     string         `json:"task_id"`
	CreatedAt  string         `json:"created_at"`
	FinishedAt *string        `json:"finished_at"`
	IsRunning  bool           `json:"isRunning"`
	Success    *bool          `json:"success"`
	Message    string         `json:"message"`
	Total      int            `json:"total"`
	Processed  int            `json:"processed"`
	Stats      map[string]any `json:"stats"`
	QueryInfo  map[string]any `json:"query_info"`
}

type IYUUBatchProcessor func(item map[string]any) (map[string]any, error)

type IYUUTaskService struct {
	mu    sync.RWMutex
	tasks map[string]*IYUUBatchTask
}

func NewIYUUTaskService() *IYUUTaskService {
	return &IYUUTaskService{tasks: map[string]*IYUUBatchTask{}}
}

// CreateTask 创建一个新的批量 IYUU 查询任务，并返回 task_id。
// 参数/返回：total 为任务总组数；queryInfo 为初始化查询信息；返回 task_id。
// 失败场景：无。
// 副作用：写入内存任务表。
func (s *IYUUTaskService) CreateTask(total int, queryInfo map[string]any) string {
	if total < 0 {
		total = 0
	}
	taskID := s.newTaskID()
	now := time.Now().Format("2006-01-02 15:04:05")

	task := &IYUUBatchTask{
		TaskID:    taskID,
		CreatedAt: now,
		IsRunning: true,
		Message:   "任务已启动",
		Total:     total,
		Processed: 0,
		Stats:     map[string]any{},
		QueryInfo: queryInfo,
	}

	s.mu.Lock()
	s.tasks[taskID] = task
	s.mu.Unlock()
	return taskID
}

// UpdateTask 更新内存任务状态（并发安全）。
// 参数/返回：taskID 为任务ID；fn 用于原地修改任务对象；无返回值。
// 失败场景：任务不存在则忽略更新。
// 副作用：更新内存任务表。
func (s *IYUUTaskService) UpdateTask(taskID string, fn func(task *IYUUBatchTask)) {
	if strings.TrimSpace(taskID) == "" || fn == nil {
		return
	}
	s.mu.Lock()
	task, ok := s.tasks[taskID]
	if ok {
		fn(task)
	}
	s.mu.Unlock()
}

// FinishTask 结束任务并写入最终结果。
// 参数/返回：success 表示任务整体是否成功；message 为最终文案；stats/queryInfo 为最终统计与查询信息；无返回值。
// 失败场景：任务不存在则忽略。
// 副作用：更新内存任务表。
func (s *IYUUTaskService) FinishTask(taskID string, success bool, message string, stats map[string]any, queryInfo map[string]any) {
	finished := time.Now().Format("2006-01-02 15:04:05")
	s.UpdateTask(taskID, func(task *IYUUBatchTask) {
		task.IsRunning = false
		task.Success = &success
		task.FinishedAt = &finished
		task.Message = message
		if stats != nil {
			task.Stats = stats
		}
		if queryInfo != nil {
			task.QueryInfo = queryInfo
		}
	})
}

func (s *IYUUTaskService) GetTask(taskID string) (*IYUUBatchTask, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.tasks[taskID]
	if !ok {
		return nil, false
	}
	copied := *item
	if item.Stats != nil {
		copied.Stats = map[string]any{}
		for key, value := range item.Stats {
			copied.Stats[key] = value
		}
	}
	if item.QueryInfo != nil {
		copied.QueryInfo = map[string]any{}
		for key, value := range item.QueryInfo {
			copied.QueryInfo[key] = value
		}
	}
	return &copied, true
}

func (s *IYUUTaskService) newTaskID() string {
	return fmt.Sprintf("iyuu-%d-%06d", time.Now().UnixNano(), rand.Intn(1000000))
}

func sortedKeys(items map[string]struct{}) []string {
	if len(items) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(items))
	for key := range items {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
