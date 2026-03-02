package torrentdata

import (
	"fmt"
	"sort"
	"strings"

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

	aggregated := map[string]*torrentSummary{}
	for _, row := range torrentRows {
		key := fmt.Sprintf("%s\x00%d", row.Name, row.Size)
		item, exists := aggregated[key]
		if !exists {
			item = &torrentSummary{
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

		item := map[string]any{
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
