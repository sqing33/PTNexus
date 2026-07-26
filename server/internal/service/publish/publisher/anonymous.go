package publisher

import (
	"strings"

	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
)

// ResolveAnonymousUploadEnabled 读取全局上传设置中的匿名发布开关。
// 参数/返回：无参数，返回当前是否启用匿名发布；读取失败时默认返回 true。
// 失败场景：配置文件不存在、读取失败或字段缺失时，按默认启用匿名处理。
// 副作用：读取运行时配置文件。
func ResolveAnonymousUploadEnabled(rootConfigs ...map[string]any) bool {
	if len(rootConfigs) == 0 || rootConfigs[0] == nil {
		return true
	}
	root := rootConfigs[0]
	uploadSettings, ok := root["upload_settings"].(map[string]any)
	if !ok || uploadSettings == nil {
		return true
	}
	return boolFromAnyWithDefault(uploadSettings["anonymous_upload"], true)
}

// ApplyAnonymousFormFields 根据站点配置向公共发布器表单注入匿名字段。
// 参数/返回：siteCode 为目标站点 code，siteCfg 为站点发布配置，formFields 为最终表单字段集合。
// 失败场景：站点未配置匿名字段且无法命中默认规则时直接跳过，不返回错误。
// 副作用：会原地修改 formFields。
func ApplyAnonymousFormFields(siteCode string, siteCfg *publishmapping.SitePublishConfig, formFields map[string]string, rootConfigs ...map[string]any) {
	if formFields == nil {
		return
	}

	cfg := resolveAnonymousFieldConfig(siteCode, siteCfg)
	field := strings.TrimSpace(cfg.Field)
	if field == "" {
		return
	}

	enabled := ResolveAnonymousUploadEnabled(rootConfigs...)
	if enabled {
		value := strings.TrimSpace(cfg.EnabledValue)
		if value == "" {
			value = "yes"
		}
		formFields[field] = value
		return
	}

	if cfg.OmitWhenDisabled {
		delete(formFields, field)
		return
	}

	if value := strings.TrimSpace(cfg.DisabledValue); value != "" {
		formFields[field] = value
		return
	}

	delete(formFields, field)
}

func resolveAnonymousFieldConfig(siteCode string, siteCfg *publishmapping.SitePublishConfig) publishmapping.SiteAnonymousConfig {
	if siteCfg != nil {
		cfg := siteCfg.Anonymous
		if strings.TrimSpace(cfg.Field) != "" {
			return cfg
		}
		if raw := strings.TrimSpace(siteCfg.FormFields["anonymous"]); raw != "" {
			return publishmapping.SiteAnonymousConfig{
				Field:            raw,
				EnabledValue:     "yes",
				OmitWhenDisabled: true,
			}
		}
	}

	// NexusPHP 系公共发布器默认复选框字段：匿名开启提交 uplver=yes，关闭时省略字段。
	if strings.TrimSpace(siteCode) != "" {
		return publishmapping.SiteAnonymousConfig{
			Field:            "uplver",
			EnabledValue:     "yes",
			OmitWhenDisabled: true,
		}
	}
	return publishmapping.SiteAnonymousConfig{}
}

func boolFromAnyWithDefault(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case string:
		text := strings.ToLower(strings.TrimSpace(typed))
		if text == "" {
			return fallback
		}
		if text == "true" || text == "1" || text == "yes" || text == "y" {
			return true
		}
		if text == "false" || text == "0" || text == "no" || text == "n" {
			return false
		}
		return fallback
	default:
		return fallback
	}
}
