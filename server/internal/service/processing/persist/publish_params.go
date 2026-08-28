package persist

import (
	"encoding/json"
	"fmt"
	"strings"

	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
)

// ComposeSeedID 组装种子唯一标识。
func ComposeSeedID(hash, torrentID, siteName string) string {
	return fmt.Sprintf("%s_%s_%s", hash, torrentID, siteName)
}

// ParseSeedID 解析种子唯一标识。
func ParseSeedID(seedID string) (string, string, string, error) {
	parts := strings.Split(seedID, "_")
	if len(parts) < 3 {
		return "", "", "", fmt.Errorf("无效的种子ID格式: %s", seedID)
	}
	siteName := parts[len(parts)-1]
	torrentID := parts[len(parts)-2]
	hash := strings.Join(parts[:len(parts)-2], "_")
	if hash == "" || torrentID == "" || siteName == "" {
		return "", "", "", fmt.Errorf("无效的种子ID格式: %s", seedID)
	}
	return hash, torrentID, siteName, nil
}

// BuildStandardizedParams 组装标准化参数。
func BuildStandardizedParams(row map[string]any) map[string]any {
	return map[string]any{
		"type":          toStringAny(row["type"], ""),
		"medium":        toStringAny(row["medium"], ""),
		"video_codec":   toStringAny(row["video_codec"], ""),
		"audio_codec":   toStringAny(row["audio_codec"], ""),
		"resolution":    toStringAny(row["resolution"], ""),
		"team":          parser.NormalizeTeamKey(toStringAny(row["team"], "")),
		"official_site": toStringAny(row["official_site"], ""),
		"source":        toStringAny(row["source"], ""),
		"tags":          parseStringArray(row["tags"]),
		"imdb_link":     toStringAny(row["imdb_link"], ""),
		"douban_link":   toStringAny(row["douban_link"], ""),
		"tmdb_link":     toStringAny(row["tmdb_link"], ""),
	}
}

// BuildFinalPublishParameters 组装发布映射前展示参数。
func BuildFinalPublishParameters(row map[string]any) map[string]any {
	standardized := BuildStandardizedParams(row)
	return map[string]any{
		"主标题 (预览)": toStringAny(row["title"], ""),
		"副标题":      toStringAny(row["subtitle"], ""),
		"IMDb链接":   standardized["imdb_link"],
		"豆瓣链接":     standardized["douban_link"],
		"TMDb链接":   standardized["tmdb_link"],
		"类型":       standardized["type"],
		"媒介":       standardized["medium"],
		"视频编码":     standardized["video_codec"],
		"音频编码":     standardized["audio_codec"],
		"分辨率":      standardized["resolution"],
		"制作组":      standardized["team"],
		"产地":       standardized["source"],
		"标签":       standardized["tags"],
	}
}

// BuildCompletePublishParams 组装完整发布参数。
func BuildCompletePublishParams(row map[string]any) map[string]any {
	return map[string]any{
		"title_components": row["title_components"],
		"subtitle":         row["subtitle"],
		"imdb_link":        row["imdb_link"],
		"douban_link":      row["douban_link"],
		"tmdb_link":        row["tmdb_link"],
		"intro": map[string]any{
			"statement":                 row["statement"],
			"poster":                    row["poster"],
			"body":                      row["body"],
			"screenshots":               row["screenshots"],
			"removed_ardtudeclarations": row["removed_ardtudeclarations"],
		},
		"mediainfo":           row["mediainfo"],
		"standardized_params": BuildStandardizedParams(row),
	}
}

// BuildRawPreviewParams 组装原始预览参数。
func BuildRawPreviewParams(row map[string]any) map[string]any {
	standardized := BuildStandardizedParams(row)
	return map[string]any{
		"final_main_title": toStringAny(row["title"], ""),
		"subtitle":         toStringAny(row["subtitle"], ""),
		"imdb_link":        standardized["imdb_link"],
		"douban_link":      standardized["douban_link"],
		"tmdb_link":        standardized["tmdb_link"],
		"type":             standardized["type"],
		"medium":           standardized["medium"],
		"video_codec":      standardized["video_codec"],
		"audio_codec":      standardized["audio_codec"],
		"resolution":       standardized["resolution"],
		"release_group":    standardized["team"],
		"source":           standardized["source"],
		"tags":             standardized["tags"],
	}
}

func toStringAny(value any, fallback string) string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback
		}
		return typed
	case []byte:
		text := strings.TrimSpace(string(typed))
		if text == "" {
			return fallback
		}
		return text
	default:
		return fallback
	}
}

func parseStringArray(value any) []string {
	if value == nil {
		return []string{}
	}
	switch typed := value.(type) {
	case []string:
		return normalizeStringArray(typed)
	case []any:
		return normalizeAnyStringArray(typed)
	case []byte:
		return parseStringArray(string(typed))
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return []string{}
		}
		// 优先按 JSON 数组解析，避免把 JSON 字符串再次序列化导致长度膨胀。
		if parsed, ok := parseJSONStringArray(trimmed); ok {
			return parsed
		}
		parts := strings.Split(trimmed, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			entry := strings.TrimSpace(part)
			if entry != "" {
				result = append(result, entry)
			}
		}
		return result
	default:
		return []string{}
	}
}

func parseJSONStringArray(raw string) ([]string, bool) {
	parsedStrings := make([]string, 0)
	if err := json.Unmarshal([]byte(raw), &parsedStrings); err == nil {
		return normalizeStringArray(parsedStrings), true
	}

	parsedAny := make([]any, 0)
	if err := json.Unmarshal([]byte(raw), &parsedAny); err == nil {
		return normalizeAnyStringArray(parsedAny), true
	}

	return nil, false
}

func normalizeAnyStringArray(values []any) []string {
	result := make([]string, 0, len(values))
	for _, item := range values {
		text := strings.TrimSpace(toStringAny(item, ""))
		if text != "" {
			result = append(result, text)
		}
	}
	return result
}

func normalizeStringArray(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text != "" {
			result = append(result, text)
		}
	}
	return result
}
