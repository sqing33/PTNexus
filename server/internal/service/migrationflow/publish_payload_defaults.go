package migrationflow

import (
	"strings"

	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
)

// normalizePublishPayloadWithCrossSeedDefaults 统一补齐发布参数中的转种默认配置。
// 参数/返回：payload 为上层传入发布参数；返回新 map，避免修改调用方原始引用。
// 失败场景：服务配置缺失时回退到内置默认值，不返回错误。
// 副作用：无。
func (s *MigrateService) normalizePublishPayloadWithCrossSeedDefaults(payload map[string]any) map[string]any {
	normalizedPayload := map[string]any{}
	for key, value := range payload {
		normalizedPayload[key] = value
	}

	rootConfig := map[string]any{}
	if s != nil && s.cfg != nil {
		rootConfig = s.cfg.Get()
	}

	crossSeed := map[string]any{}
	if rootConfig != nil {
		if item, ok := rootConfig["cross_seed"].(map[string]any); ok && item != nil {
			crossSeed = item
		}
	}

	// 对齐 Python：auto_add_existing_to_downloader 默认取 cross_seed 配置（缺失时按 true 处理）。
	if _, exists := normalizedPayload["auto_add_existing_to_downloader"]; !exists {
		if _, existsCamel := normalizedPayload["autoAddExistingToDownloader"]; !existsCamel {
			if raw, ok := crossSeed["auto_add_existing_to_downloader"]; ok {
				normalizedPayload["auto_add_existing_to_downloader"] = raw
			} else {
				normalizedPayload["auto_add_existing_to_downloader"] = true
			}
		}
	}

	// 对齐 Go 版扩展：auto_update_existing_torrent 默认取 cross_seed 配置（缺失时按 false 处理）。
	if _, exists := normalizedPayload["auto_update_existing_torrent"]; !exists {
		if _, existsCamel := normalizedPayload["autoUpdateExistingTorrent"]; !existsCamel {
			if raw, ok := crossSeed["auto_update_existing_torrent"]; ok {
				normalizedPayload["auto_update_existing_torrent"] = raw
			} else {
				normalizedPayload["auto_update_existing_torrent"] = false
			}
		}
	}

	// 对齐 Python：当 cross_seed.default_downloader 有值时，发布后自动添加优先使用该下载器。
	if defaultID := strings.TrimSpace(processingshared.ToString(crossSeed["default_downloader"], "")); defaultID != "" {
		normalizedPayload["useDefaultDownloader"] = true
		normalizedPayload["use_default_downloader"] = true
	}

	return normalizedPayload
}
