package torrentdata

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
)

type iyuuQueryGroup struct {
	Name string
	Size int64
}

type iyuuGroupState struct {
	Group          iyuuQueryGroup
	Torrents       []repository.IYUUTorrentRow
	Filtered       []repository.IYUUTorrentRow
	PriorityHashes []string
	TotalAttempts  int

	SelectedHash    string
	SelectedTorrent *repository.IYUUTorrentRow
	Results         []map[string]any
	Found           bool
	Resolved        bool
}

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *TorrentDataService) RefreshData() map[string]any {
	return s.refreshFromDownloaders()
}

func (s *TorrentDataService) QueryIYUU(payload map[string]any) (map[string]any, int) {
	if !s.iyuuRunning.CompareAndSwap(false, true) {
		return map[string]any{"success": false, "message": "IYUU查询正在进行中，请稍后再试"}, 500
	}
	defer s.iyuuRunning.Store(false)

	forceQuery := toBool(payload["force_query"], true)
	savePath := strings.TrimSpace(stringValue(payload["save_path"], stringValue(payload["path"], "")))
	if savePath != "" {
		limit := intValue(payload["limit"], 200)
		if limit <= 0 {
			limit = 200
		}
		anchorName := strings.TrimSpace(stringValue(payload["anchor_name"], stringValue(payload["name"], "")))
		groups, totalGroups := s.groupsByPath(savePath, limit, anchorName)
		stats, err := s.performIYUUQuery(context.Background(), groups, forceQuery, "", nil)
		if err != nil {
			return map[string]any{"success": false, "error": "触发IYUU查询失败", "message": err.Error()}, 500
		}
		return map[string]any{
			"success": true,
			"message": fmt.Sprintf("路径 '%s' 的 IYUU 查询已完成，处理 %d/%d 个种子组", savePath, len(groups), totalGroups),
			"stats":   stats,
			"query_info": map[string]any{
				"mode":             "path",
				"save_path":        savePath,
				"total_groups":     totalGroups,
				"processed_groups": len(groups),
				"limit":            limit,
				"anchor_name":      anchorName,
				"force_query":      forceQuery,
			},
		}, 200
	}

	torrentName := strings.TrimSpace(stringValue(payload["name"], ""))
	if torrentName == "" {
		return map[string]any{"success": false, "error": "缺少种子名称参数"}, 400
	}
	torrentSize := int64Value(payload["size"], 0)
	if torrentSize <= 0 {
		return map[string]any{"success": false, "error": "缺少种子大小参数"}, 400
	}

	pathFilters := s.iyuuPathFilters()
	stats, err := s.performIYUUQuery(context.Background(), []iyuuQueryGroup{{Name: torrentName, Size: torrentSize}}, forceQuery, "", pathFilters)
	if err != nil {
		return map[string]any{"success": false, "error": "触发IYUU查询失败", "message": err.Error()}, 500
	}

	return map[string]any{
		"success": true,
		"message": fmt.Sprintf("种子 '%s' 的 IYUU 查询已完成", torrentName),
		"stats":   stats,
		"query_info": map[string]any{
			"mode":        "single",
			"name":        torrentName,
			"size":        torrentSize,
			"force_query": forceQuery,
		},
	}, 200
}

func (s *TorrentDataService) StartIYUUQueryBatch(torrents []map[string]any, maxGroups int, forceQuery bool) (string, error) {
	if len(torrents) == 0 {
		return "", errors.New("缺少种子列表参数")
	}
	groups := normalizeIYUUBatchInput(torrents, maxGroups)
	if len(groups) == 0 {
		return "", errors.New("未找到可用的种子组（需包含 name/size）")
	}

	queryInfo := map[string]any{
		"processed_groups": 0,
		"total_groups":     len(groups),
		"force_query":      forceQuery,
		"max_groups":       maxGroups,
	}
	taskID := s.iyuuTasks.CreateTask(len(groups), queryInfo)

	go func() {
		if !s.iyuuRunning.CompareAndSwap(false, true) {
			s.iyuuTasks.FinishTask(taskID, false, "IYUU查询正在进行中，请稍后再试", map[string]any{}, queryInfo)
			return
		}
		defer s.iyuuRunning.Store(false)

		ctx := context.Background()
		stats, err := s.performIYUUQuery(ctx, groups, forceQuery, taskID, nil)
		if err != nil {
			s.iyuuTasks.FinishTask(taskID, false, "任务执行失败: "+err.Error(), map[string]any{}, queryInfo)
			return
		}
		message := fmt.Sprintf("批量 IYUU 查询完成：处理 %d 组，匹配 %d 组", intValue(stats["processed_groups"], 0), intValue(stats["matched_groups"], 0))
		s.iyuuTasks.FinishTask(taskID, true, message, stats, queryInfo)
	}()

	return taskID, nil
}

func (s *TorrentDataService) GetIYUUQueryBatchTask(taskID string) (*IYUUBatchTask, bool) {
	return s.iyuuTasks.GetTask(taskID)
}

func (s *TorrentDataService) groupsByPath(savePath string, limit int, anchorName string) ([]iyuuQueryGroup, int) {
	torrents, err := s.repo.ListTorrents(false, false)
	if err != nil {
		return []iyuuQueryGroup{}, 0
	}
	type key struct {
		name string
		size int64
	}
	groupMap := map[key]struct{}{}
	for _, torrent := range torrents {
		if strings.TrimSpace(torrent.SavePath) != savePath {
			continue
		}
		if strings.TrimSpace(torrent.Name) == "" {
			continue
		}
		if torrent.Size <= 207374182 {
			continue
		}
		if strings.TrimSpace(torrent.State) == "不存在" {
			continue
		}
		groupMap[key{name: torrent.Name, size: torrent.Size}] = struct{}{}
	}

	all := make([]iyuuQueryGroup, 0, len(groupMap))
	for item := range groupMap {
		all = append(all, iyuuQueryGroup{Name: item.name, Size: item.size})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Name == all[j].Name {
			return all[i].Size < all[j].Size
		}
		return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name)
	})
	total := len(all)
	if limit <= 0 || len(all) <= limit {
		return all, total
	}

	selected := make([]iyuuQueryGroup, 0, limit)
	if anchorName != "" {
		for _, group := range all {
			if strings.EqualFold(group.Name, anchorName) {
				selected = append(selected, group)
				break
			}
		}
	}
	for _, group := range all {
		if len(selected) >= limit {
			break
		}
		if anchorName != "" && strings.EqualFold(group.Name, anchorName) {
			continue
		}
		selected = append(selected, group)
	}
	return selected, total
}

// performIYUUQuery 执行真实 IYUU API 查询并将结果补全写入 torrents 表。
// 参数/返回：groups 为待查询的 name+size 组；forceQuery=false 时会按 iyuu_last_check 做间隔跳过；taskID 为空表示非批量任务；pathFilters 仅用于单个种子查询的路径过滤。
// 失败场景：IYUU API 调用失败、数据库读写失败。
// 副作用：发起远程 HTTP 请求；写入数据库（INSERT/UPDATE torrents）；更新内存批量任务状态（若 taskID 非空）。
func (s *TorrentDataService) performIYUUQuery(ctx context.Context, groups []iyuuQueryGroup, forceQuery bool, taskID string, pathFilters []string) (map[string]any, error) {
	client := newIYUUClient()

	cfg := s.cfg.Get()
	token := strings.TrimSpace(toString(cfg["iyuu_token"], ""))
	if token == "" {
		return map[string]any{
			"processed_groups": len(groups),
			"total_found":      0,
			"matched_groups":   0,
			"failed_groups":    0,
			"new_records":      0,
			"updated_records":  0,
			"sites_found":      []string{},
		}, nil
	}

	configuredSites, err := s.repo.ListDistinctTorrentSites()
	if err != nil {
		return nil, err
	}
	configuredSet := toStringSet(configuredSites)
	excluded := map[string]struct{}{"青蛙": {}, "柠檬不甜": {}}

	sidSha1, filteredSites, err := s.getFilteredSidSha1AndSites(ctx, client, token, configuredSites)
	if err != nil {
		return nil, err
	}
	sitesByID := map[int64]iyuuSite{}
	iyuuSupportedNicknames := map[string]struct{}{}
	for _, site := range filteredSites {
		sitesByID[site.ID] = site
		if strings.TrimSpace(site.Nickname) != "" {
			iyuuSupportedNicknames[strings.TrimSpace(site.Nickname)] = struct{}{}
		}
	}

	siteFieldToNickname, _ := s.repo.SiteFieldToNicknameMap()

	// 初始化 group state
	states := make([]*iyuuGroupState, 0, len(groups))
	stateMap := map[string]*iyuuGroupState{}
	for _, group := range groups {
		key := fmt.Sprintf("%s\x00%d", group.Name, group.Size)
		if _, ok := stateMap[key]; ok {
			continue
		}
		rows, err := s.repo.ListTorrentsByNameAndSizeForIYUU(group.Name, group.Size, pathFilters)
		if err != nil {
			rows = []repository.IYUUTorrentRow{}
		}

		filtered := make([]repository.IYUUTorrentRow, 0, len(rows))
		priority := make([]string, 0, len(rows))

		iyuuFirst := make([]repository.IYUUTorrentRow, 0, len(rows))
		other := make([]repository.IYUUTorrentRow, 0, len(rows))
		for _, row := range rows {
			siteName := strings.TrimSpace(ptrString(row.Sites))
			if siteName == "" {
				continue
			}
			if _, ok := excluded[siteName]; ok {
				continue
			}
			if _, ok := configuredSet[siteName]; !ok {
				continue
			}
			if _, ok := iyuuSupportedNicknames[siteName]; ok {
				iyuuFirst = append(iyuuFirst, row)
			} else {
				other = append(other, row)
			}
		}
		filtered = append(filtered, iyuuFirst...)
		filtered = append(filtered, other...)
		for _, row := range filtered {
			if strings.TrimSpace(row.Hash) != "" {
				priority = append(priority, strings.TrimSpace(row.Hash))
			}
		}
		totalAttempts := 3
		if len(priority) < totalAttempts {
			totalAttempts = len(priority)
		}

		state := &iyuuGroupState{
			Group:          group,
			Torrents:       rows,
			Filtered:       filtered,
			PriorityHashes: priority,
			TotalAttempts:  totalAttempts,
			Results:        []map[string]any{},
			Resolved:       totalAttempts == 0,
			Found:          false,
		}
		if len(filtered) > 0 {
			state.SelectedTorrent = &filtered[0]
			state.SelectedHash = strings.TrimSpace(filtered[0].Hash)
		}
		states = append(states, state)
		stateMap[key] = state
	}

	if taskID != "" {
		s.iyuuTasks.UpdateTask(taskID, func(task *IYUUBatchTask) {
			task.Message = fmt.Sprintf("准备查询 %d 组", len(states))
			task.Stats = map[string]any{"processed": 0, "total_found": 0, "matched_groups": 0, "failed_groups": 0, "sites_found": []string{}, "new_records": 0, "updated_records": 0}
		})
	}

	maxHashesPerRequest := 200
	for attempt := 0; attempt < 3; attempt++ {
		hashToStates := map[string][]*iyuuGroupState{}
		for _, st := range states {
			if st.Resolved {
				continue
			}
			if attempt >= st.TotalAttempts {
				st.Resolved = true
				st.Found = false
				continue
			}
			selectedHash := strings.ToLower(strings.TrimSpace(st.PriorityHashes[attempt]))
			if selectedHash == "" {
				continue
			}
			st.SelectedHash = selectedHash
			if len(st.Filtered) > attempt {
				st.SelectedTorrent = &st.Filtered[attempt]
			}
			hashToStates[selectedHash] = append(hashToStates[selectedHash], st)
		}
		if len(hashToStates) == 0 {
			continue
		}

		uniqueHashes := make([]string, 0, len(hashToStates))
		for h := range hashToStates {
			uniqueHashes = append(uniqueHashes, h)
		}
		sort.Strings(uniqueHashes)

		totalBatches := (len(uniqueHashes) + maxHashesPerRequest - 1) / maxHashesPerRequest
		iyuuInfof("批量查询 attempt %d/3: %d 个hash，共 %d 批", attempt+1, len(uniqueHashes), totalBatches)
		s.iyuuLogLine("INFO", fmt.Sprintf("批量查询 attempt %d/3: %d 个hash，共 %d 批", attempt+1, len(uniqueHashes), totalBatches))
		if taskID != "" {
			s.iyuuTasks.UpdateTask(taskID, func(task *IYUUBatchTask) {
				task.Message = fmt.Sprintf("IYUU 查询中：attempt %d/3（%d 批）", attempt+1, totalBatches)
			})
		}

		for batchIndex := 0; batchIndex < totalBatches; batchIndex++ {
			start := batchIndex * maxHashesPerRequest
			end := start + maxHashesPerRequest
			if end > len(uniqueHashes) {
				end = len(uniqueHashes)
			}
			batch := uniqueHashes[start:end]

			batchStart := time.Now()
			results, err := client.queryCrossSeedBatch(ctx, token, batch, sidSha1)
			if err != nil {
				return nil, err
			}
			cost := time.Since(batchStart)
			iyuuInfof("attempt %d/3, batch %d/%d: hash=%d 耗时=%s", attempt+1, batchIndex+1, totalBatches, len(batch), cost.Round(time.Millisecond))
			s.iyuuLogLine("INFO", fmt.Sprintf("attempt %d/3, batch %d/%d 完成：hash=%d，耗时=%s", attempt+1, batchIndex+1, totalBatches, len(batch), cost.Round(time.Millisecond)))

			for _, h := range batch {
				items := results[strings.ToLower(h)]
				for _, st := range hashToStates[strings.ToLower(h)] {
					if st.Resolved {
						continue
					}
					if len(items) > 0 {
						st.Results = items
						st.Found = true
						st.Resolved = true
					} else if attempt >= st.TotalAttempts-1 {
						st.Results = []map[string]any{}
						st.Found = false
						st.Resolved = true
					}
				}
			}

			if allResolved(states) {
				break
			}
		}
		if allResolved(states) {
			break
		}
	}

	// 写库与统计
	totalFound := 0
	matchedGroups := 0
	failedGroups := 0
	newRecords := 0
	updatedRecords := 0
	sitesSet := map[string]struct{}{}

	now := time.Now().Format("2006-01-02 15:04:05")
	processed := 0
	for _, st := range states {
		processed++
		name := strings.TrimSpace(st.Group.Name)
		size := st.Group.Size

		matchedSites := map[string]string{}
		if len(st.Results) > 0 {
			for _, item := range st.Results {
				sid := int64Value(item["sid"], 0)
				siteInfo, ok := sitesByID[sid]
				if !ok {
					continue
				}

				detailsPage := strings.TrimSpace(stringValue(item["details_page"], ""))
				torrentID := strings.TrimSpace(stringValue(item["torrent_id"], ""))
				if detailsPage == "" && torrentID == "" {
					continue
				}
				if !strings.Contains(detailsPage, "details.php") {
					if torrentID == "" {
						continue
					}
					detailsPage = "details.php?id=" + url.QueryEscape(torrentID)
				}
				detailsPage = strings.TrimLeft(detailsPage, "/")

				base := strings.TrimSpace(siteInfo.BaseURL)
				if base == "" {
					continue
				}
				base = strings.TrimRight(base, "/")
				fullURL := ""
				if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
					fullURL = base + "/" + detailsPage
				} else {
					fullURL = "https://" + base + "/" + detailsPage
				}
				fullURL = strings.Replace(fullURL, "://api.", "://kp.", 1)

				dbSiteName := ""
				if strings.TrimSpace(siteInfo.Site) != "" {
					if nick, ok := siteFieldToNickname[siteInfo.Site]; ok && strings.TrimSpace(nick) != "" {
						dbSiteName = strings.TrimSpace(nick)
					}
				}
				if dbSiteName == "" {
					dbSiteName = strings.TrimSpace(siteInfo.Nickname)
				}
				if dbSiteName == "" {
					continue
				}
				if _, ok := configuredSet[dbSiteName]; !ok {
					continue
				}
				if _, exists := matchedSites[dbSiteName]; !exists {
					matchedSites[dbSiteName] = fullURL
				}
			}
		}

		if len(matchedSites) > 0 {
			matchedGroups++
			totalFound += len(matchedSites)
			for site := range matchedSites {
				sitesSet[site] = struct{}{}
			}
		}

		baseSavePath := ""
		baseDownloaderID := ""
		selectedHash := st.SelectedHash
		if st.SelectedTorrent != nil {
			baseSavePath = strings.TrimSpace(ptrString(st.SelectedTorrent.SavePath))
			baseDownloaderID = strings.TrimSpace(st.SelectedTorrent.Downloader)
			if strings.TrimSpace(st.SelectedTorrent.Hash) != "" {
				selectedHash = strings.TrimSpace(st.SelectedTorrent.Hash)
			}
		}
		if baseDownloaderID == "" && len(st.Torrents) > 0 {
			baseDownloaderID = strings.TrimSpace(st.Torrents[0].Downloader)
		}

		insertedCount, insertErr := s.repo.InsertMissingIYUUSiteTorrents(name, size, baseSavePath, baseDownloaderID, selectedHash, matchedSites, now)
		if insertErr != nil {
			failedGroups++
			iyuuErrorf("写入缺失站点失败 name=%s size=%d err=%v", name, size, insertErr)
		} else {
			newRecords += insertedCount
		}

		filledDetails, err := s.repo.UpdateIYUUCheckAndFillDetails(name, size, matchedSites, now)
		if err != nil {
			failedGroups++
			iyuuErrorf("更新 iyuu_last_check/details 失败 name=%s size=%d err=%v", name, size, err)
			continue
		}
		updatedRecords += filledDetails

		if taskID != "" {
			s.iyuuTasks.UpdateTask(taskID, func(task *IYUUBatchTask) {
				task.Processed = processed
				task.Message = fmt.Sprintf("正在处理第 %d/%d 组", processed, len(states))
				task.Stats = map[string]any{
					"processed":        processed,
					"processed_groups": processed,
					"total_found":      totalFound,
					"matched_groups":   matchedGroups,
					"failed_groups":    failedGroups,
					"sites_found":      sortedKeys(sitesSet),
					"new_records":      newRecords,
					"updated_records":  updatedRecords,
				}
			})
		}
	}

	if taskID != "" {
		s.iyuuTasks.UpdateTask(taskID, func(task *IYUUBatchTask) {
			task.Processed = len(states)
		})
	}

	return map[string]any{
		"processed_groups": len(states),
		"total_found":      totalFound,
		"matched_groups":   matchedGroups,
		"failed_groups":    failedGroups,
		"new_records":      newRecords,
		"updated_records":  updatedRecords,
		"sites_found":      sortedKeys(sitesSet),
	}, nil
}

func allResolved(states []*iyuuGroupState) bool {
	for _, st := range states {
		if !st.Resolved {
			return false
		}
	}
	return true
}

func (s *TorrentDataService) iyuuLogLine(level string, message string) {
	s.iyuuMu.Lock()
	logger := s.iyuuLog
	s.iyuuMu.Unlock()
	if logger == nil {
		return
	}
	logger(level, time.Now().Format("2006-01-02 15:04:05")+" "+strings.TrimSpace(message))
}

func (s *TorrentDataService) iyuuPathFilters() []string {
	cfg := s.cfg.Get()
	settings, _ := cfg["iyuu_settings"].(map[string]any)
	if !toBool(settings["path_filter_enabled"], false) {
		return nil
	}
	raw := toSlice(settings["selected_paths"])
	paths := make([]string, 0, len(raw))
	for _, item := range raw {
		value := strings.TrimSpace(toString(item, ""))
		if value != "" {
			paths = append(paths, value)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	return paths
}

// getFilteredSidSha1AndSites 获取过滤后的 sid_sha1 和可辅种站点列表（仅保留 torrents 表存在的站点昵称）。
// 参数/返回：configuredSites 为当前 torrents 表出现过的站点列表；返回 sid_sha1 与站点列表。
// 失败场景：IYUU API 调用失败、缓存读写失败（读写失败会降级为重新获取）。
// 副作用：可能读取/写入本地缓存文件；发起远程 HTTP 请求。
func (s *TorrentDataService) getFilteredSidSha1AndSites(ctx context.Context, client *iyuuClient, token string, configuredSites []string) (string, []iyuuSite, error) {
	siteList := append([]string{}, configuredSites...)
	if sid, sites, ok := loadIYUUSiteCache(siteList); ok {
		return sid, sites, nil
	}

	supported, err := client.getSupportedSites(ctx, token)
	if err != nil {
		return "", nil, err
	}

	siteFieldToNickname, _ := s.repo.SiteFieldToNicknameMap()
	siteSet := toStringSet(siteList)

	ids := make([]int64, 0, len(supported))
	filtered := make([]iyuuSite, 0, len(supported))
	seen := map[int64]struct{}{}
	for _, site := range supported {
		if site.ID <= 0 {
			continue
		}
		if _, ok := seen[site.ID]; ok {
			continue
		}

		dbNick := ""
		if strings.TrimSpace(site.Site) != "" {
			if nick, ok := siteFieldToNickname[site.Site]; ok && strings.TrimSpace(nick) != "" {
				dbNick = strings.TrimSpace(nick)
				if _, ok := siteSet[dbNick]; ok {
					seen[site.ID] = struct{}{}
					ids = append(ids, site.ID)
					filtered = append(filtered, site)
					continue
				}
			}
		}

		if strings.TrimSpace(site.Nickname) != "" {
			if _, ok := siteSet[strings.TrimSpace(site.Nickname)]; ok {
				seen[site.ID] = struct{}{}
				ids = append(ids, site.ID)
				filtered = append(filtered, site)
			}
		}
	}
	if len(ids) == 0 {
		return "", nil, errors.New("没有找到在 torrents 表中存在的 IYUU 支持站点")
	}

	sidSha1, err := client.reportExisting(ctx, token, ids)
	if err != nil {
		return "", nil, err
	}
	if err := saveIYUUSiteCache(sidSha1, filtered, siteList); err != nil {
		logx.Warnf(iyuuLogModule, "写入 IYUU 缓存失败 err=%v", err)
	}
	return sidSha1, filtered, nil
}

func normalizeIYUUBatchInput(torrents []map[string]any, maxGroups int) []iyuuQueryGroup {
	if maxGroups <= 0 {
		maxGroups = 200
	}
	type key struct {
		name string
		size int64
	}
	seen := map[key]struct{}{}
	result := make([]iyuuQueryGroup, 0, len(torrents))

	for _, torrent := range torrents {
		name := strings.TrimSpace(stringValue(torrent["name"], ""))
		size := int64Value(torrent["size"], 0)
		if name == "" || size <= 0 {
			continue
		}
		itemKey := key{name: name, size: size}
		if _, exists := seen[itemKey]; exists {
			continue
		}
		seen[itemKey] = struct{}{}
		result = append(result, iyuuQueryGroup{Name: name, Size: size})
		if len(result) >= maxGroups {
			break
		}
	}
	return result
}
