package fetch

import (
	"errors"
	"path/filepath"
	"strings"

	processingshared "github.com/pt-nexus/server-go/internal/service/processing/shared"
)

// DownloadTorrentOnly 仅下载种子文件并返回路径、详情页与可选元信息。
// 参数/返回：payload 需包含 torrent_id/site_name；reader 用于读取站点配置；返回接口响应与状态码。
// 失败场景：参数缺失、站点配置不可用、下载失败时返回对应错误。
// 副作用：会发起网络请求并写入临时 torrent 文件。
func DownloadTorrentOnly(payload map[string]any, reader SiteInfoReader) (map[string]any, int) {
	torrentID := strings.TrimSpace(processingshared.ToString(payload["torrent_id"], ""))
	siteName := strings.TrimSpace(processingshared.ToString(payload["site_name"], ""))
	if torrentID == "" || siteName == "" {
		return map[string]any{"success": false, "message": "错误：缺少必要参数（torrent_id、site_name）"}, 400
	}

	sourceInfo, err := PrepareSourceSite(reader, siteName)
	if err != nil {
		return map[string]any{"success": false, "message": "错误：" + err.Error()}, 404
	}

	torrentPath, detailURL, torrentBytes, err := DownloadTorrentForSource(sourceInfo, torrentID)
	if err != nil {
		if errors.Is(err, ErrSourceCookieExpired) {
			return map[string]any{
				"success":    false,
				"message":    "种子文件下载失败: 源站点登录状态失效，请更新 Cookie 后重试",
				"error_code": "SOURCE_COOKIE_EXPIRED",
			}, 200
		}
		return map[string]any{"success": false, "message": "种子文件下载失败: " + err.Error()}, 500
	}

	meta, metaErr := ParseTorrentMeta(torrentBytes)
	response := map[string]any{
		"success":      true,
		"torrent_path": torrentPath,
		"torrent_dir":  filepath.Dir(torrentPath),
		"detail_url":   detailURL,
		"message":      "种子文件下载成功",
	}
	if metaErr == nil {
		response["hash"] = meta.InfoHash
		response["name"] = meta.Name
		response["size"] = meta.Size
	}
	return response, 200
}
