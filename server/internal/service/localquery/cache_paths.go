package localquery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Service) GetScanCache() (map[string]any, bool, error) {
	content, err := os.ReadFile(s.cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	parsed := map[string]any{}
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil, false, err
	}
	return parsed, true, nil
}

func (s *Service) GetPaths(videoOnly bool) (map[string]any, error) {
	paths, err := s.repo.DistinctPaths(videoOnly)
	if err != nil {
		return nil, err
	}
	return map[string]any{"paths": paths, "total": len(paths)}, nil
}

func (s *Service) GetDownloadersWithPaths(videoOnly bool) (map[string]any, error) {
	downloaders := s.downloadersInConfigOrder()
	result := make([]map[string]any, 0)

	for _, meta := range downloaders {
		pathRows, err := s.repo.DownloaderPathCounts(meta.ID, videoOnly)
		if err != nil {
			return nil, err
		}

		merged := map[string]int64{}
		for _, row := range pathRows {
			savePath := strings.TrimSpace(row.SavePath)
			if savePath == "" {
				continue
			}
			merged[savePath] += row.Count
			for _, mapping := range meta.Mappings {
				if mapping.Local == "" || mapping.Remote == "" {
					continue
				}
				if savePath == mapping.Local || strings.HasPrefix(savePath, mapping.Local+"/") {
					mappedPath := mapping.Remote + strings.TrimPrefix(savePath, mapping.Local)
					merged[mappedPath] += row.Count
				}
			}
		}

		paths := make([]map[string]any, 0)
		for path, count := range merged {
			if !meta.Remote {
				localPath := applyRemoteToLocal(path, meta.Mappings)
				if _, err := os.Stat(localPath); err != nil {
					continue
				}
			}
			paths = append(paths, map[string]any{"path": path, "count": count})
		}
		sort.Slice(paths, func(i, j int) bool {
			return stringFromAny(paths[i]["path"]) < stringFromAny(paths[j]["path"])
		})

		result = append(result, map[string]any{
			"id":    meta.ID,
			"name":  meta.Name,
			"paths": paths,
		})
	}
	return map[string]any{"downloaders": result}, nil
}

func (s *Service) saveScanCache(data map[string]any) error {
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.cacheFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.cacheFile, content, 0o644)
}
