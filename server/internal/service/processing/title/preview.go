package title

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/pt-nexus/server-go/internal/config"
	"github.com/pt-nexus/server-go/internal/platform/logx"
	"gopkg.in/yaml.v3"
)

const titlePreviewLogModule = "迁移-标题预览"

var titleComponentOrderCache sync.Map

// BuildPreviewTitleFromTitleComponents 根据 title_components 拼接 “主标题(预览)”。
// 参数/返回：titleComponents 为前端/后端生成的组件数组；fallbackTitle 用于缺失主标题时兜底；返回拼接后的预览标题。
// 失败场景：读取 default_title_components 失败时使用内置默认顺序，不会返回错误。
// 副作用：读取 `configs/global_mappings.yaml` 并缓存默认字段顺序。
func BuildPreviewTitleFromTitleComponents(titleComponents []any, fallbackTitle string) string {
	params := map[string]any{}
	for _, item := range titleComponents {
		component, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key := strings.TrimSpace(toStringAny(component["key"], ""))
		if key == "" {
			continue
		}
		valueAny := component["value"]
		if valueAny == nil {
			continue
		}
		switch typed := valueAny.(type) {
		case []any:
			parts := make([]string, 0, len(typed))
			for _, part := range typed {
				text := strings.TrimSpace(toStringAny(part, ""))
				if text != "" {
					parts = append(parts, text)
				}
			}
			if len(parts) > 0 {
				params[key] = strings.Join(parts, " ")
			}
		case []string:
			parts := make([]string, 0, len(typed))
			for _, part := range typed {
				text := strings.TrimSpace(part)
				if text != "" {
					parts = append(parts, text)
				}
			}
			if len(parts) > 0 {
				params[key] = strings.Join(parts, " ")
			}
		default:
			value := strings.TrimSpace(toStringAny(valueAny, ""))
			if value != "" {
				params[key] = value
			}
		}
	}

	order := defaultTitleComponentsOrder()
	titleParts := make([]string, 0, len(order))
	for _, key := range order {
		value := strings.TrimSpace(toStringAny(params[key], ""))
		if value == "" {
			continue
		}
		titleParts = append(titleParts, value)
	}

	mainPart := normalizeTitleForDisplayLocal(strings.Join(titleParts, " "))
	if strings.TrimSpace(mainPart) == "" {
		mainPart = normalizeTitleForDisplayLocal(strings.TrimSpace(fallbackTitle))
	}

	releaseGroup := strings.TrimSpace(toStringAny(params["制作组"], "NOGROUP"))
	if releaseGroup == "" {
		releaseGroup = "NOGROUP"
	}
	if strings.Contains(releaseGroup, "N/A") {
		releaseGroup = "NOGROUP"
	}

	// 对特殊制作组进行处理，不需要添加前缀连字符（对齐 Python）。
	switch releaseGroup {
	case "MNHD-FRDS", "mUHD-FRDS":
		return strings.TrimSpace(mainPart + " " + releaseGroup)
	default:
		return strings.TrimSpace(mainPart + "-" + releaseGroup)
	}
}

func defaultTitleComponentsOrder() []string {
	fallback := []string{
		"主标题",
		"季集",
		"年份",
		"剧集状态",
		"发布版本",
		"分辨率",
		"片源平台",
		"媒介",
		"帧率",
		"视频编码",
		"视频格式",
		"HDR格式",
		"色深",
		"音频编码",
	}

	paths := config.ResolveRuntimePaths()
	mappingPath := strings.TrimSpace(paths.GlobalMapYML)
	if mappingPath == "" || filepath.Clean(mappingPath) == "." {
		return fallback
	}
	cacheKey := "order:" + mappingPath
	if cached, ok := titleComponentOrderCache.Load(cacheKey); ok {
		if order, ok := cached.([]string); ok && len(order) > 0 {
			return order
		}
	}

	content, err := os.ReadFile(mappingPath)
	if err != nil {
		return fallback
	}
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return fallback
	}
	if len(root.Content) == 0 {
		return fallback
	}
	doc := root.Content[0]
	if doc == nil || doc.Kind != yaml.MappingNode {
		return fallback
	}

	node := mappingValueNode(doc, "default_title_components")
	if node == nil || node.Kind != yaml.MappingNode {
		return fallback
	}

	order := make([]string, 0, len(node.Content)/2)
	for idx := 0; idx+1 < len(node.Content); idx += 2 {
		valueNode := node.Content[idx+1]
		if valueNode == nil || valueNode.Kind != yaml.MappingNode {
			continue
		}
		sourceKey := mappingValueNode(valueNode, "source_key")
		if sourceKey == nil {
			continue
		}
		key := strings.TrimSpace(sourceKey.Value)
		if key == "" {
			continue
		}
		order = append(order, key)
	}
	if len(order) == 0 {
		logx.Debugf(titlePreviewLogModule, "读取 default_title_components 为空，使用默认顺序 path=%s", mappingPath)
		return fallback
	}

	titleComponentOrderCache.Store(cacheKey, order)
	return order
}

func mappingValueNode(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for idx := 0; idx+1 < len(mapping.Content); idx += 2 {
		k := mapping.Content[idx]
		v := mapping.Content[idx+1]
		if k == nil || v == nil {
			continue
		}
		if strings.TrimSpace(k.Value) == key {
			return v
		}
	}
	return nil
}

func normalizeTitleForDisplayLocal(title string) string {
	runes := []rune(strings.TrimSpace(title))
	if len(runes) == 0 {
		return ""
	}
	for i, r := range runes {
		if r != '.' {
			continue
		}
		prevDigit := i > 0 && unicode.IsDigit(runes[i-1])
		nextDigit := i+1 < len(runes) && unicode.IsDigit(runes[i+1])
		if !(prevDigit && nextDigit) && !isVideoCodecDot(runes, i) {
			runes[i] = ' '
		}
	}
	return strings.Join(strings.Fields(string(runes)), " ")
}

func isVideoCodecDot(runes []rune, dotIndex int) bool {
	if dotIndex <= 0 || dotIndex+3 >= len(runes) {
		return false
	}
	if unicode.ToUpper(runes[dotIndex-1]) != 'H' {
		return false
	}
	if !unicode.IsDigit(runes[dotIndex+1]) || !unicode.IsDigit(runes[dotIndex+2]) || !unicode.IsDigit(runes[dotIndex+3]) {
		return false
	}
	codecSuffix := string([]rune{runes[dotIndex+1], runes[dotIndex+2], runes[dotIndex+3]})
	return codecSuffix == "264" || codecSuffix == "265" || codecSuffix == "266"
}

func toStringAny(value any, fallback string) string {
	switch typed := value.(type) {
	case nil:
		return fallback
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
		return fallback
	}
}
