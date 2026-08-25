package fetch

import (
	"strings"
	"time"

	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
)

// BatchFetchRunnerDeps 定义批量抓取执行器依赖。
type BatchFetchRunnerDeps struct {
	FetchAndStore func(payload map[string]any) (map[string]any, int)
	OnResult      func(success bool, result map[string]any)
	Sleep         func(duration time.Duration)
}

// RunBatchFetch 按种子名称列表执行批量抓取流程。
// 参数/返回：torrentNames 为待抓取名称，sourcePriority 为站点优先级，rows 为 torrents 查询结果，deps 为流程回调依赖。
// 失败场景：单项抓取失败会记录失败结果并继续下一项，不中断整体任务。
// 副作用：会通过 FetchAndStore 触发抓取写库，并通过 OnResult 回调持续上报每项结果。
func RunBatchFetch(torrentNames []string, sourcePriority []string, rows []map[string]any, deps BatchFetchRunnerDeps) {
	byName := map[string][]map[string]any{}
	for _, row := range rows {
		name := strings.TrimSpace(toStringAny(row["name"], ""))
		if name == "" {
			continue
		}
		byName[name] = append(byName[name], row)
	}

	sleep := deps.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	emit := func(success bool, result map[string]any) {
		if deps.OnResult != nil {
			deps.OnResult(success, result)
		}
	}

	for _, name := range torrentNames {
		result := map[string]any{"name": name}
		entries := byName[name]
		if len(entries) == 0 {
			result["status"] = "failed"
			result["reason"] = "未找到种子记录"
			emit(false, result)
			continue
		}

		sourceSite, torrentID, matched := selectSourceByPriority(entries, sourcePriority)
		if !matched {
			result["status"] = "failed"
			result["reason"] = "未找到可用源站点或详情ID"
			result["attempted_sites"] = len(entries)
			emit(false, result)
			continue
		}

		savePath := strings.TrimSpace(toStringAny(entries[0]["save_path"], ""))
		downloaderID := strings.TrimSpace(toStringAny(entries[0]["downloader_id"], ""))
		if deps.FetchAndStore == nil {
			result["status"] = "failed"
			result["reason"] = "内部错误：FetchAndStore 回调未配置"
			result["source_site"] = sourceSite
			result["torrent_id"] = torrentID
			emit(false, result)
			continue
		}

		fetchResult, status := deps.FetchAndStore(map[string]any{
			"sourceSite":               sourceSite,
			"searchTerm":               torrentID,
			"torrentName":              name,
			"savePath":                 savePath,
			"downloaderId":             downloaderID,
			"screenshotReviewMode":     processingshared.ScreenshotReviewModeBackground,
			"refreshMediainfoForBatch": true,
		})
		if status != 200 || !isSuccessResult(fetchResult) {
			result["status"] = "failed"
			result["reason"] = toStringAny(fetchResult["message"], "抓取失败")
			result["source_site"] = sourceSite
			result["torrent_id"] = torrentID
			emit(false, result)
			continue
		}

		screenshotReviewStatus := processingshared.NormalizeScreenshotReviewStatus(toStringAny(fetchResult["screenshot_review_status"], processingshared.ScreenshotReviewStatusNone))
		if processingshared.NeedsScreenshotManualReview(screenshotReviewStatus) {
			result["status"] = "pending_review"
			result["message"] = "抓取成功，截图待人工确认"
		} else {
			result["status"] = "success"
			result["message"] = "抓取成功"
		}
		result["source_site"] = sourceSite
		result["torrent_id"] = torrentID
		result["task_id"] = toStringAny(fetchResult["task_id"], "")
		result["screenshot_review_status"] = screenshotReviewStatus
		emit(true, result)
		sleep(50 * time.Millisecond)
	}
}

func selectSourceByPriority(entries []map[string]any, sourcePriority []string) (string, string, bool) {
	sourceBySite := map[string]string{}
	for _, row := range entries {
		site := strings.TrimSpace(toStringAny(row["sites"], ""))
		if site == "" {
			continue
		}
		torrentID := extractTorrentIDFromDetails(toStringAny(row["details"], ""), toStringAny(row["hash"], ""))
		if torrentID == "" {
			continue
		}
		sourceBySite[site] = torrentID
	}
	if len(sourceBySite) == 0 {
		return "", "", false
	}

	for _, preferred := range sourcePriority {
		for site, torrentID := range sourceBySite {
			if strings.EqualFold(site, preferred) {
				return site, torrentID, true
			}
		}
	}
	for site, torrentID := range sourceBySite {
		return site, torrentID, true
	}
	return "", "", false
}

func isSuccessResult(result map[string]any) bool {
	if result == nil {
		return false
	}
	value, ok := result["success"]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		text := strings.ToLower(strings.TrimSpace(typed))
		return text == "true" || text == "1" || text == "yes" || text == "ok"
	case int:
		return typed != 0
	case int8:
		return typed != 0
	case int16:
		return typed != 0
	case int32:
		return typed != 0
	case int64:
		return typed != 0
	case uint:
		return typed != 0
	case uint8:
		return typed != 0
	case uint16:
		return typed != 0
	case uint32:
		return typed != 0
	case uint64:
		return typed != 0
	case float32:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return false
	}
}
