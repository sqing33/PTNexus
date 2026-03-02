package service

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/pt-nexus/server-go/internal/platform/logx"
	migrationflow "github.com/pt-nexus/server-go/internal/service/migrationflow"
)

const (
	goProxyBatchLogModule = "Go增强-批量转种"
	minVideoSizeBytes     = int64(1024 * 1024 * 1024) // 1GiB，强制开启，不允许关闭
	bytesPerGB            = float64(1024 * 1024 * 1024)
	batchAutoAddNotRun    = `{"success": false, "message": "未执行"}`
)

// BatchEnhance 执行批量转种增强（强制启用 torrent 视频体积过滤）。
// 参数/返回：payload 需包含 target_site_name 与 seeds 数组；返回处理结果与 HTTP 状态码。
// 失败场景：参数缺失或迁移服务不可用时返回错误响应。
// 副作用：会触发种子下载、站点抓取、发布，并写入 publish_logs（触发为“批量转种-<批次>”）。
func (s *GoProxyService) BatchEnhance(payload map[string]any) (map[string]any, int) {
	targetSite := strings.TrimSpace(goProxyToString(payload["target_site_name"], ""))
	if targetSite == "" {
		return map[string]any{"success": false, "error": "缺少必需参数: target_site_name"}, 400
	}
	if s.migrate == nil {
		return map[string]any{"success": false, "error": "迁移服务未初始化"}, 500
	}

	rawSeeds, ok := payload["seeds"].([]any)
	if !ok || len(rawSeeds) == 0 {
		return map[string]any{"success": false, "error": "缺少必需参数: seeds"}, 400
	}

	if !s.tryStartBatchEnhance() {
		return map[string]any{"success": false, "error": "已有运行中的批量转种任务，请稍后再试"}, 200
	}
	defer s.finishBatchEnhance()

	batchID := fmt.Sprintf("batch_%d", time.Now().UnixNano())
	publishTrigger := "批量转种-1"
	batchNo := 1
	if trigger, no, err := s.migrate.NextBatchCrossSeedTrigger(); err == nil && strings.TrimSpace(trigger) != "" && no > 0 {
		publishTrigger = strings.TrimSpace(trigger)
		batchNo = no
	} else if err != nil {
		logx.Warnf(goProxyBatchLogModule, "计算批量转种批次失败，回退到默认批次 trigger=%s err=%v", publishTrigger, err)
	}

	insertExternalLog := func(input migrationflow.ExternalPublishLogInput) {
		if strings.TrimSpace(input.Trigger) == "" {
			input.Trigger = publishTrigger
		}
		if strings.TrimSpace(input.Scene) == "" {
			input.Scene = "multi_torrent"
		}
		if strings.TrimSpace(input.AutoAddResult) == "" {
			input.AutoAddResult = batchAutoAddNotRun
		}

		if err := s.migrate.InsertExternalPublishLog(input); err != nil {
			logx.Warnf(
				goProxyBatchLogModule,
				"写入发种日志失败 trigger=%s torrent_id=%s source_site=%s target_site=%s err=%v",
				input.Trigger,
				input.TorrentID,
				input.SourceSite,
				input.TargetSite,
				err,
			)
		}
	}

	logx.Infof(goProxyBatchLogModule, "批量转种开始 batch_id=%s trigger=%s target_site=%s seeds=%d", batchID, publishTrigger, targetSite, len(rawSeeds))

	seedsSuccess := 0
	seedsFailed := 0
	seedsFiltered := 0
	stopped := false

	for idx, raw := range rawSeeds {
		if s.isStopRequested() {
			stopped = true
			logx.Warnf(goProxyBatchLogModule, "收到停止信号 batch_id=%s stopped_at=%d/%d", batchID, idx, len(rawSeeds))
			break
		}

		seed, ok := raw.(map[string]any)
		if !ok {
			seedsFailed++
			continue
		}

		torrentID := strings.TrimSpace(goProxyToString(seed["torrent_id"], ""))
		sourceSite := strings.TrimSpace(goProxyToString(seed["nickname"], goProxyToString(seed["site_name"], "")))
		downloaderID := strings.TrimSpace(goProxyToString(seed["downloader_id"], ""))
		savePath := strings.TrimSpace(goProxyToString(seed["save_path"], ""))

		if torrentID == "" || sourceSite == "" {
			seedsFailed++
			title := strings.TrimSpace(torrentID)
			if title == "" {
				title = "-"
			}
			insertExternalLog(migrationflow.ExternalPublishLogInput{
				Trigger:       publishTrigger,
				Scene:         "multi_torrent",
				TorrentID:     torrentID,
				SourceSite:    sourceSite,
				TargetSite:    targetSite,
				DownloaderID:  downloaderID,
				Title:         title,
				Status:        "failed",
				Logs:          "缺少 torrent_id 或 site_name",
				AutoAddResult: batchAutoAddNotRun,
				CostMS:        0,
			})
			continue
		}

		title := strings.TrimSpace(goProxyToString(seed["title"], goProxyToString(seed["name"], "")))
		if title == "" {
			title = s.resolveSeedTitle(torrentID, sourceSite)
		}
		if title == "" {
			title = torrentID
		}

		videoBytes, videoGB, sizeErr := s.computeSeedVideoSize(torrentID, sourceSite)
		if sizeErr != nil {
			seedsFiltered++
			logx.Warnf(goProxyBatchLogModule, "大小过滤失败 batch_id=%s index=%d/%d torrent_id=%s source_site=%s err=%v", batchID, idx+1, len(rawSeeds), torrentID, sourceSite, sizeErr)
			insertExternalLog(migrationflow.ExternalPublishLogInput{
				Trigger:       publishTrigger,
				Scene:         "multi_torrent",
				TorrentID:     torrentID,
				SourceSite:    sourceSite,
				TargetSite:    targetSite,
				DownloaderID:  downloaderID,
				Title:         title,
				Status:        "filtered",
				Logs:          "大小过滤失败: " + sizeErr.Error(),
				AutoAddResult: batchAutoAddNotRun,
				CostMS:        0,
			})
			continue
		}
		if videoBytes < minVideoSizeBytes {
			seedsFiltered++
			logx.Infof(goProxyBatchLogModule, "大小过滤拦截 batch_id=%s index=%d/%d torrent_id=%s source_site=%s video_size_gb=%.2f", batchID, idx+1, len(rawSeeds), torrentID, sourceSite, videoGB)
			insertExternalLog(migrationflow.ExternalPublishLogInput{
				Trigger:       publishTrigger,
				Scene:         "multi_torrent",
				TorrentID:     torrentID,
				SourceSite:    sourceSite,
				TargetSite:    targetSite,
				DownloaderID:  downloaderID,
				Title:         title,
				Status:        "filtered",
				Logs:          fmt.Sprintf("小于 1.0GB（%.2fGB）", videoGB),
				AutoAddResult: batchAutoAddNotRun,
				CostMS:        0,
			})
			continue
		}

		fetchResult, fetchStatus := s.migrate.FetchAndStore(map[string]any{
			"sourceSite":   sourceSite,
			"searchTerm":   torrentID,
			"torrentName":  title,
			"savePath":     savePath,
			"downloaderId": downloaderID,
		})
		if fetchStatus != 200 || !toBool(fetchResult["success"], false) {
			seedsFailed++
			errorDetail := "抓取失败: " + goProxyToString(fetchResult["message"], "未知错误")
			insertExternalLog(migrationflow.ExternalPublishLogInput{
				Trigger:       publishTrigger,
				Scene:         "multi_torrent",
				TorrentID:     torrentID,
				SourceSite:    sourceSite,
				TargetSite:    targetSite,
				DownloaderID:  downloaderID,
				Title:         title,
				Status:        "failed",
				Logs:          errorDetail,
				AutoAddResult: batchAutoAddNotRun,
				CostMS:        0,
			})
			continue
		}

		taskID := strings.TrimSpace(goProxyToString(fetchResult["task_id"], ""))
		seed["title"] = title

		publishResult, publishStatus := s.migrate.Publish(map[string]any{
			"task_id":                taskID,
			"targetSite":             targetSite,
			"sourceSite":             sourceSite,
			"torrent_id":             torrentID,
			"upload_data":            seed,
			"downloaderId":           downloaderID,
			"savePath":               savePath,
			"auto_add_to_downloader": true,
			"publish_trigger":        publishTrigger,
			"publish_scene":          "multi_torrent",
		})
		if publishStatus == 200 && toBool(publishResult["success"], false) {
			seedsSuccess++
		} else {
			seedsFailed++
		}
	}

	logx.Infof(
		goProxyBatchLogModule,
		"批量转种结束 batch_id=%s target_site=%s success=%d failed=%d filtered=%d stopped=%v",
		batchID,
		targetSite,
		seedsSuccess,
		seedsFailed,
		seedsFiltered,
		stopped,
	)

	message := "批量转种请求已处理"
	if stopped {
		message = "批量转种任务已停止"
	}

	return map[string]any{
		"success": true,
		"data": map[string]any{
			"batch_id":        batchID,
			"batch_no":        batchNo,
			"publish_trigger": publishTrigger,
			"seeds_processed": seedsSuccess,
			"seeds_failed":    seedsFailed + seedsFiltered,
			"seeds_filtered":  seedsFiltered,
			"stopped":         stopped,
		},
		"message": message,
	}, 200
}

func (s *GoProxyService) tryStartBatchEnhance() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	s.stopRequested = false
	return true
}

func (s *GoProxyService) finishBatchEnhance() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.stopRequested = false
}

func (s *GoProxyService) isStopRequested() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stopRequested
}

func (s *GoProxyService) resolveSeedTitle(torrentID, sourceSite string) string {
	if s.migrate == nil {
		return strings.TrimSpace(torrentID)
	}
	title, err := s.migrate.QuerySeedTitle(torrentID, sourceSite)
	if err != nil {
		logx.Warnf(goProxyBatchLogModule, "查询种子标题失败 torrent_id=%s source_site=%s err=%v", torrentID, sourceSite, err)
		return strings.TrimSpace(torrentID)
	}
	if strings.TrimSpace(title) == "" {
		return strings.TrimSpace(torrentID)
	}
	return strings.TrimSpace(title)
}

func (s *GoProxyService) computeSeedVideoSize(torrentID, sourceSite string) (int64, float64, error) {
	if s.migrate == nil {
		return 0, 0, errors.New("迁移服务未初始化")
	}

	result, status := s.migrate.DownloadTorrentOnly(map[string]any{
		"torrent_id": torrentID,
		"site_name":  sourceSite,
	})
	if status != 200 || !toBool(result["success"], false) {
		message := strings.TrimSpace(goProxyToString(result["message"], "下载种子失败"))
		if message == "" {
			message = "下载种子失败"
		}
		return 0, 0, fmt.Errorf("下载种子失败: %s", message)
	}

	torrentPath := strings.TrimSpace(goProxyToString(result["torrent_path"], ""))
	if torrentPath == "" {
		return 0, 0, errors.New("下载种子成功但未返回 torrent_path")
	}

	videoBytes, _, err := s.migrate.ExtractVideoSizeFromTorrentFile(torrentPath)
	if err != nil {
		return 0, 0, err
	}

	videoGB := float64(videoBytes) / bytesPerGB
	if videoGB > 0 {
		videoGB = math.Round(videoGB*100) / 100
	}
	return videoBytes, videoGB, nil
}
