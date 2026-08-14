package torrentdata

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
)

func (s *TorrentDataService) GetData(params TorrentsDataParams) (map[string]any, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 50
	}
	if params.PageSize > 200 {
		params.PageSize = 200
	}

	siteConfigs, err := s.repo.ListSiteConfigs()
	if err != nil {
		return nil, fmt.Errorf("load site configs failed: %w", err)
	}
	siteConfigMap := map[string]repository.SiteConfig{}
	targetSites := map[string]struct{}{}
	for _, item := range siteConfigs {
		siteConfigMap[item.Nickname] = item
		if item.Migration == 2 || item.Migration == 3 {
			targetSites[item.Nickname] = struct{}{}
		}
	}

	allDiscoveredSites, err := s.repo.ListAllDiscoveredSites()
	if err != nil {
		return nil, fmt.Errorf("load discovered sites failed: %w", err)
	}

	torrentRows, err := s.repo.ListTorrents(params.OnlyCompleted)
	if err != nil {
		return nil, fmt.Errorf("load torrents failed: %w", err)
	}
	uploadTotals, err := s.repo.UploadTotalsByHash()
	if err != nil {
		uploadTotals = map[string]int64{}
	}
	publishAtMap, err := s.repo.PublishAtByNames()
	if err != nil {
		publishAtMap = map[string]string{}
	}
	lastPublishAtMap, err := s.repo.LastPublishAtByNames()
	if err != nil {
		// 不能静默降级为空：此处记录日志便于定位（如 MySQL 日期列与空串比较导致的 SQL 错误）。
		logx.Warnf(torrentDataLogModule, "加载最后发布时间映射失败 err=%v", err)
		lastPublishAtMap = map[string]string{}
	}
	sourceStatusMap, err := s.repo.SeedParameterSourceStatusByNames()
	if err != nil {
		sourceStatusMap = map[string]repository.SeedParameterSourceStatus{}
	}

	aggregated := map[string]*torrentSummary{}
	for _, row := range torrentRows {
		key := fmt.Sprintf("%s\x00%d", row.Name, row.Size)
		item, exists := aggregated[key]
		if !exists {
			item = &torrentSummary{
				Hash:          row.Hash,
				Hashes:        []string{},
				Name:          row.Name,
				SavePath:      row.SavePath,
				Size:          row.Size,
				Progress:      row.Progress,
				StateSet:      map[string]struct{}{},
				Sites:         map[string]*siteSummary{},
				TotalUploaded: 0,
				Seeders:       row.Seeders,
				DownloaderIDs: []string{},
			}
			aggregated[key] = item
		}
		if row.Hash != "" && !containsString(item.Hashes, row.Hash) {
			item.Hashes = append(item.Hashes, row.Hash)
			if item.Hash == "" {
				item.Hash = row.Hash
			}
		}
		if item.SavePath == "" && row.SavePath != "" {
			item.SavePath = row.SavePath
		}
		if row.Progress > item.Progress {
			item.Progress = row.Progress
		}
		if row.State != "" {
			item.StateSet[row.State] = struct{}{}
		}
		if row.Seeders > item.Seeders {
			item.Seeders = row.Seeders
		}
		if row.Downloader != "" && !containsString(item.DownloaderIDs, row.Downloader) {
			item.DownloaderIDs = append(item.DownloaderIDs, row.Downloader)
		}

		uploaded := uploadTotals[row.Hash]
		item.TotalUploaded += uploaded

		siteName := strings.TrimSpace(row.Sites)
		if siteName == "" {
			continue
		}
		siteItem, exists := item.Sites[siteName]
		if !exists {
			migration := 0
			if config, ok := siteConfigMap[siteName]; ok {
				migration = config.Migration
			}
			siteItem = &siteSummary{Migration: migration}
			item.Sites[siteName] = siteItem
		}
		siteItem.Uploaded += uploaded
		siteItem.Comment = row.Details
		siteItem.State = row.State
		if row.Seeders > siteItem.Seeders {
			siteItem.Seeders = row.Seeders
		}
	}

	allItems := make([]map[string]any, 0, len(aggregated))
	for _, value := range aggregated {
		states := make([]string, 0, len(value.StateSet))
		for state := range value.StateSet {
			states = append(states, state)
		}
		sort.Strings(states)

		// Python baseline keeps the first-seen downloader order; don't sort.
		downloaderIDs := append([]string{}, value.DownloaderIDs...)

		sites := map[string]any{}
		existingSites := map[string]struct{}{}
		for siteName, details := range value.Sites {
			existingSites[siteName] = struct{}{}
			sites[siteName] = map[string]any{
				"uploaded":  details.Uploaded,
				"comment":   details.Comment,
				"migration": details.Migration,
				"state":     details.State,
				"seeders":   details.Seeders,
			}
		}
		targetSiteCount := 0
		for target := range targetSites {
			if _, exists := existingSites[target]; !exists {
				targetSiteCount++
			}
		}

		sourceStatus := sourceStatusMap[value.Name]
		sourceDataStatus := "missing"
		if sourceStatus.IsReviewed {
			sourceDataStatus = "reviewed"
		} else if sourceStatus.HasFetchedSourceData {
			sourceDataStatus = "unreviewed"
		}
		if !sourceStatus.HasRecord {
			sourceDataStatus = "missing"
		}

		item := map[string]any{
			"hash":                     value.Hash,
			"hashes":                   append([]string{}, value.Hashes...),
			"name":                     value.Name,
			"save_path":                value.SavePath,
			"size":                     value.Size,
			"progress":                 value.Progress,
			"state":                    strings.Join(states, ", "),
			"sites":                    sites,
			"total_uploaded":           value.TotalUploaded,
			"size_formatted":           formatBytes(value.Size),
			"total_uploaded_formatted": formatBytes(value.TotalUploaded),
			"site_count":               len(value.Sites),
			"total_site_count":         len(allDiscoveredSites),
			"target_sites_count":       targetSiteCount,
			"seeders":                  value.Seeders,
			"downloaderIds":            downloaderIDs,
			"downloader_ids":           downloaderIDs,
			"downloaderId":             s.selectBestDownloader(downloaderIDs),
			"unique_id":                fmt.Sprintf("%s_%d", value.Name, value.Size),
			"publish_at":               publishAtMap[value.Name],
			"last_publish_at":          lastPublishAtMap[value.Name],
			"source_data_status":       sourceDataStatus,
			"source_data_fetched":      sourceStatus.HasFetchedSourceData,
			"source_data_reviewed":     sourceStatus.IsReviewed,
		}
		allItems = append(allItems, item)
	}

	filtered := s.applyFilters(allItems, params, siteConfigMap)
	s.sortData(filtered, params.SortProp, params.SortOrder)

	total := len(filtered)
	start := (params.Page - 1) * params.PageSize
	if start > total {
		start = total
	}
	end := start + params.PageSize
	if end > total {
		end = total
	}
	paginated := filtered[start:end]

	uniquePaths := collectUniqueStrings(torrentRows, func(row repository.TorrentRecord) string {
		return row.SavePath
	})
	uniqueStates := collectUniqueStrings(torrentRows, func(row repository.TorrentRecord) string {
		return row.State
	})

	siteLinkRules, err := s.repo.SiteLinkRules()
	if err != nil {
		siteLinkRules = map[string]any{}
	}

	return map[string]any{
		"data":                 paginated,
		"total":                total,
		"page":                 params.Page,
		"pageSize":             params.PageSize,
		"unique_paths":         uniquePaths,
		"unique_states":        uniqueStates,
		"all_discovered_sites": allDiscoveredSites,
		"site_link_rules":      siteLinkRules,
		"active_path_filters":  params.PathFilters,
	}, nil
}

func (s *TorrentDataService) UpdatePublishAt(payload map[string]any) (map[string]any, int) {
	name, _ := payload["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return map[string]any{"success": false, "error": "缺少 name 参数"}, 400
	}

	publishAtRaw, exists := payload["publish_at"]
	if !exists {
		return map[string]any{"success": false, "error": "缺少 publish_at 参数"}, 400
	}

	var publishAt any
	if publishAtRaw == nil || publishAtRaw == "" {
		publishAt = nil
	} else {
		publishAtStr, ok := publishAtRaw.(string)
		if !ok {
			return map[string]any{"success": false, "error": "publish_at 格式错误"}, 400
		}
		publishAt = strings.TrimSpace(publishAtStr)
	}

	affected, err := s.repo.UpdatePublishAtByName(name, publishAt)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, 500
	}
	if affected == 0 {
		return map[string]any{"success": false, "error": "未找到匹配的种子数据"}, 404
	}
	return map[string]any{"success": true, "message": "可发种时间已更新"}, 200
}
