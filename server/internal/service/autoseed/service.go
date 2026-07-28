package autoseed

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

const (
	moduleAutoSeed         = "自动发种"
	minDownloaderFreeBytes = int64(10 * 1024 * 1024 * 1024)
)

// EnqueueFn 定义自动发种向现有发布队列投递任务的函数签名。
type EnqueueFn func(payload map[string]any) (map[string]any, int)

// FetchSeedFn 定义自动发种复用详情抓取与种子下载链路的函数签名。
type FetchSeedFn func(payload map[string]any) (map[string]any, int)

// Service 编排自动发种规则、RSS 拉取、下载器推送、进度同步和发布入队。
// 参数/返回：依赖仓储、配置和发布队列函数；接口方法返回结果 map 与错误。
// 失败场景：配置缺失、RSS 请求失败、下载器不可用或数据库写入失败时返回 error。
// 副作用：会发起网络请求、写数据库、向下载器添加/删除任务，并启动后台 goroutine。
type Service struct {
	repo      *repository.AutoSeedRepository
	cfg       *config.Manager
	enqueueFn EnqueueFn
	fetchFn   FetchSeedFn

	stopCh    chan struct{}
	doneCh    chan struct{}
	triggerCh chan int64
	once      sync.Once
}

// NewService 创建自动发种服务实例。
func NewService(repo *repository.AutoSeedRepository, cfg *config.Manager) *Service {
	return &Service{
		repo:      repo,
		cfg:       cfg,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
		triggerCh: make(chan int64, 32),
	}
}

// SetEnqueueFn 注入发布队列入队函数。
func (s *Service) SetEnqueueFn(fn EnqueueFn) {
	if s == nil {
		return
	}
	s.enqueueFn = fn
}

// SetFetchSeedFn 注入详情抓取与种子下载函数。
func (s *Service) SetFetchSeedFn(fn FetchSeedFn) {
	if s == nil {
		return
	}
	s.fetchFn = fn
}

// Start 启动后台自动发种轮询任务。
func (s *Service) Start() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		go s.run()
		logx.Infof(moduleAutoSeed, "自动发种调度器已启动")
	})
}

// Stop 停止后台自动发种轮询任务。
func (s *Service) Stop() {
	if s == nil {
		return
	}
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
		<-s.doneCh
	}
}

// TriggerRule 手动触发指定规则立即拉取一次 RSS。
func (s *Service) TriggerRule(ruleID int64) {
	if s == nil || ruleID <= 0 {
		return
	}
	select {
	case s.triggerCh <- ruleID:
	default:
		logx.Warnf(moduleAutoSeed, "手动触发队列已满 rule_id=%d", ruleID)
	}
}

func (s *Service) run() {
	defer close(s.doneCh)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.processTick()
		case ruleID := <-s.triggerCh:
			s.processRuleByID(ruleID)
		case <-s.stopCh:
			return
		}
	}
}

func (s *Service) processTick() {
	if s == nil || s.repo == nil {
		return
	}
	now := time.Now()
	rules, err := s.repo.FindDueRules(now)
	if err != nil {
		logx.Warnf(moduleAutoSeed, "查询到期规则失败 err=%v", err)
		return
	}
	for idx := range rules {
		s.processRule(&rules[idx])
	}
	s.SyncProgressAndAutoPublish("")
}

func (s *Service) processRuleByID(ruleID int64) {
	rule, err := s.repo.GetRule(ruleID)
	if err != nil {
		logx.Warnf(moduleAutoSeed, "查询手动规则失败 rule_id=%d err=%v", ruleID, err)
		return
	}
	s.processRule(rule)
}

func (s *Service) processRule(rule *repository.AutoSeedRule) {
	if s == nil || s.repo == nil || rule == nil {
		return
	}
	next := time.Now().Add(time.Duration(clampInt(rule.PullIntervalMinutes, 1, 1440)) * time.Minute)

	root := s.rootConfig()
	downloader, downloaderErr := downloaderclient.FromConfig(root, strings.TrimSpace(rule.DownloaderID))
	if downloaderErr != nil {
		_ = s.repo.MarkRulePulled(rule.ID, next, "未获取到下载器: "+downloaderErr.Error(), rule.Enabled)
		logx.Warnf(moduleAutoSeed, "规则缺少下载器 rule_id=%d err=%v", rule.ID, downloaderErr)
		return
	}

	freeBytes, freeErr := downloader.FetchFreeSpaceBytes()
	if freeErr == nil && freeBytes > 0 && freeBytes < minDownloaderFreeBytes {
		reason := "下载器空间已满，可用空间不足 10GB"
		enabled := rule.Enabled
		if rule.AutoPause {
			enabled = false
		}
		_ = s.repo.MarkRulePulled(rule.ID, next, reason, enabled)
		logx.Warnf(moduleAutoSeed, "规则暂停 rule_id=%d reason=%s free_bytes=%d", rule.ID, reason, freeBytes)
		return
	}

	entries, err := fetchRSS(rule.RSSURL)
	if err != nil {
		_ = s.repo.MarkRulePulled(rule.ID, next, "RSS 拉取失败: "+err.Error(), rule.Enabled)
		logx.Warnf(moduleAutoSeed, "RSS 拉取失败 rule_id=%d err=%v", rule.ID, err)
		return
	}

	for _, entry := range entries {
		item := s.buildItemFromEntry(rule, entry)
		reason := rejectReason(rule, item)
		if reason != "" {
			item.Status = repository.AutoSeedItemStatusRejected
			item.RejectReason = reason
			_, _, _ = s.repo.UpsertItem(item)
			continue
		}
		created, isNew, err := s.repo.UpsertItem(item)
		if err != nil {
			logx.Warnf(moduleAutoSeed, "写入 RSS 记录失败 rule_id=%d name=%s err=%v", rule.ID, item.Name, err)
			continue
		}
		if !isNew || created == nil {
			continue
		}
		fetchResult, fetchReason := s.fetchItemDetails(created)
		if fetchReason != "" {
			_ = s.repo.MarkItemRejected(created.ID, fetchReason)
			continue
		}
		if reason := rejectReason(rule, created); reason != "" {
			_ = s.repo.MarkItemRejected(created.ID, reason)
			continue
		}
		skipChecking := false
		options := downloaderclient.AddTorrentOptions{
			Paused:       rule.AutoPause,
			Tags:         parseJSONStrings(rule.TagsJSON),
			SkipChecking: &skipChecking,
		}
		torrentPath := strings.TrimSpace(toString(fetchResult["torrent_path"], ""))
		var addErr error
		if torrentPath != "" {
			addErr = downloader.AddTorrentFileWithOptions(torrentPath, "", options)
		} else {
			addErr = downloader.AddTorrentURLWithOptions(created.TorrentURL, "", options)
		}
		if addErr != nil {
			_ = s.repo.MarkItemRejected(created.ID, "推送下载器失败: "+addErr.Error())
			continue
		}
		hash := s.findDownloaderHash(downloader, created.Name)
		_ = s.repo.MarkItemPushed(created.ID, hash, "")
	}

	_ = s.repo.MarkRulePulled(rule.ID, next, "", rule.Enabled)
}

// SyncProgressAndAutoPublish 同步下载器进度，并对已完成记录执行自动整理和发布。
func (s *Service) SyncProgressAndAutoPublish(downloaderID string) {
	items, err := s.repo.ListProgressItems(downloaderID)
	if err != nil {
		logx.Warnf(moduleAutoSeed, "查询进度记录失败 err=%v", err)
		return
	}
	root := s.rootConfig()
	byDownloader := map[string][]repository.AutoSeedItem{}
	for _, item := range items {
		if strings.TrimSpace(item.DownloaderID) == "" {
			continue
		}
		byDownloader[item.DownloaderID] = append(byDownloader[item.DownloaderID], item)
	}
	for id, rows := range byDownloader {
		d, err := downloaderclient.FromConfig(root, id)
		if err != nil {
			continue
		}
		snapshots, err := d.FetchTorrents()
		if err != nil {
			logx.Warnf(moduleAutoSeed, "同步下载器进度失败 downloader_id=%s err=%v", id, err)
			continue
		}
		for _, item := range rows {
			if snapshot, ok := matchSnapshot(item, snapshots); ok {
				downloaded := snapshot.Progress >= 99.9
				_ = s.repo.UpdateItemProgress(item.ID, snapshot.Progress, downloaded, snapshot.Hash)
				if downloaded && item.Status == repository.AutoSeedItemStatusPushed {
					s.autoOrganizeAndPublish(item)
				}
			}
		}
	}
}

func (s *Service) autoOrganizeAndPublish(item repository.AutoSeedItem) {
	rule, err := s.repo.GetRule(item.RuleID)
	if err != nil {
		return
	}
	nowText := time.Now().Format(repository.PublishQueueTimeLayout)
	if rule.AutoOrganize && item.Status == repository.AutoSeedItemStatusPushed {
		item.Status = repository.AutoSeedItemStatusOrganized
		item.OrganizedAt = &nowText
		if strings.TrimSpace(item.TorrentID) == "" {
			item.TorrentID = inferTorrentID(item)
		}
		if strings.TrimSpace(item.SiteName) == "" {
			item.SiteName = firstNonEmpty(item.SourceSite, rule.SourceSite)
		}
		_ = s.repo.UpdateItemBasics(&item)
	}
	if len(parseJSONStrings(rule.TargetSitesJSON)) > 0 && strings.TrimSpace(item.TorrentID) != "" {
		_, _ = s.PublishItems([]int64{item.ID}, parseJSONStrings(rule.TargetSitesJSON))
	}
}

// ListRules 返回自动发种规则列表。
func (s *Service) ListRules() ([]repository.AutoSeedRule, error) {
	return s.repo.ListRules()
}

// SaveRule 新增或更新自动发种规则。
func (s *Service) SaveRule(rule *repository.AutoSeedRule) error {
	normalizeRule(rule)
	if rule.ID > 0 {
		return s.repo.UpdateRule(rule)
	}
	return s.repo.CreateRule(rule)
}

// DeleteRule 删除自动发种规则。
func (s *Service) DeleteRule(id int64) error {
	return s.repo.DeleteRule(id)
}

// ListItems 查询自动发种列表，并附加最新发布日志摘要。
func (s *Service) ListItems(query repository.AutoSeedListQuery) ([]repository.AutoSeedItem, int64, error) {
	rows, total, err := s.repo.ListItems(query)
	if err != nil {
		return nil, 0, err
	}
	s.enrichItemSavePaths(rows)
	s.enrichItemPublishResults(rows)
	return rows, total, nil
}

// AddManualURL 将用户输入的种子地址加入自动发种列表，并自动抓取详情页数据和推送下载器。
func (s *Service) AddManualURL(torrentURL, downloaderID, sourceSite string) error {
	torrentURL = strings.TrimSpace(torrentURL)
	if torrentURL == "" {
		return errors.New("种子地址不能为空")
	}
	sourceSite = strings.TrimSpace(sourceSite)
	if sourceSite == "" {
		return errors.New("请选择源站")
	}
	torrentID := firstNonEmpty(inferTorrentIDFromURL(torrentURL), torrentURL)
	item := &repository.AutoSeedItem{
		RuleID:       0,
		SourceSite:   sourceSite,
		GUID:         stableID(torrentURL),
		TorrentURL:   torrentURL,
		DetailURL:    torrentURL,
		Name:         filepath.Base(strings.Split(torrentURL, "?")[0]),
		Status:       repository.AutoSeedItemStatusPending,
		DownloaderID: strings.TrimSpace(downloaderID),
		SiteName:     sourceSite,
		TorrentID:    torrentID,
	}
	if item.Name == "." || item.Name == "/" || strings.TrimSpace(item.Name) == "" {
		item.Name = torrentURL
	}
	if err := s.repo.CreateManualItem(item); err != nil {
		return err
	}
	fetchResult, fetchReason := s.fetchItemDetails(item)
	if fetchReason != "" {
		_ = s.repo.MarkItemRejected(item.ID, fetchReason)
		return nil
	}
	if reason := restrictedTagRejectReason(item); reason != "" {
		_ = s.repo.MarkItemRejected(item.ID, reason)
		return nil
	}
	if strings.TrimSpace(downloaderID) == "" {
		_ = s.repo.MarkItemRejected(item.ID, "未获取到下载器")
		return nil
	}
	d, err := downloaderclient.FromConfig(s.rootConfig(), downloaderID)
	if err != nil {
		_ = s.repo.MarkItemRejected(item.ID, "未获取到下载器: "+err.Error())
		return nil
	}
	addErr := error(nil)
	skipChecking := false
	torrentPath := toString(fetchResult["torrent_path"], "")
	if torrentPath != "" {
		addErr = d.AddTorrentFileWithOptions(torrentPath, "", downloaderclient.AddTorrentOptions{Paused: false, Tags: []string{"PT Nexus", "自动发种"}, SkipChecking: &skipChecking})
	} else {
		addErr = d.AddTorrentURLWithOptions(torrentURL, "", downloaderclient.AddTorrentOptions{Paused: false, Tags: []string{"PT Nexus", "自动发种"}, SkipChecking: &skipChecking})
	}
	if addErr != nil {
		_ = s.repo.MarkItemRejected(item.ID, "推送下载器失败: "+addErr.Error())
		return nil
	}
	downloaderHash := firstNonEmpty(toString(fetchResult["hash"], ""), s.findDownloaderHash(d, item.Name))
	_ = s.repo.MarkItemPushed(item.ID, downloaderHash, "")
	return nil
}

// OrganizeItem 保存人工整理后的基础种子信息。
func (s *Service) OrganizeItem(id int64, patch map[string]any) error {
	item, err := s.repo.GetItem(id)
	if err != nil {
		return err
	}
	item.Name = toString(patch["name"], item.Name)
	item.ResourceType = toString(patch["resource_type"], item.ResourceType)
	item.Medium = toString(patch["medium"], item.Medium)
	item.TorrentID = toString(patch["torrent_id"], item.TorrentID)
	item.SiteName = toString(patch["site_name"], item.SiteName)
	if tags, ok := patch["tags"].([]any); ok {
		item.TagsJSON = encodeStrings(anySliceToStrings(tags))
	}
	if strings.TrimSpace(item.TorrentID) != "" {
		row, err := s.repo.GetSeedParameter(item.TorrentID, firstNonEmpty(item.SiteName, item.SourceSite))
		if err != nil {
			logx.Warnf(moduleAutoSeed, "同步整理后的种子参数失败 item_id=%d torrent_id=%s site=%s err=%v", item.ID, item.TorrentID, firstNonEmpty(item.SiteName, item.SourceSite), err)
		} else {
			applySeedParameterRow(item, row)
		}
	}
	nowText := time.Now().Format(repository.PublishQueueTimeLayout)
	item.OrganizedAt = &nowText
	item.Status = repository.AutoSeedItemStatusOrganized
	return s.repo.UpdateItemBasics(item)
}

// PublishItems 将自动发种记录投递到已有发布队列。
func (s *Service) PublishItems(ids []int64, targetSites []string) (map[string]any, error) {
	if s == nil || s.enqueueFn == nil {
		return nil, errors.New("发布队列未初始化")
	}
	targetSites = compactStrings(targetSites)
	if len(ids) == 0 {
		return nil, errors.New("请选择要发布的种子")
	}
	if len(targetSites) == 0 {
		return nil, errors.New("请选择发布站点")
	}
	results := make([]map[string]any, 0, len(ids)*len(targetSites))
	for _, id := range ids {
		item, err := s.repo.GetItem(id)
		if err != nil {
			results = append(results, map[string]any{"id": id, "success": false, "message": err.Error()})
			continue
		}
		torrentID := strings.TrimSpace(item.TorrentID)
		if torrentID == "" {
			torrentID = inferTorrentID(*item)
		}
		siteName := strings.TrimSpace(firstNonEmpty(item.SiteName, item.SourceSite))
		if torrentID == "" || siteName == "" {
			results = append(results, map[string]any{"id": id, "success": false, "message": "缺少 torrent_id 或源站"})
			continue
		}
		currentSavePath := s.resolveItemCurrentSavePath(*item)
		interval, concurrency := s.resolveDownloaderPublishSettings(item.DownloaderID)
		now := time.Now()
		itemResults := make([]map[string]any, 0, len(targetSites))
		for idx, target := range targetSites {
			seed := map[string]any{
				"torrent_id":    torrentID,
				"site_name":     siteName,
				"nickname":      item.SourceSite,
				"downloader_id": item.DownloaderID,
			}
			if currentSavePath != "" {
				seed["save_path"] = currentSavePath
			}
			payload := map[string]any{
				"target_site_name": target,
				"publish_scene":    "auto_seed",
				"publish_trigger":  fmt.Sprintf("auto:%d", id),
				"seeds":            []any{seed},
			}
			wave := idx / concurrency
			if wave > 0 && interval > 0 {
				payload["scheduled_at"] = now.Add(time.Duration(wave) * interval).Format(repository.PublishQueueTimeLayout)
			}
			result, code := s.enqueueFn(payload)
			entry := map[string]any{"id": id, "target_site": target, "status": code, "result": result}
			results = append(results, entry)
			itemResults = append(itemResults, entry)
		}
		encoded, _ := json.Marshal(mergeAutoSeedPublishResults(item.PublishResultsJSON, itemResults))
		_ = s.repo.MarkItemPublished(id, string(encoded))
	}
	return map[string]any{"success": true, "results": results}, nil
}

// DeleteItems 删除记录，并尝试同步删除下载器中的任务和文件。
func (s *Service) DeleteItems(ids []int64, deleteFiles bool) (int64, error) {
	root := s.rootConfig()
	for _, id := range ids {
		item, err := s.repo.GetItem(id)
		if err != nil || strings.TrimSpace(item.DownloaderID) == "" || strings.TrimSpace(item.DownloaderHash) == "" {
			continue
		}
		d, err := downloaderclient.FromConfig(root, item.DownloaderID)
		if err != nil {
			continue
		}
		if err := d.DeleteTorrents([]string{item.DownloaderHash}, deleteFiles); err != nil {
			logx.Warnf(moduleAutoSeed, "删除下载器任务失败 item_id=%d err=%v", id, err)
		}
	}
	return s.repo.DeleteItems(ids)
}

// Progress 返回下载器进度页数据。
func (s *Service) Progress(downloaderID string) ([]repository.AutoSeedItem, error) {
	s.SyncProgressAndAutoPublish(downloaderID)
	rows, err := s.repo.ListProgressItems(downloaderID)
	if err != nil {
		return nil, err
	}
	s.enrichItemSavePaths(rows)
	return rows, nil
}

func (s *Service) rootConfig() map[string]any {
	if s == nil || s.cfg == nil {
		return map[string]any{}
	}
	return s.cfg.Get()
}

func (s *Service) buildItemFromEntry(rule *repository.AutoSeedRule, entry feedEntry) *repository.AutoSeedItem {
	resourceType, medium := classifyEntry(entry)
	torrentURL := firstNonEmpty(entry.EnclosureURL, entry.Link)
	return &repository.AutoSeedItem{
		RuleID:       rule.ID,
		SourceSite:   rule.SourceSite,
		GUID:         firstNonEmpty(entry.GUID, stableID(torrentURL+entry.Title)),
		TorrentURL:   torrentURL,
		DetailURL:    firstNonEmpty(entry.Link, torrentURL),
		Name:         entry.Title,
		SizeBytes:    entry.SizeBytes,
		ResourceType: resourceType,
		Medium:       medium,
		TagsJSON:     encodeStrings(entry.Categories),
		Status:       repository.AutoSeedItemStatusPending,
		DownloaderID: rule.DownloaderID,
		SiteName:     rule.SourceSite,
		TorrentID:    inferTorrentIDFromURL(firstNonEmpty(entry.Link, torrentURL)),
	}
}

// fetchItemDetails 抓取源站详情并回填自动发种记录，确保推送下载器前已取得源站标签。
// 参数/返回：item 为已写入数据库的自动发种记录；成功返回抓取结果，失败返回可直接展示的未推送原因。
// 失败场景：详情抓取函数未注入、缺少源站定位信息或抓取接口返回失败。
// 副作用：会发起源站请求，并更新自动发种记录及对应的种子参数。
func (s *Service) fetchItemDetails(item *repository.AutoSeedItem) (map[string]any, string) {
	if s == nil || s.fetchFn == nil {
		return nil, "未获取到源站标签，不允许下载"
	}
	if item == nil {
		return nil, "RSS 数据为空"
	}
	searchTerm := firstNonEmpty(item.TorrentID, item.DetailURL, item.TorrentURL)
	if strings.TrimSpace(item.SourceSite) == "" || strings.TrimSpace(searchTerm) == "" {
		return nil, "未获取到源站详情，不允许下载"
	}
	result, status := s.fetchFn(map[string]any{
		"sourceSite":           item.SourceSite,
		"searchTerm":           searchTerm,
		"torrentName":          item.Name,
		"downloaderId":         item.DownloaderID,
		"savePath":             "",
		"screenshotReviewMode": "background",
		"task_id":              fmt.Sprintf("auto-seed-%d", item.ID),
	})
	if status >= 400 || !boolFromAny(result["success"]) {
		return result, "详情页数据抓取失败: " + toString(result["message"], "未知错误")
	}
	s.applyFetchedDetails(item, result)
	return result, ""
}

func (s *Service) findDownloaderHash(d downloaderclient.Downloader, title string) string {
	snapshots, err := d.FetchTorrents()
	if err != nil {
		return ""
	}
	for _, item := range snapshots {
		if strings.TrimSpace(item.Name) == strings.TrimSpace(title) {
			return item.Hash
		}
	}
	return ""
}

func (s *Service) resolveItemCurrentSavePath(item repository.AutoSeedItem) string {
	if path := strings.TrimSpace(item.SavePath); path != "" {
		return path
	}
	downloaderID := strings.TrimSpace(item.DownloaderID)
	if s == nil || downloaderID == "" {
		return ""
	}
	downloader, err := downloaderclient.FromConfig(s.rootConfig(), downloaderID)
	if err != nil {
		logx.Warnf(moduleAutoSeed, "发布前回填下载器路径失败 downloader_id=%s err=%v", downloaderID, err)
		return ""
	}
	snapshots, err := downloader.FetchTorrents()
	if err != nil {
		logx.Warnf(moduleAutoSeed, "发布前拉取下载器任务失败 downloader_id=%s err=%v", downloaderID, err)
		return ""
	}
	if snapshot, ok := matchSnapshot(item, snapshots); ok {
		return bestSnapshotMediaPath(snapshot)
	}
	return ""
}

func (s *Service) applyFetchedDetails(item *repository.AutoSeedItem, fetchResult map[string]any) {
	if s == nil || s.repo == nil || item == nil {
		return
	}
	torrentID := firstNonEmpty(toString(fetchResult["torrent_id"], ""), item.TorrentID)
	siteName := firstNonEmpty(toString(fetchResult["nickname"], ""), toString(fetchResult["site_name"], ""), item.SiteName, item.SourceSite)
	row, err := s.repo.GetSeedParameter(torrentID, siteName)
	if err != nil {
		logx.Warnf(moduleAutoSeed, "查询抓取种子参数失败 item_id=%d torrent_id=%s site=%s err=%v", item.ID, torrentID, siteName, err)
	}

	item.TorrentID = torrentID
	item.SiteName = siteName
	item.DetailURL = firstNonEmpty(toString(fetchResult["detail_url"], ""), item.DetailURL)
	item.Name = firstNonEmpty(toString(row["title"], ""), toString(row["name"], ""), toString(fetchResult["name"], ""), item.Name)
	item.ResourceType = firstNonEmpty(toString(row["type"], ""), item.ResourceType)
	item.Medium = firstNonEmpty(toString(row["medium"], ""), item.Medium)
	if size := toInt64(fetchResult["size_bytes"], 0); size > 0 {
		item.SizeBytes = size
	}
	if tags := parseStringArrayAny(row["tags"]); len(tags) > 0 {
		item.TagsJSON = encodeStrings(tags)
	}
	if strings.TrimSpace(item.TagsJSON) == "" {
		item.TagsJSON = encodeStrings([]string{"PT Nexus", "自动发种"})
	}
	if err := s.repo.UpdateItemFetchedDetails(item); err != nil {
		logx.Warnf(moduleAutoSeed, "回填自动发种详情失败 item_id=%d err=%v", item.ID, err)
	}
}

func applySeedParameterRow(item *repository.AutoSeedItem, row map[string]any) {
	if item == nil || len(row) == 0 {
		return
	}
	item.TorrentID = firstNonEmpty(toString(row["torrent_id"], ""), item.TorrentID)
	item.SiteName = firstNonEmpty(toString(row["site_name"], ""), item.SiteName)
	item.Name = firstNonEmpty(toString(row["title"], ""), toString(row["name"], ""), item.Name)
	item.ResourceType = firstNonEmpty(toString(row["type"], ""), item.ResourceType)
	item.Medium = firstNonEmpty(toString(row["medium"], ""), item.Medium)
	if tags := parseStringArrayAny(row["tags"]); len(tags) > 0 {
		item.TagsJSON = encodeStrings(tags)
	}
}

// resolveDownloaderPublishSettings 从下载器配置读取自动发种发布间隔和并发数。
func (s *Service) resolveDownloaderPublishSettings(downloaderID string) (time.Duration, int) {
	concurrency := 1
	if s == nil || s.cfg == nil {
		return 0, concurrency
	}
	root := s.cfg.Get()
	for _, raw := range toSlice(root["downloaders"]) {
		item := toMap(raw)
		if strings.TrimSpace(toString(item["id"], "")) != strings.TrimSpace(downloaderID) {
			continue
		}
		minutes := toInt(item["publish_interval_minutes"], 0)
		if value := toInt(item["publish_concurrency"], 1); value > 0 {
			concurrency = value
		}
		if minutes <= 0 {
			return 0, concurrency
		}
		return time.Duration(minutes) * time.Minute, concurrency
	}
	return 0, concurrency
}

type feedEntry struct {
	Title        string
	Link         string
	GUID         string
	EnclosureURL string
	SizeBytes    int64
	Categories   []string
}

type rssDocument struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
	Entries []atomEntry `xml:"entry"`
}

type rssItem struct {
	Title     string `xml:"title"`
	Link      string `xml:"link"`
	GUID      string `xml:"guid"`
	Enclosure struct {
		URL    string `xml:"url,attr"`
		Length string `xml:"length,attr"`
	} `xml:"enclosure"`
	Categories []string `xml:"category"`
}

type atomEntry struct {
	Title string `xml:"title"`
	ID    string `xml:"id"`
	Links []struct {
		Href   string `xml:"href,attr"`
		Rel    string `xml:"rel,attr"`
		Length string `xml:"length,attr"`
	} `xml:"link"`
	Categories []struct {
		Term string `xml:"term,attr"`
	} `xml:"category"`
}

func fetchRSS(rssURL string) ([]feedEntry, error) {
	rssURL = strings.TrimSpace(rssURL)
	if rssURL == "" {
		return nil, errors.New("RSS 地址不能为空")
	}
	client := &http.Client{Timeout: 45 * time.Second}
	req, err := http.NewRequest(http.MethodGet, rssURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "PT Nexus AutoSeed")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	doc := rssDocument{}
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("RSS 解析失败: %w", err)
	}
	result := make([]feedEntry, 0, len(doc.Channel.Items)+len(doc.Entries))
	for _, item := range doc.Channel.Items {
		result = append(result, feedEntry{
			Title:        strings.TrimSpace(item.Title),
			Link:         strings.TrimSpace(item.Link),
			GUID:         strings.TrimSpace(item.GUID),
			EnclosureURL: strings.TrimSpace(item.Enclosure.URL),
			SizeBytes:    parseInt64(item.Enclosure.Length),
			Categories:   item.Categories,
		})
	}
	for _, entry := range doc.Entries {
		feed := feedEntry{Title: strings.TrimSpace(entry.Title), GUID: strings.TrimSpace(entry.ID)}
		for _, link := range entry.Links {
			if feed.Link == "" || strings.EqualFold(link.Rel, "alternate") {
				feed.Link = strings.TrimSpace(link.Href)
			}
			if strings.EqualFold(link.Rel, "enclosure") {
				feed.EnclosureURL = strings.TrimSpace(link.Href)
				feed.SizeBytes = parseInt64(link.Length)
			}
		}
		for _, category := range entry.Categories {
			feed.Categories = append(feed.Categories, category.Term)
		}
		result = append(result, feed)
	}
	return result, nil
}

func rejectReason(rule *repository.AutoSeedRule, item *repository.AutoSeedItem) string {
	if item == nil {
		return "RSS 数据为空"
	}
	if strings.TrimSpace(item.TorrentURL) == "" {
		return "未获取到种子地址"
	}
	if reason := restrictedTagRejectReason(item); reason != "" {
		return reason
	}
	if rule == nil {
		return ""
	}
	sizeGB := float64(item.SizeBytes) / 1024 / 1024 / 1024
	if rule.MinSizeGB > 0 && item.SizeBytes > 0 && sizeGB < rule.MinSizeGB {
		return "因大小限制"
	}
	if rule.MaxSizeGB > 0 && item.SizeBytes > 0 && sizeGB > rule.MaxSizeGB {
		return "因大小限制"
	}
	if allowed := parseJSONStrings(rule.TypesJSON); len(allowed) > 0 && !containsFold(allowed, item.ResourceType) {
		return "类型不符合"
	}
	if allowed := parseJSONStrings(rule.MediaJSON); len(allowed) > 0 && !containsFold(allowed, item.Medium) {
		return "媒介不符合"
	}
	return ""
}

func restrictedTagRejectReason(item *repository.AutoSeedItem) string {
	if item == nil {
		return ""
	}
	for _, rawTag := range parseJSONStrings(item.TagsJSON) {
		tag := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rawTag), "tag."))
		for _, restricted := range []string{"分集", "禁转", "限转"} {
			if strings.Contains(tag, restricted) {
				return fmt.Sprintf("因%s标签不允许下载", restricted)
			}
		}
	}
	return ""
}

func classifyEntry(entry feedEntry) (string, string) {
	text := strings.ToLower(entry.Title + " " + strings.Join(entry.Categories, " "))
	resourceType := "电影"
	if strings.Contains(text, "season") || strings.Contains(text, "episode") || strings.Contains(text, "s0") || strings.Contains(text, "剧") || strings.Contains(text, "tv") {
		resourceType = "电视剧"
	}
	medium := ""
	for _, candidate := range []string{"Blu-ray", "Remux", "WEB-DL", "WEBRip", "HDTV", "DVD", "UHD"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(candidate)) {
			medium = candidate
			break
		}
	}
	return resourceType, medium
}

func matchSnapshot(item repository.AutoSeedItem, snapshots []downloaderclient.TorrentSnapshot) (downloaderclient.TorrentSnapshot, bool) {
	hash := strings.ToLower(strings.TrimSpace(item.DownloaderHash))
	for _, snapshot := range snapshots {
		if hash != "" && strings.EqualFold(snapshot.Hash, hash) {
			return snapshot, true
		}
	}
	name := strings.TrimSpace(item.Name)
	for _, snapshot := range snapshots {
		if name != "" && strings.TrimSpace(snapshot.Name) == name {
			return snapshot, true
		}
	}
	return downloaderclient.TorrentSnapshot{}, false
}

func (s *Service) enrichItemSavePaths(items []repository.AutoSeedItem) {
	if s == nil || len(items) == 0 {
		return
	}
	root := s.rootConfig()
	byDownloader := map[string][]int{}
	for idx := range items {
		downloaderID := strings.TrimSpace(items[idx].DownloaderID)
		if downloaderID == "" {
			continue
		}
		byDownloader[downloaderID] = append(byDownloader[downloaderID], idx)
	}
	for downloaderID, indexes := range byDownloader {
		downloader, err := downloaderclient.FromConfig(root, downloaderID)
		if err != nil {
			continue
		}
		snapshots, err := downloader.FetchTorrents()
		if err != nil {
			logx.Warnf(moduleAutoSeed, "回填下载器保存路径失败 downloader_id=%s err=%v", downloaderID, err)
			continue
		}
		for _, idx := range indexes {
			if snapshot, ok := matchSnapshot(items[idx], snapshots); ok {
				items[idx].SavePath = bestSnapshotMediaPath(snapshot)
			}
		}
	}
}

func (s *Service) enrichItemPublishResults(items []repository.AutoSeedItem) {
	if s == nil || s.repo == nil || len(items) == 0 {
		return
	}
	logs, err := s.repo.FindPublishLogsForItems(items)
	if err != nil {
		logx.Warnf(moduleAutoSeed, "回填发布结果失败 err=%v", err)
		return
	}
	logsByTorrent := map[string][]repository.PublishLogEntry{}
	for _, entry := range logs {
		torrentID := strings.TrimSpace(entry.TorrentID)
		if torrentID == "" {
			continue
		}
		logsByTorrent[torrentID] = append(logsByTorrent[torrentID], entry)
	}
	for idx := range items {
		torrentID := strings.TrimSpace(items[idx].TorrentID)
		if torrentID == "" {
			continue
		}
		latestBySite := make([]map[string]any, 0)
		seen := map[string]struct{}{}
		for _, entry := range logsByTorrent[torrentID] {
			targetSite := strings.TrimSpace(entry.TargetSite)
			targetKey := strings.ToLower(targetSite)
			if targetKey == "" {
				continue
			}
			if _, ok := seen[targetKey]; ok {
				continue
			}
			seen[targetKey] = struct{}{}
			latestBySite = append(latestBySite, autoSeedPublishResultFromLog(entry))
		}
		if len(latestBySite) == 0 {
			continue
		}
		encoded, _ := json.Marshal(mergeAutoSeedPublishResults(items[idx].PublishResultsJSON, latestBySite))
		items[idx].PublishResultsJSON = string(encoded)
	}
}

func bestSnapshotMediaPath(snapshot downloaderclient.TorrentSnapshot) string {
	if path := strings.TrimSpace(snapshot.ContentPath); path != "" {
		return path
	}
	return strings.TrimSpace(snapshot.SavePath)
}

func mergeAutoSeedPublishResults(existingJSON string, next []map[string]any) []map[string]any {
	merged := make([]map[string]any, 0)
	indexBySite := map[string]int{}
	for _, entry := range parseAutoSeedPublishResults(existingJSON) {
		site := autoSeedPublishResultSite(entry)
		if site == "" {
			continue
		}
		key := strings.ToLower(site)
		if _, exists := indexBySite[key]; exists {
			continue
		}
		indexBySite[key] = len(merged)
		merged = append(merged, entry)
	}
	for _, entry := range next {
		normalized := normalizeAutoSeedPublishResult(entry)
		site := autoSeedPublishResultSite(normalized)
		if site == "" {
			continue
		}
		key := strings.ToLower(site)
		if idx, exists := indexBySite[key]; exists {
			merged[idx] = normalized
			continue
		}
		indexBySite[key] = len(merged)
		merged = append(merged, normalized)
	}
	return merged
}

func parseAutoSeedPublishResults(value string) []map[string]any {
	value = strings.TrimSpace(value)
	if value == "" {
		return []map[string]any{}
	}
	raw := []map[string]any{}
	if err := json.Unmarshal([]byte(value), &raw); err == nil {
		return raw
	}
	items := []any{}
	if err := json.Unmarshal([]byte(value), &items); err != nil {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if mapped, ok := item.(map[string]any); ok {
			result = append(result, mapped)
		}
	}
	return result
}

func normalizeAutoSeedPublishResult(entry map[string]any) map[string]any {
	if entry == nil {
		return map[string]any{}
	}
	if strings.TrimSpace(toString(entry["status_text"], "")) != "" || strings.TrimSpace(toString(entry["updated_at"], "")) != "" {
		return entry
	}
	result := toMap(entry["result"])
	statusText := strings.TrimSpace(toString(result["message"], "已入队"))
	success := boolFromAny(result["success"])
	if statusText == "" {
		if success {
			statusText = "已入队"
		} else {
			statusText = "发布失败"
		}
	}
	out := map[string]any{
		"target_site": autoSeedPublishResultSite(entry),
		"status":      toString(entry["status"], ""),
		"status_text": statusText,
		"result_url":  firstNonEmpty(toString(result["result_url"], ""), toString(result["url"], "")),
		"updated_at":  time.Now().Format(repository.PublishQueueTimeLayout),
	}
	return out
}

func autoSeedPublishResultFromLog(entry repository.PublishLogEntry) map[string]any {
	updatedAt := firstNonEmpty(entry.UpdatedAt, entry.CreatedAt)
	return map[string]any{
		"target_site":  strings.TrimSpace(entry.TargetSite),
		"status":       strings.TrimSpace(entry.Status),
		"status_text":  autoSeedPublishStatusText(entry.Status),
		"result_url":   strings.TrimSpace(entry.ResultURL),
		"updated_at":   updatedAt,
		"seeding_time": autoSeedElapsedText(updatedAt),
	}
}

func autoSeedPublishResultSite(entry map[string]any) string {
	result := toMap(entry["result"])
	return firstNonEmpty(
		toString(entry["target_site"], ""),
		toString(entry["targetSite"], ""),
		toString(result["target_site"], ""),
		toString(result["targetSite"], ""),
	)
}

func autoSeedPublishStatusText(status string) string {
	switch strings.TrimSpace(status) {
	case "success":
		return "发布成功"
	case "failed":
		return "发布失败"
	case "exists":
		return "已存在"
	case "edited":
		return "已更新"
	case "pre_check_limit":
		return "预检查限制"
	case "queued":
		return "等待发布"
	case "running":
		return "发布中"
	case "cancelled":
		return "已取消"
	default:
		return strings.TrimSpace(status)
	}
}

func autoSeedElapsedText(value string) string {
	start, err := time.ParseInLocation(repository.PublishQueueTimeLayout, strings.TrimSpace(value), time.Local)
	if err != nil || start.IsZero() {
		return ""
	}
	elapsed := time.Since(start)
	if elapsed < 0 {
		elapsed = 0
	}
	days := int(elapsed.Hours()) / 24
	hours := int(elapsed.Hours()) % 24
	minutes := int(elapsed.Minutes()) % 60
	if days > 0 {
		if hours > 0 {
			return fmt.Sprintf("%d天%d小时", days, hours)
		}
		return fmt.Sprintf("%d天", days)
	}
	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%d小时%d分钟", hours, minutes)
		}
		return fmt.Sprintf("%d小时", hours)
	}
	if minutes > 0 {
		return fmt.Sprintf("%d分钟", minutes)
	}
	return "刚刚"
}

func normalizeRule(rule *repository.AutoSeedRule) {
	if rule == nil {
		return
	}
	rule.Name = strings.TrimSpace(rule.Name)
	rule.SourceSite = strings.TrimSpace(rule.SourceSite)
	rule.RSSURL = strings.TrimSpace(rule.RSSURL)
	rule.DownloaderID = strings.TrimSpace(rule.DownloaderID)
	if rule.PullIntervalMinutes <= 0 {
		rule.PullIntervalMinutes = 30
	}
	if rule.PublishConcurrency <= 0 {
		rule.PublishConcurrency = 1
	}
	if strings.TrimSpace(rule.NextRunAt) == "" {
		rule.NextRunAt = time.Now().Format(repository.PublishQueueTimeLayout)
	}
}

func inferTorrentID(item repository.AutoSeedItem) string {
	return firstNonEmpty(item.TorrentID, inferTorrentIDFromURL(item.DetailURL), inferTorrentIDFromURL(item.TorrentURL), stableID(item.Name))
}

func inferTorrentIDFromURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		return value
	}
	for _, marker := range []string{"id=", "torrentid=", "torrent_id=", "/torrent/", "/dl/"} {
		idx := strings.LastIndex(strings.ToLower(value), marker)
		if idx < 0 {
			continue
		}
		raw := value[idx+len(marker):]
		for cut, ch := range raw {
			if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '-' || ch == '_' {
				continue
			}
			return raw[:cut]
		}
		return raw
	}
	return ""
}

func stableID(value string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.TrimSpace(value)))
	return strconv.FormatUint(h.Sum64(), 16)
}

func parseJSONStrings(value string) []string {
	items := []string{}
	_ = json.Unmarshal([]byte(strings.TrimSpace(value)), &items)
	return compactStrings(items)
}

func encodeStrings(items []string) string {
	encoded, _ := json.Marshal(compactStrings(items))
	return string(encoded)
}

func compactStrings(items []string) []string {
	result := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func anySliceToStrings(items []any) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, toString(item, ""))
	}
	return result
}

func containsFold(items []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), target) {
			return true
		}
	}
	return false
}

func firstNonEmpty(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func toString(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	if text, ok := value.(string); ok {
		if strings.TrimSpace(text) == "" {
			return fallback
		}
		return strings.TrimSpace(text)
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", value))
	if text == "" || text == "<nil>" {
		return fallback
	}
	return text
}

func toInt(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func toInt64(value any, fallback int64) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func toSlice(value any) []any {
	if typed, ok := value.([]any); ok {
		return typed
	}
	return []any{}
}

func toMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func boolFromAny(value any) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}
	if text, ok := value.(string); ok {
		trimmed := strings.ToLower(strings.TrimSpace(text))
		return trimmed == "1" || trimmed == "true" || trimmed == "yes"
	}
	return false
}

func parseStringArrayAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return compactStrings(typed)
	case []any:
		return compactStrings(anySliceToStrings(typed))
	case []byte:
		return parseStringArrayAny(string(typed))
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return []string{}
		}
		parsed := []string{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return compactStrings(parsed)
		}
		return compactStrings(strings.Split(trimmed, ","))
	default:
		return []string{}
	}
}
