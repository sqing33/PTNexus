package downloaderclient

import (
	"path/filepath"
	"sort"
	"strings"
)

// TranslateDownloaderPath 按下载器 path_mappings 把远端保存路径转换为本地路径。
// 参数/返回：rootConfig 为全量配置；downloaderID 为下载器 ID；remotePath 为下载器返回的保存路径；返回映射后的本地路径（未命中映射时返回原路径）。
// 失败场景：配置缺失、下载器不存在、映射项为空时均返回原路径，不报错。
// 副作用：无。
func TranslateDownloaderPath(rootConfig map[string]any, downloaderID, remotePath string) string {
	trimmedID := strings.TrimSpace(downloaderID)
	trimmedPath := strings.TrimSpace(remotePath)
	if trimmedID == "" || trimmedPath == "" {
		return trimmedPath
	}

	rawDownloaders := toSlice(rootConfig["downloaders"])
	for _, raw := range rawDownloaders {
		item := toMap(raw)
		if strings.TrimSpace(toString(item["id"], "")) != trimmedID {
			continue
		}

		type pathMapping struct {
			remote string
			local  string
		}
		mappings := make([]pathMapping, 0)
		for _, rawEntry := range toSlice(item["path_mappings"]) {
			entry := toMap(rawEntry)
			remote := strings.TrimRight(normalizePathForMatch(toString(entry["remote"], "")), "/")
			local := strings.TrimSpace(toString(entry["local"], ""))
			if remote == "" || local == "" {
				continue
			}
			mappings = append(mappings, pathMapping{
				remote: remote,
				local:  local,
			})
		}
		sort.SliceStable(mappings, func(i, j int) bool {
			return len(mappings[i].remote) > len(mappings[j].remote)
		})

		normalizedPath := strings.TrimRight(normalizePathForMatch(trimmedPath), "/")
		for _, mapping := range mappings {
			if normalizedPath == mapping.remote {
				return mapping.local
			}
			prefix := mapping.remote + "/"
			if strings.HasPrefix(normalizedPath, prefix) {
				rel := strings.TrimPrefix(normalizedPath, prefix)
				if rel == "" {
					return mapping.local
				}
				parts := strings.Split(rel, "/")
				return filepath.Join(append([]string{mapping.local}, parts...)...)
			}
		}
		return trimmedPath
	}

	return trimmedPath
}

func normalizePathForMatch(path string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(path), "\\\\", "/")
	for strings.Contains(normalized, "//") {
		normalized = strings.ReplaceAll(normalized, "//", "/")
	}
	return normalized
}
