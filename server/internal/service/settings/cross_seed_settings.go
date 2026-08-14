package settings

import (
	"fmt"
	"runtime"
	"strings"
)

func (s *SettingsService) GetCrossSeedSettings() map[string]any {
	defaults := map[string]any{
		"image_hoster":                     "pixhost",
		"pixhost_domain":                   "img2.pixhost.cc",
		"agsv_email":                       "",
		"agsv_password":                    "",
		"default_downloader":               "",
		"auto_add_existing_to_downloader":  true,
		"auto_update_existing_torrent":     false,
		"publish_batch_concurrency_mode":   "cpu",
		"publish_batch_concurrency_manual": BatchPublishDefaultConcurrency,
	}
	cfg := s.cfg.Get()
	return mergeWithDefault(defaults, nestedMap(cfg, "cross_seed"))
}

func (s *SettingsService) SaveCrossSeedSettings(newSettings map[string]any) error {
	cfg := s.cfg.Get()
	existing := nestedMap(cfg, "cross_seed")
	merged := mergeWithDefault(existing, newSettings)

	mode := toString(merged["publish_batch_concurrency_mode"], "cpu")
	if mode != "cpu" && mode != "manual" && mode != "all" {
		mode = "cpu"
	}
	merged["publish_batch_concurrency_mode"] = mode
	manual := toIntWithDefault(merged["publish_batch_concurrency_manual"], BatchPublishDefaultConcurrency)
	if manual < 1 {
		manual = 1
	}
	merged["publish_batch_concurrency_manual"] = manual
	merged["auto_add_existing_to_downloader"] = toBool(merged["auto_add_existing_to_downloader"], true)
	merged["auto_update_existing_torrent"] = toBool(merged["auto_update_existing_torrent"], false)
	if toString(merged["image_hoster"], "") == "" {
		merged["image_hoster"] = "pixhost"
	}
	merged["pixhost_domain"] = normalizePixhostDomainSetting(merged["pixhost_domain"])

	cfg["cross_seed"] = merged
	return s.cfg.Save(cfg)
}

func normalizePixhostDomainSetting(value any) string {
	domain := strings.TrimSpace(toString(value, ""))
	if domain == "" {
		return "img2.pixhost.cc"
	}
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "https://")
	if i := strings.IndexAny(domain, "/?#"); i >= 0 {
		domain = domain[:i]
	}
	domain = strings.ToLower(strings.Trim(domain, ". "))
	if domain == "" {
		return "img2.pixhost.cc"
	}
	if strings.HasPrefix(domain, "api.") {
		domain = "img2." + strings.TrimPrefix(domain, "api.")
	}
	if domain == "pixhost.to" || domain == "pixhost.cc" {
		domain = "img2." + domain
	}
	return domain
}

func (s *SettingsService) PublishConcurrencyInfo() map[string]any {
	cpuThreads := 1
	if detected := runtime.NumCPU(); detected > 0 {
		cpuThreads = detected
	}
	suggested := cpuThreads * 2
	effective := suggested
	if effective > BatchPublishMaxConcurrency {
		effective = BatchPublishMaxConcurrency
	}
	if effective < 1 {
		effective = 1
	}

	return map[string]any{
		"success":                         true,
		"cpu_threads":                     cpuThreads,
		"suggested_concurrency":           suggested,
		"effective_suggested_concurrency": effective,
		"max_concurrency":                 BatchPublishMaxConcurrency,
		"default_concurrency":             BatchPublishDefaultConcurrency,
	}
}

func (s *SettingsService) GetSourcePriority() []any {
	cfg := s.cfg.Get()
	return toSlice(cfg["source_priority"])
}

func (s *SettingsService) SaveSourcePriority(items []any) error {
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return fmt.Errorf("源站点优先级数组中的元素必须是字符串")
		}
	}
	cfg := s.cfg.Get()
	cfg["source_priority"] = items
	return s.cfg.Save(cfg)
}

func (s *SettingsService) GetBatchFetchFilters() map[string]any {
	defaults := map[string]any{"paths": []any{}, "states": []any{}, "downloaderIds": []any{}}
	cfg := s.cfg.Get()
	return mergeWithDefault(defaults, nestedMap(cfg, "batch_fetch_filters"))
}

func (s *SettingsService) SaveBatchFetchFilters(filters map[string]any) error {
	required := []string{"paths", "states", "downloaderIds"}
	for _, key := range required {
		value, exists := filters[key]
		if !exists {
			return fmt.Errorf("缺少必需字段: %s", key)
		}
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("字段 %s 必须是数组", key)
		}
	}
	cfg := s.cfg.Get()
	cfg["batch_fetch_filters"] = filters
	return s.cfg.Save(cfg)
}

func (s *SettingsService) GetCrossSeedReviewFilter() string {
	cfg := s.cfg.Get()
	crossSeed := nestedMap(cfg, "cross_seed")
	return toString(crossSeed["review_filter"], "")
}

func (s *SettingsService) SaveCrossSeedReviewFilter(filter string) error {
	valid := map[string]struct{}{"": {}, "reviewed": {}, "unreviewed": {}, "error": {}}
	if _, ok := valid[filter]; !ok {
		return fmt.Errorf("无效的检查状态筛选值: %s", filter)
	}
	cfg := s.cfg.Get()
	crossSeed := ensureMap(cfg, "cross_seed")
	crossSeed["review_filter"] = filter
	cfg["cross_seed"] = crossSeed
	return s.cfg.Save(cfg)
}

func (s *SettingsService) GetTagsConfig() map[string]any {
	defaults := map[string]any{
		"category": map[string]any{"enabled": true, "category": ""},
		"tags":     map[string]any{"enabled": true, "tags": []any{"PT Nexus", "站点/{站点名称}"}},
	}
	cfg := s.cfg.Get()
	return mergeWithDefault(defaults, nestedMap(cfg, "tags_config"))
}

func (s *SettingsService) SaveTagsConfig(data map[string]any) error {
	category, categoryOK := data["category"].(map[string]any)
	tags, tagsOK := data["tags"].(map[string]any)
	if !categoryOK {
		category = map[string]any{"enabled": true, "category": ""}
	}
	if !tagsOK {
		tags = map[string]any{"enabled": true, "tags": []any{"PT Nexus", "站点/{站点名称}"}}
	}
	cfg := s.cfg.Get()
	cfg["tags_config"] = map[string]any{"category": category, "tags": tags}
	return s.cfg.Save(cfg)
}
