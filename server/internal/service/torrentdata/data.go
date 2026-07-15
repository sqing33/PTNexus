package torrentdata

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pt-nexus/server/internal/repository"
)

// GetData 聚合维护种子列表数据并按查询条件分页返回。
// 参数/返回：params 为页面、筛选、排序和 metadata 控制参数；返回前端表格数据及可选筛选元数据。
// 失败场景：站点配置、种子列表或筛选元数据查询失败时返回错误。
// 副作用：仅读取数据库和运行配置，不写入数据。
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

	if params.MetadataOnly {
		return s.getDataMetadataResponse(params)
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

	listFilters := repository.TorrentListFilters{
		OnlyCompleted:     params.OnlyCompleted,
		NameSearch:        params.NameSearch,
		PathFilters:       params.PathFilters,
		StateFilters:      params.StateFilters,
		DownloaderFilters: params.DownloaderFilters,
		ExcludeExisting:   params.ExcludeExisting,
	}
	databasePaginated := canPrePaginateTorrentGroups(params)
	databasePaginatedTotal := 0
	if databasePaginated {
		groupKeys, groupErr := s.repo.ListTorrentGroupKeysWithFilters(listFilters)
		if groupErr != nil {
			return nil, fmt.Errorf("load torrent group keys failed: %w", groupErr)
		}
		sortTorrentGroupKeys(groupKeys, params.SortProp, params.SortOrder)
		databasePaginatedTotal = len(groupKeys)
		groupKeys = paginateTorrentGroupKeys(groupKeys, params.Page, params.PageSize)
		if len(groupKeys) == 0 {
			listFilters = repository.TorrentListFilters{Groups: []repository.TorrentNameSizeKey{}}
		} else {
			// 分组键已经完成筛选，只按当前页键读取明细，避免再次扫描完成状态和已维护分组。
			listFilters = repository.TorrentListFilters{Groups: groupKeys}
		}
	}

	torrentRows := make([]repository.TorrentRecord, 0)
	if !databasePaginated || len(listFilters.Groups) > 0 {
		torrentRows, err = s.repo.ListTorrentsWithFilters(listFilters)
	}
	if err != nil {
		return nil, fmt.Errorf("load torrents failed: %w", err)
	}
	uploadTotals, err := s.repo.UploadTotalsByHashes(torrentRecordHashes(torrentRows))
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

	postFilterParams := params
	postFilterParams.ExcludeExisting = false
	filtered := s.applyFilters(allItems, postFilterParams, siteConfigMap)
	s.sortData(filtered, params.SortProp, params.SortOrder)

	total := len(filtered)
	paginated := filtered
	if databasePaginated {
		total = databasePaginatedTotal
	} else {
		start := (params.Page - 1) * params.PageSize
		if start > total {
			start = total
		}
		end := start + params.PageSize
		if end > total {
			end = total
		}
		paginated = filtered[start:end]
	}

	result := map[string]any{
		"data":     paginated,
		"total":    total,
		"page":     params.Page,
		"pageSize": params.PageSize,
	}
	if !params.SkipMetadata {
		metadata, err := s.dataMetadata(params, allDiscoveredSites)
		if err != nil {
			return nil, err
		}
		for key, value := range metadata {
			result[key] = value
		}
	}
	return result, nil
}

// canPrePaginateTorrentGroups 判断当前筛选和排序能否在聚合明细前先分页。
// 参数/返回：params 为列表参数；返回 true 时只需读取当前页 name+size 分组的明细。
// 失败场景：无；依赖站点明细或聚合数值的筛选/排序会返回 false 并走兼容路径。
// 副作用：无。
func canPrePaginateTorrentGroups(params TorrentsDataParams) bool {
	if len(params.SourceAvailabilityFilters) > 0 || len(params.ExistSiteNames) > 0 || len(params.NotExistSiteNames) > 0 {
		return false
	}
	sortProp := strings.TrimSpace(params.SortProp)
	switch sortProp {
	case "", "name", "size", "size_formatted":
		return true
	default:
		return false
	}
}

func sortTorrentGroupKeys(groups []repository.TorrentNameSizeKey, sortProp string, sortOrder string) {
	descending := strings.EqualFold(strings.TrimSpace(sortOrder), "descending")
	sortProp = strings.TrimSpace(sortProp)
	if sortProp == "size_formatted" {
		sortProp = "size"
	}

	sort.SliceStable(groups, func(i, j int) bool {
		left := groups[i]
		right := groups[j]
		if sortProp == "size" {
			if left.Size != right.Size {
				if descending {
					return left.Size > right.Size
				}
				return left.Size < right.Size
			}
		} else if left.Name != right.Name {
			if descending {
				return customNameLess(right.Name, left.Name)
			}
			return customNameLess(left.Name, right.Name)
		}

		if left.Name != right.Name {
			if descending {
				return customNameLess(right.Name, left.Name)
			}
			return customNameLess(left.Name, right.Name)
		}
		if descending {
			return left.Size > right.Size
		}
		return left.Size < right.Size
	})
}

func paginateTorrentGroupKeys(groups []repository.TorrentNameSizeKey, page int, pageSize int) []repository.TorrentNameSizeKey {
	start := (page - 1) * pageSize
	if start >= len(groups) {
		return []repository.TorrentNameSizeKey{}
	}
	end := start + pageSize
	if end > len(groups) {
		end = len(groups)
	}
	return groups[start:end]
}

// getDataMetadataResponse 返回维护种子列表筛选器需要的轻量元数据。
// 参数/返回：params 只使用完成状态和当前路径筛选；返回空 data 与路径、状态、站点、链接规则等选项。
// 失败场景：站点或筛选选项查询失败时返回错误。
// 副作用：仅读取数据库，不写入数据。
func (s *TorrentDataService) getDataMetadataResponse(params TorrentsDataParams) (map[string]any, error) {
	allDiscoveredSites, err := s.repo.ListAllDiscoveredSites()
	if err != nil {
		return nil, fmt.Errorf("load discovered sites failed: %w", err)
	}
	metadata, err := s.dataMetadata(params, allDiscoveredSites)
	if err != nil {
		return nil, err
	}
	metadata["data"] = []map[string]any{}
	metadata["total"] = 0
	metadata["page"] = params.Page
	metadata["pageSize"] = params.PageSize
	return metadata, nil
}

// dataMetadata 读取列表筛选和链接展示所需的元数据。
// 参数/返回：allDiscoveredSites 可复用主列表已查询的站点集合；返回 unique_paths、unique_states、all_discovered_sites 等字段。
// 失败场景：路径/状态候选查询失败时返回错误，站点链接规则失败时降级为空 map。
// 副作用：仅读取数据库，不写入数据。
func (s *TorrentDataService) dataMetadata(params TorrentsDataParams, allDiscoveredSites []string) (map[string]any, error) {
	uniquePaths, uniqueStates, err := s.repo.ListTorrentFilterOptions(params.OnlyCompleted)
	if err != nil {
		return nil, fmt.Errorf("load torrent filter options failed: %w", err)
	}
	siteLinkRules, err := s.repo.SiteLinkRules()
	if err != nil {
		siteLinkRules = map[string]any{}
	}
	return map[string]any{
		"unique_paths":         uniquePaths,
		"unique_states":        uniqueStates,
		"all_discovered_sites": allDiscoveredSites,
		"site_link_rules":      siteLinkRules,
		"active_path_filters":  params.PathFilters,
	}, nil
}

func torrentRecordHashes(rows []repository.TorrentRecord) []string {
	hashes := make([]string, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		hash := strings.TrimSpace(row.Hash)
		if hash == "" {
			continue
		}
		if _, exists := seen[hash]; exists {
			continue
		}
		seen[hash] = struct{}{}
		hashes = append(hashes, hash)
	}
	return hashes
}
