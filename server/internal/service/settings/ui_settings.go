package settings

import (
	"fmt"
	"strings"
)

func normalizeTorrentsViewFilters(view map[string]any) {
	// Python 侧直接返回 config.json 的 torrents_view，不会把默认 active_filters 深度合并进去。
	// Go 的默认配置会注入 siteExistence/siteNames，这会导致合同校验不一致。
	activeFilters, ok := view["active_filters"].(map[string]any)
	if !ok {
		return
	}

	_, hasExist := activeFilters["existSiteNames"]
	_, hasNotExist := activeFilters["notExistSiteNames"]
	if hasExist || hasNotExist {
		delete(activeFilters, "siteExistence")
		delete(activeFilters, "siteNames")
		view["active_filters"] = activeFilters
	}
}

func (s *SettingsService) GetTorrentsUIViewSettings() map[string]any {
	defaults := map[string]any{
		"page_size":      50,
		"sort_prop":      "name",
		"sort_order":     "ascending",
		"name_search":    "",
		"active_filters": map[string]any{"paths": []any{}, "states": []any{}, "siteExistence": "all", "siteNames": []any{}, "downloaderIds": []any{}},
	}
	cfg := s.cfg.Get()
	ui, ok := cfg["ui_settings"].(map[string]any)
	if ok {
		if view, ok := ui["torrents_view"].(map[string]any); ok {
			normalizeTorrentsViewFilters(view)
			return view
		}
	}
	return defaults
}

func (s *SettingsService) SaveTorrentsUIViewSettings(newSettings map[string]any) error {
	cfg := s.cfg.Get()
	ui := ensureMap(cfg, "ui_settings")
	ui["torrents_view"] = newSettings
	cfg["ui_settings"] = ui
	return s.cfg.Save(cfg)
}

func (s *SettingsService) GetCrossSeedUIViewSettings() map[string]any {
	defaults := map[string]any{
		"page_size":    20,
		"search_query": "",
		"active_filters": map[string]any{
			"savePath":  "",
			"isDeleted": "",
		},
	}
	cfg := s.cfg.Get()
	ui, ok := cfg["ui_settings"].(map[string]any)
	if ok {
		if view, ok := ui["cross_seed_view"].(map[string]any); ok {
			return view
		}
	}
	return defaults
}

func (s *SettingsService) SaveCrossSeedUIViewSettings(newSettings map[string]any) error {
	cfg := s.cfg.Get()
	ui := ensureMap(cfg, "ui_settings")
	ui["cross_seed_view"] = newSettings
	cfg["ui_settings"] = ui
	return s.cfg.Save(cfg)
}

func (s *SettingsService) GetUploadSettings() map[string]any {
	defaults := map[string]any{"anonymous_upload": true}
	cfg := s.cfg.Get()
	if settings, ok := cfg["upload_settings"].(map[string]any); ok {
		return settings
	}
	return defaults
}

func (s *SettingsService) SaveUploadSettings(newSettings map[string]any) error {
	cfg := s.cfg.Get()
	cfg["upload_settings"] = newSettings
	return s.cfg.Save(cfg)
}

func (s *SettingsService) GetIYUUSettings() map[string]any {
	defaults := map[string]any{"path_filter_enabled": false, "selected_paths": []any{}}
	cfg := s.cfg.Get()
	if settings, ok := cfg["iyuu_settings"].(map[string]any); ok {
		return settings
	}
	return defaults
}

func (s *SettingsService) SaveIYUUSettings(newSettings map[string]any) error {
	cfg := s.cfg.Get()
	cfg["iyuu_settings"] = newSettings
	return s.cfg.Save(cfg)
}

func (s *SettingsService) TriggerIYUUQuery() map[string]any {
	s.logMu.RLock()
	trigger := s.iyuuTrigger
	s.logMu.RUnlock()

	if trigger == nil {
		message := fmt.Sprintf("IYUU 触发器未配置（%s）", nowString())
		s.logMu.Lock()
		s.iyuuLogs = append(s.iyuuLogs, message)
		if len(s.iyuuLogs) > 500 {
			s.iyuuLogs = s.iyuuLogs[len(s.iyuuLogs)-500:]
		}
		s.logMu.Unlock()
		return map[string]any{"success": false, "message": message}
	}

	result := trigger()
	success := toBool(result["success"], false)
	message := strings.TrimSpace(toString(result["message"], ""))
	if message == "" {
		if success {
			message = fmt.Sprintf("IYUU 查询任务触发成功（%s）", nowString())
		} else {
			message = fmt.Sprintf("IYUU 查询任务触发失败（%s）", nowString())
		}
	}
	logMessage := fmt.Sprintf("[%s] %s", map[bool]string{true: "SUCCESS", false: "ERROR"}[success], message)

	s.logMu.Lock()
	s.iyuuLogs = append(s.iyuuLogs, logMessage)
	if len(s.iyuuLogs) > 500 {
		s.iyuuLogs = s.iyuuLogs[len(s.iyuuLogs)-500:]
	}
	s.logMu.Unlock()

	result["message"] = message
	return result
}

func (s *SettingsService) GetIYUULogs() []string {
	s.logMu.RLock()
	defer s.logMu.RUnlock()
	copied := make([]string, len(s.iyuuLogs))
	copy(copied, s.iyuuLogs)
	return copied
}
