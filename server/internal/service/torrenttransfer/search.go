package torrenttransfer

import (
	"fmt"
	"strconv"
	"strings"
)

type transferRequest struct {
	SourceDownloaderID string
	TargetDownloaderID string
	SiteName           string
	TorrentName        string
	TorrentSize        int64
	SavePath           string
}

func parseTransferRequest(payload map[string]any) (transferRequest, map[string]any, int) {
	rawSize, sizeExists := payload["torrent_size"]
	size, sizeOK := parsePythonInt(rawSize)
	req := transferRequest{
		SourceDownloaderID: strings.TrimSpace(transferToString(payload["source_downloader_id"], "")),
		TargetDownloaderID: strings.TrimSpace(transferToString(payload["target_downloader_id"], "")),
		SiteName:           strings.TrimSpace(transferToString(payload["site_name"], "")),
		TorrentName:        strings.TrimSpace(transferToString(payload["torrent_name"], "")),
		TorrentSize:        size,
		SavePath:           strings.TrimSpace(transferToString(payload["save_path"], "")),
	}

	if req.SourceDownloaderID == "" || req.TargetDownloaderID == "" || req.SiteName == "" || req.TorrentName == "" || !sizeExists || rawSize == nil {
		return transferRequest{}, map[string]any{
			"success": false,
			"message": "缺少必需参数: source_downloader_id, target_downloader_id, site_name, torrent_name, torrent_size",
		}, 400
	}
	if !sizeOK {
		return transferRequest{}, map[string]any{
			"success": false,
			"message": "torrent_size 必须是整数",
		}, 400
	}

	return req, nil, 0
}

func parsePythonInt(value any) (int64, bool) {
	switch typed := value.(type) {
	case nil:
		return 0, false
	case bool:
		if typed {
			return 1, true
		}
		return 0, true
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float32:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0, false
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func (s *TorrentTransferService) findSimilarTorrents(name string, size int64, siteName string) []map[string]any {
	if strings.TrimSpace(name) == "" {
		return []map[string]any{}
	}

	parts := strings.Fields(name)
	keyword := name
	if len(parts) > 0 {
		keyword = parts[0]
	}

	rows := make([]map[string]any, 0)
	db := s.repo.DB().Table("torrents").Select("hash, name, size, sites, save_path, downloader_id, state").Where("name LIKE ? AND (is_hidden = 0 OR is_hidden IS NULL)", "%"+keyword+"%")
	db = db.Where("state IS NULL OR state != ?", "不存在").Order("last_seen DESC").Limit(8)
	if err := db.Scan(&rows).Error; err != nil {
		return []map[string]any{}
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		rowSize := transferToInt64(row["size"])
		if size > 0 {
			delta := rowSize - size
			if delta < 0 {
				delta = -delta
			}
			if delta > size/5 && delta > 2*1024*1024*1024 {
				continue
			}
		}
		site := transferToString(row["sites"], "")
		if strings.TrimSpace(siteName) != "" && site != "" && !strings.EqualFold(siteName, site) {
			if !strings.Contains(strings.ToLower(site), strings.ToLower(siteName)) {
				continue
			}
		}
		result = append(result, map[string]any{
			"hash":          transferToString(row["hash"], ""),
			"name":          transferToString(row["name"], ""),
			"size":          rowSize,
			"sites":         site,
			"save_path":     transferToString(row["save_path"], ""),
			"downloader_id": transferToString(row["downloader_id"], ""),
			"state":         transferToString(row["state"], ""),
		})
	}
	return result
}

func (s *TorrentTransferService) Similar(payload map[string]any) (map[string]any, int) {
	if len(payload) == 0 {
		return map[string]any{"success": false, "message": "请求数据为空"}, 400
	}
	torrentName := strings.TrimSpace(transferToString(payload["torrent_name"], ""))
	rawSize, exists := payload["torrent_size"]
	if torrentName == "" || !exists || rawSize == nil {
		return map[string]any{"success": false, "message": "缺少必需参数: torrent_name, torrent_size"}, 400
	}
	torrentSize, ok := parsePythonInt(rawSize)
	if !ok {
		return map[string]any{"success": false, "message": "torrent_size 必须是整数"}, 400
	}

	similar := s.findSimilarTorrents(torrentName, torrentSize, "")
	return map[string]any{
		"success":  true,
		"message":  fmt.Sprintf("找到 %d 个相似的种子", len(similar)),
		"torrents": similar,
	}, 200
}
