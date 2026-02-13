package torrenttransfer

import (
	"time"
)

func (s *TorrentTransferService) Prepare(payload map[string]any) (map[string]any, int) {
	if len(payload) == 0 {
		return map[string]any{"success": false, "message": "请求数据为空"}, 400
	}
	req, errResp, status := parseTransferRequest(payload)
	if status != 0 {
		return errResp, status
	}

	if req.SourceDownloaderID == req.TargetDownloaderID {
		return map[string]any{
			"success": false,
			"message": "源下载器和目标下载器不能相同",
		}, 400
	}

	current, err := s.repo.GetCurrentTorrentByName(req.TorrentName)
	if err == nil {
		candidate := map[string]any{
			"hash":          transferToString(current["hash"], ""),
			"name":          transferToString(current["name"], req.TorrentName),
			"size":          transferToInt64(current["size"]),
			"sites":         transferToString(current["sites"], req.SiteName),
			"save_path":     transferToString(current["save_path"], req.SavePath),
			"downloader_id": transferToString(current["downloader_id"], req.SourceDownloaderID),
			"state":         transferToString(current["state"], ""),
		}
		if candidate["save_path"] == "" {
			candidate["save_path"] = req.SavePath
		}
		if transferToInt64(candidate["size"]) == 0 && req.TorrentSize > 0 {
			candidate["size"] = req.TorrentSize
		}
		return map[string]any{
			"success":     true,
			"message":     "找到 1 个匹配的种子",
			"found_count": 1,
			"torrents":    []map[string]any{candidate},
		}, 200
	}

	suggestions := s.findSimilarTorrents(req.TorrentName, req.TorrentSize, req.SiteName)
	return map[string]any{
		"success":          false,
		"message":          "未找到完全匹配的种子",
		"found_count":      0,
		"suggestion_count": len(suggestions),
		"suggestions":      suggestions,
		"step":             "find",
	}, 404
}

func (s *TorrentTransferService) Execute(payload map[string]any) (map[string]any, int) {
	if len(payload) == 0 {
		return map[string]any{"success": false, "message": "请求数据为空"}, 400
	}
	req, errResp, status := parseTransferRequest(payload)
	if status != 0 {
		return errResp, status
	}

	prepareResult, prepareStatus := s.Prepare(payload)
	if prepareStatus != 200 || !transferToBool(prepareResult["success"]) {
		if _, ok := prepareResult["step"]; !ok {
			prepareResult["step"] = "find"
		}
		if _, ok := prepareResult["found_count"]; !ok {
			prepareResult["found_count"] = 0
		}
		if prepareStatus == 0 {
			prepareStatus = 404
		}
		return prepareResult, prepareStatus
	}

	hashes := extractTransferHashes(prepareResult["torrents"])
	if len(hashes) == 0 {
		return map[string]any{
			"success":     false,
			"message":     "未找到可转移的种子 hash",
			"step":        "find",
			"found_count": 0,
		}, 404
	}

	pauseResult, pauseStatus := s.Pause(map[string]any{
		"downloader_id":  req.SourceDownloaderID,
		"torrent_hashes": hashes,
	})
	if pauseStatus != 200 || !transferToBool(pauseResult["success"]) {
		return map[string]any{
			"success":        false,
			"message":        transferToString(pauseResult["message"], "暂停种子失败"),
			"step":           "pause",
			"found_count":    len(hashes),
			"pause_result":   pauseResult,
			"prepare_result": prepareResult,
		}, 400
	}

	exportResult, exportStatus := s.Export(map[string]any{
		"downloader_id":  req.SourceDownloaderID,
		"torrent_hashes": hashes,
	})
	if exportStatus != 200 || !transferToBool(exportResult["success"]) {
		return map[string]any{
			"success":       false,
			"message":       transferToString(exportResult["message"], "导出种子文件失败"),
			"step":          "export",
			"method":        "torrent_file",
			"found_count":   len(hashes),
			"pause_result":  pauseResult,
			"export_result": exportResult,
		}, 400
	}

	exportedFiles := transferToStringList(exportResult["exported_files"])
	addResult, addStatus := s.Add(map[string]any{
		"target_downloader_id": req.TargetDownloaderID,
		"torrent_files":        exportedFiles,
		"save_path":            req.SavePath,
		"paused":               true,
	})
	if addStatus != 200 {
		return map[string]any{
			"success":       false,
			"message":       transferToString(addResult["message"], "添加种子到目标下载器失败"),
			"step":          "add",
			"method":        "torrent_file",
			"found_count":   len(hashes),
			"pause_result":  pauseResult,
			"export_result": exportResult,
			"add_result":    addResult,
		}, 400
	}

	success := transferToBool(addResult["success"])
	message := "种子转移部分成功"
	if success {
		message = "种子转移完成（使用种子文件）"
	}

	result := map[string]any{
		"success":              success,
		"message":              message,
		"step":                 "complete",
		"method":               "torrent_file",
		"found_count":          len(hashes),
		"pause_success":        true,
		"exported_count":       len(exportedFiles),
		"add_result":           addResult,
		"source_downloader_id": req.SourceDownloaderID,
		"target_downloader_id": req.TargetDownloaderID,
		"site_name":            req.SiteName,
		"torrent_name":         req.TorrentName,
		"save_path":            req.SavePath,
		"transferred_at":       time.Now().Format("2006-01-02 15:04:05"),
		"step_results": map[string]any{
			"pause":  pauseResult,
			"export": exportResult,
			"add":    addResult,
		},
	}
	if success {
		return result, 200
	}
	return result, 400
}

func extractTransferHashes(value any) []string {
	if value == nil {
		return []string{}
	}
	switch typed := value.(type) {
	case []map[string]any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			hash := transferToString(item["hash"], "")
			if hash != "" {
				result = append(result, hash)
			}
		}
		return result
	case []any:
		result := make([]string, 0, len(typed))
		for _, raw := range typed {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			hash := transferToString(item["hash"], "")
			if hash != "" {
				result = append(result, hash)
			}
		}
		return result
	default:
		return []string{}
	}
}
