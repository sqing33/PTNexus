package media

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/platform/logx"
	"gopkg.in/yaml.v3"
)

const mediaFormatValidateLogModule = "媒体格式判定"

type mediaFormatKeywordConfig struct {
	mediainfoRequired []string
	mediainfoOptional []string
	bdinfoRequired    []string
	bdinfoOptional    []string
	forbiddenPatterns []patternConfig
}

type patternConfig struct {
	pattern     string
	description string
}

var (
	defaultMediainfoRequired = []string{"General", "Video", "Audio"}
	defaultMediainfoOptional = []string{"Complete name", "File size", "Duration", "Width", "Height"}
	defaultBDInfoRequired    = []string{"DISC INFO", "PLAYLIST REPORT"}
	defaultBDInfoOptional    = []string{
		"VIDEO:",
		"AUDIO:",
		"SUBTITLES:",
		"FILES:",
		"Disc Label",
		"Disc Size",
		"BDInfo:",
		"Protection:",
		"Codec",
		"Bitrate",
		"Language",
		"Description",
	}
)

var (
	mediaKeywordConfigOnce sync.Once
	mediaKeywordConfig     mediaFormatKeywordConfig
)

func loadMediaFormatKeywordConfig() mediaFormatKeywordConfig {
	mediaKeywordConfigOnce.Do(func() {
		mediaKeywordConfig = mediaFormatKeywordConfig{
			mediainfoRequired: append([]string{}, defaultMediainfoRequired...),
			mediainfoOptional: append([]string{}, defaultMediainfoOptional...),
			bdinfoRequired:    append([]string{}, defaultBDInfoRequired...),
			bdinfoOptional:    append([]string{}, defaultBDInfoOptional...),
			forbiddenPatterns: []patternConfig{},
		}

		paths := config.ResolveRuntimePaths()
		mappingPath := strings.TrimSpace(paths.GlobalMapYML)
		if mappingPath == "" {
			return
		}

		data, err := os.ReadFile(mappingPath)
		if err != nil {
			logx.Debugf(mediaFormatValidateLogModule, "读取 global_mappings 失败，使用默认关键字 path=%s err=%v", mappingPath, err)
			return
		}

		parsed := map[string]any{}
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			logx.Warnf(mediaFormatValidateLogModule, "解析 global_mappings 失败，使用默认关键字 path=%s err=%v", mappingPath, err)
			return
		}

		contentFiltering := toAnyMapValue(parsed["content_filtering"])
		mediainfoKeywords := toAnyMapValue(contentFiltering["mediainfo_keywords"])
		bdinfoKeywords := toAnyMapValue(contentFiltering["bdinfo_keywords"])

		if required := toStringArrayValue(mediainfoKeywords["required"]); len(required) > 0 {
			mediaKeywordConfig.mediainfoRequired = required
		}
		if optional := toStringArrayValue(mediainfoKeywords["optional"]); len(optional) > 0 {
			mediaKeywordConfig.mediainfoOptional = optional
		}
		if required := toStringArrayValue(bdinfoKeywords["required"]); len(required) > 0 {
			mediaKeywordConfig.bdinfoRequired = required
		}
		if optional := toStringArrayValue(bdinfoKeywords["optional"]); len(optional) > 0 {
			mediaKeywordConfig.bdinfoOptional = optional
		}

		rawForbidden, ok := contentFiltering["forbidden_patterns"].([]any)
		if ok {
			patterns := make([]patternConfig, 0, len(rawForbidden))
			for _, raw := range rawForbidden {
				item := toAnyMapValue(raw)
				pattern := strings.TrimSpace(toStringAny(item["pattern"]))
				if pattern == "" {
					continue
				}
				patterns = append(patterns, patternConfig{
					pattern:     pattern,
					description: strings.TrimSpace(toStringAny(item["description"])),
				})
			}
			if len(patterns) > 0 {
				mediaKeywordConfig.forbiddenPatterns = patterns
			}
		}
	})
	return mediaKeywordConfig
}

func toAnyMapValue(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[any]any:
		result := map[string]any{}
		for key, item := range typed {
			result[strings.TrimSpace(toStringAny(key))] = item
		}
		return result
	default:
		return map[string]any{}
	}
}

func toStringArrayValue(value any) []string {
	switch typed := value.(type) {
	case []string:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			trimmed := strings.TrimSpace(toStringAny(item))
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	default:
		return []string{}
	}
}

func countKeywordMatches(text string, keywords []string) int {
	if len(keywords) == 0 {
		return 0
	}
	upperText := strings.ToUpper(text)
	matches := 0
	for _, keyword := range keywords {
		trimmed := strings.TrimSpace(keyword)
		if trimmed == "" {
			continue
		}
		if strings.Contains(upperText, strings.ToUpper(trimmed)) {
			matches++
		}
	}
	return matches
}

// ValidateMediaInfoFormat 用 Python 对齐规则判断媒体文本是 MediaInfo / BDInfo / Invalid。
func ValidateMediaInfoFormat(text string) (bool, bool, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false, false, "媒体文本为空"
	}

	cfg := loadMediaFormatKeywordConfig()

	mediainfoRequiredMatches := countKeywordMatches(trimmed, cfg.mediainfoRequired)
	mediainfoOptionalMatches := countKeywordMatches(trimmed, cfg.mediainfoOptional)
	bdinfoRequiredMatches := countKeywordMatches(trimmed, cfg.bdinfoRequired)
	bdinfoOptionalMatches := countKeywordMatches(trimmed, cfg.bdinfoOptional)

	isMediainfo := (len(cfg.mediainfoRequired) > 0 && mediainfoRequiredMatches == len(cfg.mediainfoRequired)) ||
		(mediainfoRequiredMatches >= 2 && mediainfoOptionalMatches >= 3)
	isBDInfo := (len(cfg.bdinfoRequired) > 0 && bdinfoRequiredMatches == len(cfg.bdinfoRequired)) ||
		(bdinfoRequiredMatches >= 1 && bdinfoOptionalMatches >= 2)

	if isMediainfo || isBDInfo {
		for _, item := range cfg.forbiddenPatterns {
			compiled, err := regexp.Compile(item.pattern)
			if err != nil {
				logx.Debugf(mediaFormatValidateLogModule, "忽略非法 forbidden pattern pattern=%s err=%v", item.pattern, err)
				continue
			}
			if compiled.MatchString(trimmed) {
				desc := strings.TrimSpace(item.description)
				if desc == "" {
					desc = item.pattern
				}
				matched := sanitizeForbiddenMatch(compiled.FindString(trimmed))
				if matched == "" {
					return false, false, "命中禁止模式:" + desc
				}
				return false, false, "命中禁止模式:" + desc + " 命中内容:" + matched
			}
		}
	}

	if isMediainfo {
		return true, false, "识别为MediaInfo"
	}
	if isBDInfo {
		return false, true, "识别为BDInfo"
	}
	return false, false, "关键字不足"
}

// sanitizeForbiddenMatch 规范化禁止模式命中片段，避免换行污染日志并限制长度。
func sanitizeForbiddenMatch(match string) string {
	trimmed := strings.TrimSpace(match)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, "\r", "\\r")
	trimmed = strings.ReplaceAll(trimmed, "\n", "\\n")
	trimmed = strings.ReplaceAll(trimmed, "\t", "\\t")

	const maxRunes = 120
	runes := []rune(trimmed)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "..."
	}
	return trimmed
}

// NormalizeMediumByMediaType 根据媒体文本类型对 medium 做末端纠偏。
func NormalizeMediumByMediaType(currentMedium string, isMediainfo bool, isBDInfo bool) string {
	medium := strings.TrimSpace(currentMedium)
	upper := strings.ToUpper(medium)

	if isMediainfo {
		if strings.Contains(upper, "BLU") || strings.Contains(upper, "UNK1") {
			return "medium.encode"
		}
		return medium
	}

	if isBDInfo {
		if strings.Contains(upper, "UHD") {
			return "medium.uhd_bluray"
		}
		if medium == "" || medium == "medium.other" || medium == "medium.encode" || strings.Contains(upper, "BLU") || strings.Contains(upper, "UNK1") {
			return "medium.bluray"
		}
	}

	return medium
}

var reBlurayToken = regexp.MustCompile(`(?i)blu-?ray`)

// NormalizeBlurayTokenByMediaType 对齐 Python：MediaInfo -> BluRay，BDInfo -> Blu-ray。
func NormalizeBlurayTokenByMediaType(text string, isMediainfo bool, isBDInfo bool) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return trimmed
	}
	if isMediainfo {
		return reBlurayToken.ReplaceAllString(trimmed, "BluRay")
	}
	if isBDInfo {
		return reBlurayToken.ReplaceAllString(trimmed, "Blu-ray")
	}
	return trimmed
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
