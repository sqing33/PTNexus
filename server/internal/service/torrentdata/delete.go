package torrentdata

import (
	"fmt"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

const torrentDataLogModule = "一种多站"

// DeleteTorrentByHash 按 hash 删除一种多站中的当前种子记录，可选同步删除下载器任务和文件。
// 参数/返回：payload.hash 为 torrents.hash，delete_files 控制是否请求下载器删除文件；返回接口响应体和 HTTP 状态码。
// 失败场景：缺少 hash、未找到 torrents 记录、下载器删除失败或数据库删除失败时返回错误响应。
// 副作用：delete_files=true 时会请求下载器删除任务和文件；成功后物理删除 torrents 与 torrent_upload_stats 中对应 hash 的记录。
func (s *TorrentDataService) DeleteTorrentByHash(payload map[string]any) (map[string]any, int) {
	hash := strings.TrimSpace(stringValue(payload["hash"], ""))
	if hash == "" {
		return map[string]any{"success": false, "error": "缺少 hash 参数"}, 400
	}

	row, found, err := s.repo.FindTorrentByHash(hash)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, 500
	}
	if !found {
		return map[string]any{"success": false, "error": "未找到 hash 对应的种子记录"}, 404
	}

	deleteFiles := boolValue(payload["delete_files"])
	fileDeleted := false
	if deleteFiles {
		downloaderID := strings.TrimSpace(row.Downloader)
		if downloaderID == "" {
			return map[string]any{"success": false, "error": "当前种子记录缺少下载器信息，无法删除下载器任务和文件"}, 400
		}
		logx.Infof(torrentDataLogModule, "请求删除下载器任务 downloader_id=%s hash=%s name=%s", downloaderID, row.Hash, row.Name)
		downloader, err := downloaderclient.FromConfig(s.rootConfig(), downloaderID)
		if err != nil {
			return map[string]any{"success": false, "error": err.Error(), "file_delete_failed_count": 1, "file_delete_errors": []string{err.Error()}}, 502
		}
		if err := downloader.DeleteTorrents([]string{row.Hash}, true); err != nil {
			logx.Warnf(torrentDataLogModule, "删除下载器任务失败 downloader_id=%s hash=%s err=%v", downloaderID, row.Hash, err)
			return map[string]any{"success": false, "error": err.Error(), "file_delete_failed_count": 1, "file_delete_errors": []string{err.Error()}}, 502
		}
		fileDeleted = true
		logx.Infof(torrentDataLogModule, "下载器任务删除请求已完成 downloader_id=%s hash=%s", downloaderID, row.Hash)
	}

	deleted, err := s.repo.DeleteTorrentByHash(row.Hash)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, 500
	}
	if deleted == 0 {
		return map[string]any{"success": false, "error": "未删除任何种子记录"}, 404
	}

	message := fmt.Sprintf("已删除种子记录：%s", row.Name)
	if deleteFiles {
		message += "，并已请求删除下载器任务和文件"
	}
	return map[string]any{
		"success":                  true,
		"message":                  message,
		"deleted_count":            deleted,
		"file_deleted_count":       boolCount(fileDeleted),
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

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
