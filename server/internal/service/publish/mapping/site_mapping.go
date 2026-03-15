package mapping

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/pt-nexus/server/internal/config"
	"gopkg.in/yaml.v3"
)

// SitePublishConfig 表示站点发布映射配置。
type SitePublishConfig struct {
	FormFields         map[string]string
	Mappings           map[string]map[string]string
	GenreOptionsByType map[string][]string
	Anonymous          SiteAnonymousConfig
}

// SiteAnonymousConfig 表示站点匿名发布字段配置。
type SiteAnonymousConfig struct {
	Field            string
	EnabledValue     string
	DisabledValue    string
	OmitWhenDisabled bool
}

var publishConfigCache sync.Map

// LoadSitePublishConfig 读取并缓存站点发布配置。
func LoadSitePublishConfig(siteCode string) (*SitePublishConfig, error) {
	trimmed := strings.ToLower(strings.TrimSpace(siteCode))
	if trimmed == "" {
		return nil, errors.New("site code 为空")
	}
	if cached, ok := publishConfigCache.Load(trimmed); ok {
		if cfg, ok := cached.(*SitePublishConfig); ok {
			return cfg, nil
		}
	}

	paths := config.ResolveRuntimePaths()
	candidates := []string{
		filepath.Join(paths.BaseDir, "configs", trimmed+".yaml"),
	}

	var data []byte
	var readErr error
	for _, candidate := range candidates {
		content, err := os.ReadFile(candidate)
		if err == nil {
			data = content
			readErr = nil
			break
		}
		readErr = err
	}
	if len(data) == 0 {
		if readErr == nil {
			readErr = errors.New("文件不存在")
		}
		return nil, fmt.Errorf("读取站点配置失败: %w", readErr)
	}

	raw, err := unmarshalYAMLMapAllowDuplicateKeys(data)
	if err != nil {
		return nil, err
	}
	cfg := &SitePublishConfig{
		FormFields:         mapStringMap(raw["form_fields"]),
		Mappings:           mapStringNestedMap(raw["mappings"]),
		GenreOptionsByType: mapStringSliceNestedMap(raw["genre_options_by_type"]),
		Anonymous:          mapAnonymousConfig(raw["anonymous"]),
	}
	publishConfigCache.Store(trimmed, cfg)
	return cfg, nil
}

func mapAnonymousConfig(value any) SiteAnonymousConfig {
	item, ok := value.(map[string]any)
	if !ok {
		if direct, ok := value.(map[string]interface{}); ok {
			item = direct
		} else {
			return SiteAnonymousConfig{}
		}
	}
	return SiteAnonymousConfig{
		Field:            strings.TrimSpace(toStringAny(item["field"])),
		EnabledValue:     strings.TrimSpace(toStringAny(item["enabled_value"])),
		DisabledValue:    strings.TrimSpace(toStringAny(item["disabled_value"])),
		OmitWhenDisabled: toBoolAny(item["omit_when_disabled"]),
	}
}

func mapStringMap(value any) map[string]string {
	result := map[string]string{}
	item, ok := value.(map[string]any)
	if !ok {
		if direct, ok := value.(map[string]interface{}); ok {
			for key, raw := range direct {
				text := strings.TrimSpace(toStringAny(raw))
				if text != "" {
					result[key] = text
				}
			}
		}
		return result
	}
	for key, raw := range item {
		text := strings.TrimSpace(toStringAny(raw))
		if text != "" {
			result[key] = text
		}
	}
	return result
}

func mapStringNestedMap(value any) map[string]map[string]string {
	result := map[string]map[string]string{}
	item, ok := value.(map[string]any)
	if !ok {
		if direct, ok := value.(map[string]interface{}); ok {
			for key, raw := range direct {
				result[key] = mapStringMap(raw)
			}
		}
		return result
	}
	for key, raw := range item {
		result[key] = mapStringMap(raw)
	}
	return result
}

func mapStringSliceNestedMap(value any) map[string][]string {
	result := map[string][]string{}
	item, ok := value.(map[string]any)
	if !ok {
		if direct, ok := value.(map[string]interface{}); ok {
			for key, raw := range direct {
				result[key] = toStringSlice(raw)
			}
		}
		return result
	}
	for key, raw := range item {
		result[key] = toStringSlice(raw)
	}
	return result
}

func toStringSlice(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(toStringAny(item)); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	default:
		if trimmed := strings.TrimSpace(toStringAny(value)); trimmed != "" {
			return []string{trimmed}
		}
		return nil
	}
}

// PickMappedValue 将标准值映射为站点字段值。
func PickMappedValue(mapping map[string]string, standardized string) string {
	trimmed := strings.TrimSpace(standardized)
	if trimmed == "" {
		return ""
	}
	if len(mapping) == 0 {
		return ""
	}
	if mapped, ok := mapping[trimmed]; ok && strings.TrimSpace(mapped) != "" {
		return strings.TrimSpace(mapped)
	}
	if mapped, ok := mapping["default"]; ok && strings.TrimSpace(mapped) != "" {
		return strings.TrimSpace(mapped)
	}
	return ""
}

func unmarshalYAMLMapAllowDuplicateKeys(data []byte) (map[string]any, error) {
	document := yaml.Node{}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 0 {
		return map[string]any{}, nil
	}
	value, err := yamlNodeToAny(document.Content[0])
	if err != nil {
		return nil, err
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("站点配置根节点不是对象")
	}
	return raw, nil
}

func yamlNodeToAny(node *yaml.Node) (any, error) {
	if node == nil {
		return nil, nil
	}

	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return map[string]any{}, nil
		}
		return yamlNodeToAny(node.Content[0])
	case yaml.MappingNode:
		result := map[string]any{}
		for idx := 0; idx+1 < len(node.Content); idx += 2 {
			keyNode := node.Content[idx]
			valueNode := node.Content[idx+1]
			key := strings.TrimSpace(keyNode.Value)
			if key == "" {
				continue
			}
			value, err := yamlNodeToAny(valueNode)
			if err != nil {
				return nil, err
			}
			// 对齐 Python yaml.safe_load：重复键后值覆盖前值。
			result[key] = value
		}
		return result, nil
	case yaml.SequenceNode:
		result := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			value, err := yamlNodeToAny(child)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		return result, nil
	case yaml.ScalarNode:
		return yamlScalarToAny(node), nil
	case yaml.AliasNode:
		if node.Alias == nil {
			return nil, nil
		}
		return yamlNodeToAny(node.Alias)
	default:
		return nil, nil
	}
}

func yamlScalarToAny(node *yaml.Node) any {
	value := strings.TrimSpace(node.Value)

	switch node.Tag {
	case "!!null":
		return nil
	case "!!bool":
		if parsed, err := strconv.ParseBool(strings.ToLower(value)); err == nil {
			return parsed
		}
		return strings.EqualFold(value, "true")
	case "!!int":
		if parsed, err := strconv.ParseInt(value, 0, 64); err == nil {
			return parsed
		}
		return value
	case "!!float":
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
		}
		return value
	default:
		return node.Value
	}
}

func toStringAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int8:
		return fmt.Sprintf("%d", typed)
	case int16:
		return fmt.Sprintf("%d", typed)
	case int32:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case uint:
		return fmt.Sprintf("%d", typed)
	case uint8:
		return fmt.Sprintf("%d", typed)
	case uint16:
		return fmt.Sprintf("%d", typed)
	case uint32:
		return fmt.Sprintf("%d", typed)
	case uint64:
		return fmt.Sprintf("%d", typed)
	case float32:
		return fmt.Sprintf("%v", typed)
	case float64:
		return fmt.Sprintf("%v", typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprintf("%v", value)
	}
}

func toBoolAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		lower := strings.ToLower(strings.TrimSpace(typed))
		return lower == "1" || lower == "true" || lower == "yes" || lower == "y"
	case int:
		return typed != 0
	case int8:
		return typed != 0
	case int16:
		return typed != 0
	case int32:
		return typed != 0
	case int64:
		return typed != 0
	case uint:
		return typed != 0
	case uint8:
		return typed != 0
	case uint16:
		return typed != 0
	case uint32:
		return typed != 0
	case uint64:
		return typed != 0
	case float32:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return false
	}
}
