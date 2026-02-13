package localquery

import (
	"fmt"
	"sort"
	"strings"
)

func (s *Service) downloaderMap() map[string]downloaderMeta {
	cfg := s.cfg.Get()
	rawDownloaders := toSlice(cfg["downloaders"])
	result := map[string]downloaderMeta{}

	for _, raw := range rawDownloaders {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := toString(item["id"], "")
		if id == "" {
			continue
		}
		result[id] = downloaderMeta{
			ID:       id,
			Name:     toString(item["name"], "未知"),
			Mappings: parsePathMappings(item["path_mappings"]),
			Remote:   isRemoteDownloader(item),
		}
	}
	return result
}

func (s *Service) downloadersInConfigOrder() []downloaderMeta {
	cfg := s.cfg.Get()
	rawDownloaders := toSlice(cfg["downloaders"])
	out := make([]downloaderMeta, 0, len(rawDownloaders))
	for _, raw := range rawDownloaders {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := toString(item["id"], "")
		if id == "" {
			continue
		}
		out = append(out, downloaderMeta{
			ID:       id,
			Name:     toString(item["name"], "未知"),
			Mappings: parsePathMappings(item["path_mappings"]),
			Remote:   isRemoteDownloader(item),
		})
	}
	return out
}

func parsePathMappings(value any) []pathMapping {
	raw := toSlice(value)
	mappings := make([]pathMapping, 0, len(raw))
	for _, item := range raw {
		mapping, ok := item.(map[string]any)
		if !ok {
			continue
		}
		remote := strings.TrimRight(normalizePath(toString(mapping["remote"], "")), "/")
		local := strings.TrimRight(normalizePath(toString(mapping["local"], "")), "/")
		if remote == "" || local == "" {
			continue
		}
		mappings = append(mappings, pathMapping{Remote: remote, Local: local})
	}
	return mappings
}

func isRemoteDownloader(item map[string]any) bool {
	// Python baseline uses `use_proxy` to decide a downloader is remote via proxy.
	if toBool(item["use_proxy"], false) {
		return true
	}
	if toBool(item["proxy_enabled"], false) {
		return true
	}
	if strings.TrimSpace(toString(item["proxy_base_url"], "")) != "" {
		return true
	}
	if toBool(item["is_remote"], false) {
		return true
	}
	location := strings.ToLower(strings.TrimSpace(toString(item["location"], "")))
	return location == "remote"
}

func normalizePath(path string) string {
	normalized := strings.ReplaceAll(path, "\\", "/")
	for strings.Contains(normalized, "//") {
		normalized = strings.ReplaceAll(normalized, "//", "/")
	}
	return normalized
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func orderByNameAndPath(items []map[string]any) {
	sort.Slice(items, func(i, j int) bool {
		leftName := stringFromAny(items[i]["name"])
		rightName := stringFromAny(items[j]["name"])
		if leftName == rightName {
			leftPath := stringFromAny(items[i]["path"])
			if leftPath == "" {
				leftPath = stringFromAny(items[i]["save_path"])
			}
			rightPath := stringFromAny(items[j]["path"])
			if rightPath == "" {
				rightPath = stringFromAny(items[j]["save_path"])
			}
			return leftPath < rightPath
		}
		return leftName < rightName
	})
}

func sortStrings(items []string) {
	sort.Strings(items)
}

func stringsHasPrefix(value, prefix string) bool {
	return strings.HasPrefix(value, prefix)
}

func stringsTrimPrefix(value, prefix string) string {
	return strings.TrimPrefix(value, prefix)
}

func toSlice(value any) []any {
	if value == nil {
		return []any{}
	}
	if typed, ok := value.([]any); ok {
		return typed
	}
	return []any{}
}

func toString(value any, fallback string) string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback
		}
		return typed
	case nil:
		return fallback
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text == "" {
			return fallback
		}
		return text
	}
}

func toBool(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		lower := strings.ToLower(strings.TrimSpace(typed))
		if lower == "true" || lower == "1" || lower == "yes" {
			return true
		}
		if lower == "false" || lower == "0" || lower == "no" {
			return false
		}
		return fallback
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return fallback
	}
}
