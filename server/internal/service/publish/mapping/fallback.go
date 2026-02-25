package mapping

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pt-nexus/server-go/internal/config"
	"github.com/pt-nexus/server-go/internal/platform/logx"
	"gopkg.in/yaml.v3"
)

const publishFallbackLogModule = "发布-参数降级"

type fallbackConfig struct {
	Enabled             bool `yaml:"enabled"`
	LogFallback         bool `yaml:"log_fallback"`
	MaxFallbackDepth    int  `yaml:"max_fallback_depth"`
	UseDefaultOnFailure bool `yaml:"use_default_on_failure"`
}

type globalFallbackRoot struct {
	FallbackChains map[string]map[string][]string `yaml:"fallback_chains"`
	FallbackConfig fallbackConfig                 `yaml:"fallback_config"`
}

type fallbackRuntime struct {
	Enabled             bool
	LogFallback         bool
	MaxDepth            int
	UseDefaultOnFailure bool
	Chains              map[string]map[string][]string
}

var fallbackRuntimeCache sync.Map

func loadFallbackRuntime() (fallbackRuntime, bool) {
	paths := config.ResolveRuntimePaths()
	mappingPath := strings.TrimSpace(paths.GlobalMapYML)
	if mappingPath == "" || filepath.Clean(mappingPath) == "." {
		return fallbackRuntime{}, false
	}
	cacheKey := "publishFallback:" + mappingPath
	if cached, ok := fallbackRuntimeCache.Load(cacheKey); ok {
		if rt, ok := cached.(fallbackRuntime); ok {
			return rt, true
		}
	}

	data, err := os.ReadFile(mappingPath)
	if err != nil {
		logx.Debugf(publishFallbackLogModule, "读取降级配置失败 path=%s err=%v", mappingPath, err)
		return fallbackRuntime{}, false
	}

	root := globalFallbackRoot{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		logx.Debugf(publishFallbackLogModule, "解析降级配置失败 path=%s err=%v", mappingPath, err)
		return fallbackRuntime{}, false
	}

	maxDepth := root.FallbackConfig.MaxFallbackDepth
	if maxDepth <= 0 {
		maxDepth = 5
	}

	rt := fallbackRuntime{
		Enabled:             root.FallbackConfig.Enabled,
		LogFallback:         root.FallbackConfig.LogFallback,
		MaxDepth:            maxDepth,
		UseDefaultOnFailure: root.FallbackConfig.UseDefaultOnFailure,
		Chains:              root.FallbackChains,
	}
	fallbackRuntimeCache.Store(cacheKey, rt)
	return rt, true
}

func pickMappingValueIgnoreCase(mapping map[string]string, key string) (string, bool) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" || len(mapping) == 0 {
		return "", false
	}
	if direct, ok := mapping[trimmed]; ok {
		value := strings.TrimSpace(direct)
		if value != "" {
			return value, true
		}
	}
	for k, v := range mapping {
		if strings.EqualFold(strings.TrimSpace(k), trimmed) {
			value := strings.TrimSpace(v)
			if value != "" {
				return value, true
			}
			return "", false
		}
	}
	return "", false
}

func pickFallbackChainIgnoreCase(rt fallbackRuntime, paramType string, standardKey string) []string {
	if rt.Chains == nil {
		return nil
	}
	typeMappings, ok := rt.Chains[paramType]
	if !ok {
		for k, v := range rt.Chains {
			if strings.EqualFold(strings.TrimSpace(k), strings.TrimSpace(paramType)) {
				typeMappings = v
				ok = true
				break
			}
		}
	}
	if !ok || typeMappings == nil {
		return nil
	}

	if chain, ok := typeMappings[standardKey]; ok {
		return chain
	}
	for k, v := range typeMappings {
		if strings.EqualFold(strings.TrimSpace(k), strings.TrimSpace(standardKey)) {
			return v
		}
	}
	return nil
}

func pickMappedValueWithFallback(paramType string, mapping map[string]string, standardized string, useDefault bool, returnOriginalOnFailure bool) string {
	trimmed := strings.TrimSpace(standardized)
	if trimmed == "" {
		return ""
	}
	if len(mapping) == 0 {
		if returnOriginalOnFailure {
			return trimmed
		}
		return ""
	}

	if mapped, ok := pickMappingValueIgnoreCase(mapping, trimmed); ok {
		return mapped
	}

	rt, hasRuntime := loadFallbackRuntime()

	if hasRuntime && rt.Enabled {
		chain := pickFallbackChainIgnoreCase(rt, paramType, trimmed)
		if len(chain) > 0 {
			limited := chain
			if rt.MaxDepth > 0 && len(limited) > rt.MaxDepth {
				limited = limited[:rt.MaxDepth]
			}

			for _, fallbackKey := range limited {
				key := strings.TrimSpace(fallbackKey)
				if key == "" {
					continue
				}
				if mapped, ok := pickMappingValueIgnoreCase(mapping, key); ok {
					if rt.LogFallback {
						logx.Infof(publishFallbackLogModule, "参数降级成功 type=%s 原始=%s 降级=%s", strings.TrimSpace(paramType), trimmed, key)
					}
					return mapped
				}
			}

			if rt.LogFallback {
				logx.Debugf(publishFallbackLogModule, "参数降级失败 type=%s 原始=%s depth=%d", strings.TrimSpace(paramType), trimmed, len(limited))
			}
		}
	}

	if useDefault {
		if mapped, ok := pickMappingValueIgnoreCase(mapping, "default"); ok {
			return mapped
		}
	}

	if returnOriginalOnFailure {
		return trimmed
	}
	return ""
}

// PickMappedValueWithFallback 基于参数类型执行发布值映射并自动降级。
// 参数/返回：paramType 为参数类型（如 audio_codec）；mapping 为站点映射；standardized 为标准化值；返回可提交到目标站点的字段值。
// 失败场景：映射缺失、降级链未命中且 default 缺失时返回空字符串。
// 副作用：可能读取 global_mappings.yaml 并按配置输出降级日志。
func PickMappedValueWithFallback(paramType string, mapping map[string]string, standardized string) string {
	useDefault := true
	if rt, ok := loadFallbackRuntime(); ok {
		useDefault = rt.UseDefaultOnFailure
	}
	return strings.TrimSpace(pickMappedValueWithFallback(paramType, mapping, standardized, useDefault, false))
}

// PickMappedValueWithFallbackNoDefault 基于参数类型执行发布值映射并自动降级（不使用 default）。
// 参数/返回：paramType 为参数类型（如 tag）；mapping 为站点映射；standardized 为标准化值；返回可提交到目标站点的字段值。
// 失败场景：映射缺失或降级链未命中时返回空字符串。
// 副作用：可能读取 global_mappings.yaml 并按配置输出降级日志。
func PickMappedValueWithFallbackNoDefault(paramType string, mapping map[string]string, standardized string) string {
	return strings.TrimSpace(pickMappedValueWithFallback(paramType, mapping, standardized, false, false))
}
