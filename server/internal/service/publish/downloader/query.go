package downloader

import (
	"strings"

	processingshared "github.com/pt-nexus/server-go/internal/service/processing/shared"
)

// DownloaderInfoRepo 定义查询下载器信息所需的最小仓储接口。
type DownloaderInfoRepo interface {
	GetSeedParameter(torrentID string, siteName string) (map[string]any, error)
	GetCurrentTorrentByName(name string) (map[string]any, error)
}

// GetDownloaderInfo 读取数据库中的种子信息并反查当前下载器与保存路径。
// 参数/返回：payload 需包含 torrent_id/site_name；repo 提供必要查询；返回接口响应与状态码。
// 失败场景：缺少参数、种子不存在、当前下载器记录不存在或仓储为空。
// 副作用：读取数据库，无写操作。
func GetDownloaderInfo(payload map[string]any, repo DownloaderInfoRepo) (map[string]any, int) {
	if repo == nil {
		return map[string]any{"success": false, "message": "服务未初始化"}, 500
	}
	torrentID := strings.TrimSpace(processingshared.ToString(payload["torrent_id"], ""))
	siteName := strings.TrimSpace(processingshared.ToString(payload["site_name"], ""))
	if torrentID == "" || siteName == "" {
		return map[string]any{"success": false, "message": "缺少必要参数: torrent_id 或 site_name"}, 400
	}

	seed, err := repo.GetSeedParameter(torrentID, siteName)
	if err != nil {
		return map[string]any{"success": false, "message": "未找到该种子信息"}, 404
	}
	name := strings.TrimSpace(processingshared.ToString(seed["name"], ""))
	if name == "" {
		return map[string]any{"success": false, "message": "未找到该种子信息"}, 404
	}
	current, err := repo.GetCurrentTorrentByName(name)
	if err != nil {
		return map[string]any{"success": false, "message": "未找到该种子信息"}, 404
	}
	return map[string]any{
		"success":       true,
		"downloader_id": strings.TrimSpace(processingshared.ToString(current["downloader_id"], "")),
		"save_path":     strings.TrimSpace(processingshared.ToString(current["save_path"], "")),
	}, 200
}
