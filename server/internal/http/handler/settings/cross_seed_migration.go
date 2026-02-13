package settings

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (h *Handler) GetCrossSeedSettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.GetCrossSeedSettings())
}

func (h *Handler) SaveCrossSeedSettings(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的设置数据格式。"})
		return
	}
	if err := h.settings.SaveCrossSeedSettings(payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法将设置写入配置文件。"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "转种设置已成功保存！"})
}

func (h *Handler) PublishConcurrencyInfo(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.PublishConcurrencyInfo())
}

func (h *Handler) CheckMigrationNeeded(c *gin.Context) {
	mappings := h.settings.BuildDownloaderIDMappings()
	c.JSON(http.StatusOK, gin.H{
		"success":         true,
		"needs_migration": len(mappings) > 0,
		"count":           len(mappings),
		"mappings":        mappings,
	})
}

func (h *Handler) ExecuteMigration(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil && !strings.Contains(strings.ToLower(err.Error()), "eof") {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}

	backup := true
	autoCleanup := true
	if raw, exists := payload["backup"]; exists {
		if parsed, ok := raw.(bool); ok {
			backup = parsed
		}
	}
	if raw, exists := payload["auto_cleanup"]; exists {
		if parsed, ok := raw.(bool); ok {
			autoCleanup = parsed
		}
	}

	mappings := h.settings.BuildDownloaderIDMappings()
	if len(mappings) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success":        true,
			"message":        "无需迁移",
			"migrated":       false,
			"migrated_count": 0,
			"backup":         backup,
			"auto_cleanup":   autoCleanup,
		})
		return
	}

	if err := h.torrents.EnsureDownloaderMigrationTable(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "创建迁移表失败: " + err.Error()})
		return
	}

	tableUpdates := map[string]int64{"traffic_stats": 0, "torrents": 0, "torrent_upload_stats": 0}
	for _, mapping := range mappings {
		if err := h.torrents.UpsertDownloaderMigration(mapping.OldID, mapping.NewID, mapping.Host, mapping.Name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存迁移映射失败: " + err.Error()})
			return
		}

		updated, err := h.torrents.UpdateDownloaderIDAcrossTables(mapping.OldID, mapping.NewID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "迁移数据库记录失败: " + err.Error()})
			return
		}
		for tableName, count := range updated {
			tableUpdates[tableName] += count
		}
	}

	configUpdatedCount, err := h.settings.ApplyDownloaderIDMappings(mappings)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":              true,
		"message":              "迁移成功完成",
		"migrated":             true,
		"migrated_count":       len(mappings),
		"config_updated_count": configUpdatedCount,
		"mappings":             mappings,
		"table_updates":        tableUpdates,
		"backup":               backup,
		"auto_cleanup":         autoCleanup,
	})
}

func (h *Handler) MigrationHistory(c *gin.Context) {
	records, err := h.torrents.ListDownloaderMigrationHistory()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取迁移历史失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"records": records,
		"history": records,
	})
}
