package localquery

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *Service) Scan(targetPath string) (map[string]any, error) {
	torrents, err := s.repo.ListTorrents(targetPath)
	if err != nil {
		return nil, err
	}
	downloaders := s.downloaderMap()

	missingFiles := make([]map[string]any, 0)
	orphanedFiles := make([]map[string]any, 0)
	syncedIndex := map[string]*syncedItem{}
	missingCandidates := map[string][]map[string]any{}
	groupHasLocalExists := map[string]bool{}
	localPaths := map[string]*localPathMeta{}
	remoteTorrentsCount := 0
	localItemsCount := 0

	for _, torrent := range torrents {
		meta, ok := downloaders[torrent.DownloaderID]
		if !ok {
			meta = downloaderMeta{ID: torrent.DownloaderID, Name: "未知", Mappings: []pathMapping{}, Remote: false}
		}

		remotePath := normalizePath(torrent.SavePath)
		localPath := remotePath
		if !meta.Remote {
			localPath = applyRemoteToLocal(remotePath, meta.Mappings)
		}

		if _, exists := localPaths[localPath]; !exists {
			localPaths[localPath] = &localPathMeta{
				RemotePath: remotePath,
				Mappings:   meta.Mappings,
				Expected:   map[string]struct{}{},
			}
		}
		localPaths[localPath].Expected[torrent.Name] = struct{}{}

		syncKey := buildNameSizeGroupKey(torrent.Name, torrent.Size)
		syncItem, exists := syncedIndex[syncKey]
		if !exists {
			syncItem = &syncedItem{
				Name:            torrent.Name,
				Path:            remotePath,
				Size:            torrent.Size,
				Count:           0,
				DownloaderNames: map[string]struct{}{},
			}
			syncedIndex[syncKey] = syncItem
		}
		syncItem.Count++
		syncItem.DownloaderNames[meta.Name] = struct{}{}
		if syncItem.Path == "" || (remotePath != "" && remotePath < syncItem.Path) {
			syncItem.Path = remotePath
		}

		if meta.Remote {
			remoteTorrentsCount++
			continue
		}

		expectedPath := filepath.Join(localPath, torrent.Name)
		if _, err := os.Stat(expectedPath); err == nil {
			groupHasLocalExists[syncKey] = true
			continue
		}
		missingCandidates[syncKey] = append(missingCandidates[syncKey], map[string]any{
			"name":            torrent.Name,
			"save_path":       remotePath,
			"expected_path":   expectedPath,
			"size":            torrent.Size,
			"downloader_name": meta.Name,
		})
	}

	for groupKey, candidates := range missingCandidates {
		if len(candidates) == 0 || groupHasLocalExists[groupKey] {
			continue
		}
		representative := candidates[0]
		downloaderSet := map[string]struct{}{}
		for _, candidate := range candidates {
			name := strings.TrimSpace(stringFromAny(candidate["downloader_name"]))
			if name != "" {
				downloaderSet[name] = struct{}{}
			}
			currentPath := stringFromAny(candidate["save_path"])
			bestPath := stringFromAny(representative["save_path"])
			if bestPath == "" || (currentPath != "" && currentPath < bestPath) {
				representative = candidate
			}
		}

		downloaderNames := make([]string, 0, len(downloaderSet))
		for name := range downloaderSet {
			downloaderNames = append(downloaderNames, name)
		}
		sortStrings(downloaderNames)

		missingFiles = append(missingFiles, map[string]any{
			"name":            stringFromAny(representative["name"]),
			"save_path":       stringFromAny(representative["save_path"]),
			"expected_path":   stringFromAny(representative["expected_path"]),
			"size":            representative["size"],
			"downloader_name": strings.Join(downloaderNames, ", "),
			"torrents_count":  len(candidates),
		})
	}

	for localPath, meta := range localPaths {
		entries, err := os.ReadDir(localPath)
		if err != nil {
			continue
		}
		localItemsCount += len(entries)

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if _, exists := meta.Expected[entry.Name()]; exists {
				continue
			}
			fullPath := filepath.Join(localPath, entry.Name())
			size := int64(0)
			if stat, err := os.Stat(fullPath); err == nil {
				size = stat.Size()
			}
			orphanedFiles = append(orphanedFiles, map[string]any{
				"name":      entry.Name(),
				"path":      meta.RemotePath,
				"full_path": fullPath,
				"size":      size,
			})
		}
	}

	syncedTorrents := make([]map[string]any, 0, len(syncedIndex))
	for _, item := range syncedIndex {
		names := make([]string, 0, len(item.DownloaderNames))
		for name := range item.DownloaderNames {
			names = append(names, name)
		}
		sortStrings(names)
		syncedTorrents = append(syncedTorrents, map[string]any{
			"name":             item.Name,
			"path":             item.Path,
			"size":             item.Size,
			"torrents_count":   item.Count,
			"downloader_names": names,
		})
	}
	orderByNameAndPath(syncedTorrents)
	orderByNameAndPath(missingFiles)
	orderByNameAndPath(orphanedFiles)

	result := map[string]any{
		"scan_summary": map[string]any{
			"total_torrents":        len(torrents),
			"total_local_items":     localItemsCount,
			"missing_count":         len(missingFiles),
			"orphaned_count":        len(orphanedFiles),
			"synced_count":          len(syncedTorrents),
			"remote_torrents_count": remoteTorrentsCount,
			"skipped_remote":        remoteTorrentsCount > 0,
		},
		"missing_files":   missingFiles,
		"orphaned_files":  orphanedFiles,
		"synced_torrents": syncedTorrents,
	}
	_ = s.saveScanCache(result)
	return result, nil
}

func applyRemoteToLocal(remotePath string, mappings []pathMapping) string {
	normalized := normalizePath(remotePath)
	for _, mapping := range mappings {
		if normalized == mapping.Remote || stringsHasPrefix(normalized, mapping.Remote+"/") {
			return mapping.Local + stringsTrimPrefix(normalized, mapping.Remote)
		}
	}
	return normalized
}

func buildNameSizeGroupKey(name string, size int64) string {
	return strings.TrimSpace(name) + "\x00" + strconv.FormatInt(size, 10)
}
