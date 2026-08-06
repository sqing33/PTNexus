package torrentdata

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

const torrentDataLogModule = "一种多站"

// DeleteTorrentByHash 按 hash 列表删除一种多站中的当前种子记录，可选同步删除下载器任务和文件。
// 参数/返回：payload.hash/hashes 为 torrents.hash，delete_files 控制是否请求下载器删除文件；返回接口响应体和 HTTP 状态码。
// 失败场景：缺少 hash、未找到 torrents 记录、下载器删除失败或数据库删除失败时返回错误响应。
// 副作用：delete_files=true 时会按下载器分组删除任务和文件；成功后物理删除 torrents 与 torrent_upload_stats 中对应 hash 的记录。
func (s *TorrentDataService) DeleteTorrentByHash(payload map[string]any) (map[string]any, int) {
	hashes := append(toStringSlice(payload["hashes"]), stringValue(payload["hash"], ""))
	hashes = compactStrings(hashes)
	if len(hashes) == 0 {
		return map[string]any{"success": false, "error": "缺少 hash 参数"}, 400
	}

	rows, err := s.repo.ListTorrentsByHashes(hashes)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, 500
	}
	if len(rows) == 0 {
		return map[string]any{"success": false, "error": "未找到 hash 对应的种子记录"}, 404
	}

	deleteFiles := boolValue(payload["delete_files"])
	fileDeletedCount := int64(0)
	if deleteFiles {
		byDownloader := map[string][]string{}
		for _, row := range rows {
			downloaderID := strings.TrimSpace(row.Downloader)
			if downloaderID == "" {
				return map[string]any{"success": false, "error": fmt.Sprintf("种子记录缺少下载器信息，无法删除下载器任务和文件: hash=%s", row.Hash)}, 400
			}
			byDownloader[downloaderID] = append(byDownloader[downloaderID], row.Hash)
		}
		downloaderIDs := make([]string, 0, len(byDownloader))
		for downloaderID := range byDownloader {
			downloaderIDs = append(downloaderIDs, downloaderID)
		}
		sort.Strings(downloaderIDs)
		for _, downloaderID := range downloaderIDs {
			groupHashes := compactStrings(byDownloader[downloaderID])
			logx.Infof(torrentDataLogModule, "请求删除下载器任务 downloader_id=%s hashes=%d", downloaderID, len(groupHashes))
			downloader, err := downloaderclient.FromConfig(s.rootConfig(), downloaderID)
			if err != nil {
				return map[string]any{"success": false, "error": err.Error(), "file_delete_failed_count": 1, "file_delete_errors": []string{err.Error()}}, 502
			}
			if err := downloader.DeleteTorrents(groupHashes, true); err != nil {
				logx.Warnf(torrentDataLogModule, "删除下载器任务失败 downloader_id=%s hashes=%d err=%v", downloaderID, len(groupHashes), err)
				return map[string]any{"success": false, "error": err.Error(), "file_delete_failed_count": 1, "file_delete_errors": []string{err.Error()}}, 502
			}
			fileDeletedCount += int64(len(groupHashes))
			logx.Infof(torrentDataLogModule, "下载器任务删除请求已完成 downloader_id=%s hashes=%d", downloaderID, len(groupHashes))
		}
	}

	deleteHashes := make([]string, 0, len(rows))
	for _, row := range rows {
		deleteHashes = append(deleteHashes, row.Hash)
	}
	deleted, err := s.repo.DeleteTorrentsByHashes(deleteHashes)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, 500
	}
	if deleted == 0 {
		return map[string]any{"success": false, "error": "未删除任何种子记录"}, 404
	}

	message := fmt.Sprintf("已删除 %d 条种子记录", deleted)
	if deleteFiles {
		message = fmt.Sprintf("%s，并已请求删除 %d 个下载器任务和文件", message, fileDeletedCount)
	}
	return map[string]any{
		"success":                  true,
		"message":                  message,
		"deleted_count":            deleted,
		"file_deleted_count":       fileDeletedCount,
		"file_delete_failed_count": 0,
		"file_delete_errors":       []string{},
	}, 200

}

func (s *TorrentDataService) rootConfig() map[string]any {
	if s == nil || s.cfg == nil {
		return map[string]any{}
	}
	return s.cfg.Get()

}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		trimmed := strings.ToLower(strings.TrimSpace(typed))
		return trimmed == "true" || trimmed == "1" || trimmed == "yes" || trimmed == "on"
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	default:
		return false
	}

}

func compactStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
