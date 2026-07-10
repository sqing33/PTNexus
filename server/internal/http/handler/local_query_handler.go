package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service"
)

type LocalQueryHandler struct {
	service *service.LocalQueryService
}

func NewLocalQueryHandler(svc *service.LocalQueryService) *LocalQueryHandler {
	return &LocalQueryHandler{service: svc}
}

func (h *LocalQueryHandler) ScanCache(c *gin.Context) {
	result, exists, err := h.service.GetScanCache()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "No cached scan result"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *LocalQueryHandler) Paths(c *gin.Context) {
	result, err := h.service.GetPaths(localQueryBool(c.Query("video_only")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *LocalQueryHandler) DownloadersWithPaths(c *gin.Context) {
	result, err := h.service.GetDownloadersWithPaths(localQueryBool(c.Query("video_only")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *LocalQueryHandler) Scan(c *gin.Context) {
	path := c.Query("path")
	result, err := h.service.Scan(path, localQueryBool(c.Query("video_only")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *LocalQueryHandler) AnalyzeDuplicates(c *gin.Context) {
	result, err := h.service.AnalyzeDuplicates(localQueryBool(c.Query("video_only")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func localQueryBool(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "1" || normalized == "true" || normalized == "yes"
}
