package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/scheduledseed"
)

// ScheduledSeedHandler 处理定时发种任务的 HTTP 请求。
type ScheduledSeedHandler struct {
	repo      *repository.ScheduledSeedRepository
	scheduler *scheduledseed.Scheduler
}

// NewScheduledSeedHandler 创建定时发种处理器。
func NewScheduledSeedHandler(repo *repository.ScheduledSeedRepository, scheduler *scheduledseed.Scheduler) *ScheduledSeedHandler {
	return &ScheduledSeedHandler{repo: repo, scheduler: scheduler}
}

// ListTasks 分页查询定时发种任务列表。
func (h *ScheduledSeedHandler) ListTasks(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := strings.TrimSpace(c.Query("status"))

	tasks, total, err := h.repo.List(page, pageSize, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    tasks,
		"total":   total,
	})
}

// GetTask 查询单个任务详情。
func (h *ScheduledSeedHandler) GetTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的任务 ID"})
		return
	}

	task, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任务不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": task})
}

// createTaskRequest 创建任务的请求体。
type createTaskRequest struct {
	Name            string            `json:"name"`
	Seeds           []seedRefRequest  `json:"seeds"`
	TargetSites     []string          `json:"target_sites"`
	IntervalMinutes int               `json:"interval_minutes"`
	LoopEnabled     bool              `json:"loop_enabled"`
	StartTime       string            `json:"start_time,omitempty"`
}

type seedRefRequest struct {
	TorrentID string `json:"torrent_id"`
	SiteName  string `json:"site_name"`
	Title     string `json:"title,omitempty"`
}

// CreateTask 创建新的定时发种任务。
func (h *ScheduledSeedHandler) CreateTask(c *gin.Context) {
	var req createTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求参数无效: " + err.Error()})
		return
	}

	// 验证
	if strings.TrimSpace(req.Name) == "" {
		// 新建任务未指定名称时，按当前时间自动生成
		req.Name = time.Now().Format("20060102-150405")
	}
	if len(req.Seeds) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "至少选择一个种子"})
		return
	}
	if len(req.TargetSites) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "至少选择一个目标站点"})
		return
	}
	if req.IntervalMinutes < 1 || req.IntervalMinutes > 1440 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "间隔时间需在 1~1440 分钟之间"})
		return
	}

	seedsJSON, _ := json.Marshal(req.Seeds)
	sitesJSON, _ := json.Marshal(req.TargetSites)

	nextRun := time.Now().Format(repository.PublishQueueTimeLayout)
	if st := strings.TrimSpace(req.StartTime); st != "" {
		// 尝试多种时间格式解析
		for _, layout := range []string{
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			time.RFC3339,
		} {
			if t, err := time.Parse(layout, st); err == nil {
				nextRun = t.Format(repository.PublishQueueTimeLayout)
				break
			}
		}
	}

	task := &repository.ScheduledSeedTask{
		Name:            strings.TrimSpace(req.Name),
		Status:          repository.ScheduledSeedStatusActive,
		SeedsJSON:       string(seedsJSON),
		TargetSitesJSON: string(sitesJSON),
		IntervalMinutes: req.IntervalMinutes,
		LoopEnabled:     req.LoopEnabled,
		NextRunAt:       nextRun,
	}

	if err := h.repo.Create(task); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": task, "message": "任务创建成功"})
}

// UpdateTask 更新定时发种任务。
func (h *ScheduledSeedHandler) UpdateTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的任务 ID"})
		return
	}

	existing, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任务不存在"})
		return
	}

	var req createTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求参数无效: " + err.Error()})
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		// 未指定名称时沿用原任务名称
		req.Name = existing.Name
	}
	if len(req.Seeds) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "至少选择一个种子"})
		return
	}
	if len(req.TargetSites) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "至少选择一个目标站点"})
		return
	}
	if req.IntervalMinutes < 1 || req.IntervalMinutes > 1440 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "间隔时间需在 1~1440 分钟之间"})
		return
	}

	seedsJSON, _ := json.Marshal(req.Seeds)
	sitesJSON, _ := json.Marshal(req.TargetSites)

	// 检测种子或站点是否变化，变化则重置指针
	seedsChanged := string(seedsJSON) != existing.SeedsJSON
	sitesChanged := string(sitesJSON) != existing.TargetSitesJSON

	existing.Name = strings.TrimSpace(req.Name)
	existing.SeedsJSON = string(seedsJSON)
	existing.TargetSitesJSON = string(sitesJSON)
	existing.IntervalMinutes = req.IntervalMinutes
	existing.LoopEnabled = req.LoopEnabled

	if seedsChanged || sitesChanged {
		existing.CurrentSeedIndex = 0
		existing.CurrentSiteIndex = 0
		if existing.Status == repository.ScheduledSeedStatusCompleted {
			existing.Status = repository.ScheduledSeedStatusActive
		}
		nextRun := time.Now().Format(repository.PublishQueueTimeLayout)
		if st := strings.TrimSpace(req.StartTime); st != "" {
			for _, layout := range []string{
				"2006-01-02 15:04:05",
				"2006-01-02T15:04:05",
				time.RFC3339,
			} {
				if t, err := time.Parse(layout, st); err == nil {
					nextRun = t.Format(repository.PublishQueueTimeLayout)
					break
				}
			}
		}
		existing.NextRunAt = nextRun
	} else if st := strings.TrimSpace(req.StartTime); st != "" {
		// 种子和站点未变，但用户显式指定了开始时间
		for _, layout := range []string{
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			time.RFC3339,
		} {
			if t, err := time.Parse(layout, st); err == nil {
				existing.NextRunAt = t.Format(repository.PublishQueueTimeLayout)
				break
			}
		}
	}

	if err := h.repo.Update(existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": existing, "message": "任务更新成功"})
}

// DeleteTask 删除定时发种任务。
func (h *ScheduledSeedHandler) DeleteTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的任务 ID"})
		return
	}

	if err := h.repo.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "任务已删除"})
}

// ToggleTask 切换任务的启动/暂停状态。
func (h *ScheduledSeedHandler) ToggleTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的任务 ID"})
		return
	}

	newStatus, err := h.repo.ToggleStatus(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"status": newStatus}, "message": "状态已切换"})
}

// ListAvailableSeeds 查询可选种子列表。
func (h *ScheduledSeedHandler) ListAvailableSeeds(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	search := strings.TrimSpace(c.Query("search"))
	downloader := strings.TrimSpace(c.Query("downloader"))
	sortProp := strings.TrimSpace(c.Query("sort_prop"))
	sortOrder := strings.TrimSpace(c.Query("sort_order"))

	// 解析 JSON 数组参数
	var existSites []string
	if v := c.Query("exist_sites"); v != "" {
		json.Unmarshal([]byte(v), &existSites)
	}
	var notExistSites []string
	if v := c.Query("not_exist_sites"); v != "" {
		json.Unmarshal([]byte(v), &notExistSites)
	}
	var stateFilters []string
	if v := c.Query("state_filters"); v != "" {
		json.Unmarshal([]byte(v), &stateFilters)
	}
	var pathFilters []string
	if v := c.Query("path_filters"); v != "" {
		json.Unmarshal([]byte(v), &pathFilters)
	}

	filter := repository.SeedFilter{
		Search:        search,
		Downloader:    downloader,
		ExistSites:    existSites,
		NotExistSites: notExistSites,
		StateFilters:  stateFilters,
		PathFilters:   pathFilters,
		SortProp:      sortProp,
		SortOrder:     sortOrder,
	}

	seeds, total, totalSiteCount, err := h.repo.ListAvailableSeeds(page, pageSize, filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	// 获取站点列表和筛选项
	allSites, _ := h.repo.ListAllSeedSites()
	uniquePaths, uniqueStates, _ := h.repo.ListSeedUniques()

	c.JSON(http.StatusOK, gin.H{
		"success":          true,
		"data":             seeds,
		"total":            total,
		"total_site_count": totalSiteCount,
		"all_sites":        allSites,
		"unique_paths":     uniquePaths,
		"unique_states":    uniqueStates,
	})
}

// GetSeedSites 查询指定种子已发布的站点列表。
func (h *ScheduledSeedHandler) GetSeedSites(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "种子名称不能为空"})
		return
	}

	sites, err := h.repo.GetSeedSites(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    sites,
	})
}

// TriggerTask 手动触发指定任务立即执行下一次发种。
func (h *ScheduledSeedHandler) TriggerTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的任务 ID"})
		return
	}

	task, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任务不存在"})
		return
	}

	if task.Status != repository.ScheduledSeedStatusActive {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "任务当前状态不允许手动触发"})
		return
	}

	h.scheduler.TriggerTask(id)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已触发发种任务"})
}
