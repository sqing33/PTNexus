package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server-go/internal/repository"
)

type TorrentsHandler struct {
	repo *repository.TorrentRepository
}

func NewTorrentsHandler(repo *repository.TorrentRepository) *TorrentsHandler {
	return &TorrentsHandler{repo: repo}
}

func (h *TorrentsHandler) Paths(c *gin.Context) {
	paths, err := h.repo.DistinctPaths()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取路径列表失败", "success": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "paths": paths})
}
