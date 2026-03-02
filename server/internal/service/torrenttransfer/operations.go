package torrenttransfer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

func (s *TorrentTransferService) Pause(payload map[string]any) (map[string]any, int) {
	if len(payload) == 0 {
		return map[string]any{"success": false, "message": "请求数据为空"}, 400
	}
	downloaderID := strings.TrimSpace(transferToString(payload["downloader_id"], ""))
	torrentHashes := transferToStringList(payload["torrent_hashes"])
	if downloaderID == "" {
		return map[string]any{"success": false, "message": "缺少必需参数: downloader_id"}, 400
	}
	if len(torrentHashes) == 0 {
		return map[string]any{"success": false, "message": "torrent_hashes 必须是非空数组"}, 400
	}

	downloader, err := downloaderclient.FromConfig(s.cfg.Get(), downloaderID)
	if err != nil {
		return map[string]any{"success": false, "message": "读取下载器配置失败: " + err.Error()}, 400
	}
	if err := downloader.Pause(torrentHashes); err != nil {
		return map[string]any{"success": false, "message": "暂停下载器种子失败: " + err.Error()}, 500
	}

	updates := map[string]any{
		"state":     "已暂停",
		"last_seen": time.Now().Format("2006-01-02 15:04:05"),
	}
	result := s.repo.DB().Table("torrents").Where("downloader_id = ? AND hash IN ?", downloaderID, torrentHashes).Updates(updates)
	if result.Error != nil {
		return map[string]any{"success": false, "message": "同步暂停状态失败: " + result.Error.Error()}, 500
	}

	return map[string]any{
		"success":  true,
		"message":  fmt.Sprintf("成功暂停 %d 个种子", len(torrentHashes)),
		"affected": result.RowsAffected,
	}, 200
}

func (s *TorrentTransferService) Export(payload map[string]any) (map[string]any, int) {
	if len(payload) == 0 {
		return map[string]any{"success": false, "message": "请求数据为空"}, 400
	}
	downloaderID := strings.TrimSpace(transferToString(payload["downloader_id"], ""))
	torrentHashes := transferToStringList(payload["torrent_hashes"])
	if downloaderID == "" {
		return map[string]any{"success": false, "message": "缺少必需参数: downloader_id"}, 400
	}
	if len(torrentHashes) == 0 {
		return map[string]any{"success": false, "message": "torrent_hashes 必须是非空数组"}, 400
	}

	downloader, err := downloaderclient.FromConfig(s.cfg.Get(), downloaderID)
	if err != nil {
		return map[string]any{"success": false, "message": "读取下载器配置失败: " + err.Error()}, 400
	}

	exportDir := filepath.Join(os.TempDir(), fmt.Sprintf("pt_nexus_export_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		return map[string]any{"success": false, "message": "创建导出目录失败: " + err.Error()}, 500
	}

	exported := make([]string, 0, len(torrentHashes))
	failed := make([]map[string]any, 0)
	for _, hash := range torrentHashes {
		content, exportErr := downloader.ExportTorrent(hash)
		if exportErr != nil {
			failed = append(failed, map[string]any{"hash": hash, "error": exportErr.Error()})
			continue
		}
		filePath := filepath.Join(exportDir, sanitizeTransferFilePart(hash)+".torrent")
		if err := os.WriteFile(filePath, content, 0o644); err != nil {
			failed = append(failed, map[string]any{"hash": hash, "error": err.Error()})
			continue
		}
		exported = append(exported, filePath)
	}
	if len(exported) == 0 {
		return map[string]any{"success": false, "message": "导出种子文件失败", "failed": failed}, 500
	}

	return map[string]any{
		"success":        true,
		"message":        fmt.Sprintf("成功导出 %d 个种子文件", len(exported)),
		"exported_files": exported,
		"export_dir":     exportDir,
		"failed":         failed,
	}, 200
}

func (s *TorrentTransferService) Add(payload map[string]any) (map[string]any, int) {
	if len(payload) == 0 {
		return map[string]any{"success": false, "message": "请求数据为空"}, 400
	}
	targetDownloaderID := strings.TrimSpace(transferToString(payload["target_downloader_id"], ""))
	torrentFiles := transferToStringList(payload["torrent_files"])
	savePath := strings.TrimSpace(transferToString(payload["save_path"], ""))
	paused := transferToBool(payload["paused"])

	if targetDownloaderID == "" {
		return map[string]any{"success": false, "message": "缺少必需参数: target_downloader_id"}, 400
	}
	if len(torrentFiles) == 0 {
		return map[string]any{"success": false, "message": "torrent_files 必须是非空数组"}, 400
	}

	downloader, err := downloaderclient.FromConfig(s.cfg.Get(), targetDownloaderID)
	if err != nil {
		return map[string]any{"success": false, "message": "读取下载器配置失败: " + err.Error()}, 400
	}

	results := make([]map[string]any, 0, len(torrentFiles))
	successCount := 0
	failedCount := 0

	for _, file := range torrentFiles {
		entry := map[string]any{"file": file, "success": false}
		trimmed := strings.TrimSpace(file)
		if trimmed == "" {
			entry["message"] = "文件路径为空"
			failedCount++
			results = append(results, entry)
			continue
		}
		if err := downloader.AddTorrentFile(trimmed, savePath, paused, nil); err != nil {
			entry["message"] = "添加失败: " + err.Error()
			failedCount++
			results = append(results, entry)
			continue
		}
		entry["success"] = true
		entry["message"] = "添加成功"
		successCount++
		results = append(results, entry)
	}

	return map[string]any{
		"success":       failedCount == 0 && successCount > 0,
		"message":       fmt.Sprintf("添加种子完成: 成功 %d, 失败 %d", successCount, failedCount),
		"success_count": successCount,
		"failed_count":  failedCount,
		"results":       results,
		"save_path":     savePath,
		"paused":        paused,
	}, 200
}
