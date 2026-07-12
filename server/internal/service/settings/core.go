package settings

import (
	"strings"
	"sync"

	"github.com/pt-nexus/server/internal/config"
)

const (
	BatchPublishMaxConcurrency     = 200
	BatchPublishDefaultConcurrency = 5
)

type SettingsService struct {
	cfg      *config.Manager
	dataDir  string
	logMu    sync.RWMutex
	iyuuLogs []string

	iyuuTrigger func() map[string]any
}

func NewSettingsService(cfg *config.Manager) *SettingsService {
	return &SettingsService{cfg: cfg, iyuuLogs: make([]string, 0)}
}

// NewSettingsServiceWithDataDir 创建设置服务并绑定数据目录（本地背景图等落盘依赖）。
// 参数/返回：cfg 配置管理器；dataDir 运行时数据目录；返回服务实例。
// 失败场景：无。
// 副作用：确保 backgrounds 目录存在。
func NewSettingsServiceWithDataDir(cfg *config.Manager, dataDir string) *SettingsService {
	service := NewSettingsService(cfg)
	service.SetDataDir(dataDir)
	return service
}

func (s *SettingsService) SetIYUUTrigger(trigger func() map[string]any) {
	s.logMu.Lock()
	s.iyuuTrigger = trigger
	s.logMu.Unlock()
}

// AppendIYUULog 追加一条 IYUU 日志到内存队列，供前端设置页查看。
// 参数/返回：level 为 INFO/WARN/ERROR 等等级；message 为日志内容；无返回值。
// 失败场景：无。
// 副作用：写入内存日志队列（最多保留 500 条）。
func (s *SettingsService) AppendIYUULog(level string, message string) {
	level = strings.TrimSpace(level)
	if level == "" {
		level = "INFO"
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	logMessage := "[" + level + "] " + message

	s.logMu.Lock()
	s.iyuuLogs = append(s.iyuuLogs, logMessage)
	if len(s.iyuuLogs) > 500 {
		s.iyuuLogs = s.iyuuLogs[len(s.iyuuLogs)-500:]
	}
	s.logMu.Unlock()
}

func (s *SettingsService) GetSettingsMasked() map[string]any {
	current := s.cfg.Get()
	for _, raw := range toSlice(current["downloaders"]) {
		downloader, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		downloader["password"] = ""
	}

	if cookieCloud, ok := current["cookiecloud"].(map[string]any); ok {
		cookieCloud["e2e_password"] = ""
		current["cookiecloud"] = cookieCloud
	}
	current["network_proxy"] = config.ParseNetworkProxyConfig(current["network_proxy"]).ToMap()

	// Align with Python behavior: do not leak Go-side default merges into UI settings output.
	if ui, ok := current["ui_settings"].(map[string]any); ok {
		if view, ok := ui["torrents_view"].(map[string]any); ok {
			normalizeTorrentsViewFilters(view)
			ui["torrents_view"] = view
			current["ui_settings"] = ui
		}
	}
	return current
}

func (s *SettingsService) UpdateSettings(newConfig map[string]any) error {
	current := s.cfg.Get()

	if rawProxy, exists := newConfig["network_proxy"]; exists {
		normalizedProxy, err := normalizeNetworkProxyValue(rawProxy)
		if err != nil {
			return err
		}
		newConfig["network_proxy"] = normalizedProxy.ToMap()
	}

	if rawDownloaders, exists := newConfig["downloaders"]; exists {
		updated := toSlice(rawDownloaders)
		currentPasswords := map[string]string{}
		for _, raw := range toSlice(current["downloaders"]) {
			downloader, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id := toString(downloader["id"], "")
			if id == "" {
				continue
			}
			currentPasswords[id] = toString(downloader["password"], "")
		}

		for index, raw := range updated {
			downloader, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id := toString(downloader["id"], "")
			if id == "" {
				continue
			}
			password := strings.TrimSpace(toString(downloader["password"], ""))
			if password == "" {
				downloader["password"] = currentPasswords[id]
			}
			updated[index] = downloader
		}
		newConfig["downloaders"] = updated
	}

	for key, value := range newConfig {
		if key == "ui_settings" {
			incomingUI, incomingOK := value.(map[string]any)
			currentUI, currentOK := current[key].(map[string]any)
			if incomingOK && currentOK {
				mergeSettingsMap(currentUI, incomingUI)
				current[key] = currentUI
				continue
			}
		}
		current[key] = value
	}

	if err := s.cfg.Save(current); err != nil {
		return err
	}

	if err := applyNetworkProxyRuntime(config.ParseNetworkProxyConfig(current["network_proxy"])); err != nil {
		return err
	}
	return nil
}

func mergeSettingsMap(destination map[string]any, source map[string]any) {
	for key, value := range source {
		sourceMap, sourceIsMap := value.(map[string]any)
		destinationMap, destinationIsMap := destination[key].(map[string]any)
		if sourceIsMap && destinationIsMap {
			mergeSettingsMap(destinationMap, sourceMap)
			destination[key] = destinationMap
			continue
		}
		destination[key] = value
	}
}

func (s *SettingsService) DownloadersList(enabledOnly bool) []map[string]any {
	cfg := s.cfg.Get()
	downloaders := toSlice(cfg["downloaders"])
	result := make([]map[string]any, 0, len(downloaders))
	for _, raw := range downloaders {
		downloader, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		enabled := toBool(downloader["enabled"], true)
		if enabledOnly && !enabled {
			continue
		}
		item := map[string]any{
			"id":      toString(downloader["id"], ""),
			"name":    toString(downloader["name"], ""),
			"enabled": enabled,
			"color":   toString(downloader["color"], ""),
		}
		if enabledOnly {
			delete(item, "enabled")
		}
		result = append(result, item)
	}
	return result
}

// DownloaderConfigs 返回下载器完整配置快照，供后端内部连接检测等流程使用。
// 参数/返回：返回值按配置文件中的顺序排列，字段包含连接探测所需的 id/type/host/认证/代理开关等。
// 失败场景：配置结构异常时会跳过异常项，不返回错误。
// 副作用：无副作用，仅从内存配置读取并构造副本。
func (s *SettingsService) DownloaderConfigs() []map[string]any {
	cfg := s.cfg.Get()
	rawDownloaders := toSlice(cfg["downloaders"])
	result := make([]map[string]any, 0, len(rawDownloaders))
	for _, raw := range rawDownloaders {
		downloader, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, map[string]any{
			"id":         toString(downloader["id"], ""),
			"name":       toString(downloader["name"], ""),
			"type":       toString(downloader["type"], ""),
			"host":       toString(downloader["host"], ""),
			"username":   toString(downloader["username"], ""),
			"password":   toString(downloader["password"], ""),
			"enabled":    toBool(downloader["enabled"], true),
			"use_proxy":  toBool(downloader["use_proxy"], false),
			"proxy_port": toIntWithDefault(downloader["proxy_port"], 9090),
		})
	}
	return result
}

func (s *SettingsService) BuildDownloaderInfo(trafficTotals, trafficToday map[string]map[string]int64) []map[string]any {
	downloaders := s.DownloadersList(false)
	cfg := s.cfg.Get()
	rawDownloaders := toSlice(cfg["downloaders"])
	configByID := map[string]map[string]any{}
	for _, raw := range rawDownloaders {
		if item, ok := raw.(map[string]any); ok {
			configByID[toString(item["id"], "")] = item
		}
	}

	result := make([]map[string]any, 0, len(downloaders))
	for _, downloader := range downloaders {
		dID := toString(downloader["id"], "")
		name := toString(downloader["name"], "")
		enabled := toBool(downloader["enabled"], false)
		typed := ""
		if raw, ok := configByID[dID]; ok {
			typed = toString(raw["type"], "")
		}

		details := map[string]any{}
		today := trafficToday[dID]
		totals := trafficTotals[dID]
		details["今日下载量"] = formatBytes(today["today_dl"])
		details["今日上传量"] = formatBytes(today["today_ul"])
		details["累计下载量"] = formatBytes(totals["total_dl"])
		details["累计上传量"] = formatBytes(totals["total_ul"])

		status := "未配置"
		if enabled {
			status = "已配置"
		}

		result = append(result, map[string]any{
			"id":      dID,
			"name":    name,
			"type":    typed,
			"enabled": enabled,
			"status":  status,
			"details": details,
		})
	}
	return result
}
