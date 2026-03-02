package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service"
)

type StatsHandler struct {
	service *service.StatsService
}

func NewStatsHandler(svc *service.StatsService) *StatsHandler {
	return &StatsHandler{service: svc}
}

func (h *StatsHandler) ChartData(c *gin.Context) {
	result, err := h.service.GetChartData(c.DefaultQuery("range", "this_week"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取图表数据失败"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *StatsHandler) SpeedData(c *gin.Context) {
	result, err := h.service.GetSpeedData()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取实时速度失败"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *StatsHandler) RecentSpeedData(c *gin.Context) {
	seconds := 60
	if raw := c.Query("seconds"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的秒数参数"})
			return
		}
		if parsed > 0 {
			seconds = parsed
		}
	}
	result, err := h.service.GetRecentSpeedData(seconds)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取最近速度数据失败"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *StatsHandler) SpeedChartData(c *gin.Context) {
	result, err := h.service.GetSpeedChartData(c.DefaultQuery("range", "last_12_hours"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取速度图表数据失败"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *StatsHandler) SiteStats(c *gin.Context) {
	result, err := h.service.GetSiteStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取站点统计信息失败"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *StatsHandler) GroupStats(c *gin.Context) {
	site := c.Query("site")
	result, err := h.service.GetGroupStats(site)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取发布组统计信息失败"})
		return
	}
	c.JSON(http.StatusOK, result)
}
