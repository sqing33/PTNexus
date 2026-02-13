package crossseed

import (
	"fmt"
	"strings"
)

func (s *CrossSeedService) AddBatchRecord(payload map[string]any) (map[string]any, int) {
	if len(payload) == 0 {
		return map[string]any{"success": false, "error": "缺少请求数据"}, 400
	}
	required := []string{"batch_id", "torrent_id", "source_site", "target_site", "status"}
	for _, key := range required {
		if _, exists := payload[key]; !exists {
			return map[string]any{"success": false, "error": "缺少必需字段: " + key}, 400
		}
	}

	err := s.repo.DB().Table("batch_enhance_records").Create(map[string]any{
		"batch_id":              payload["batch_id"],
		"torrent_id":            payload["torrent_id"],
		"title":                 payload["title"],
		"source_site":           payload["source_site"],
		"target_site":           payload["target_site"],
		"video_size_gb":         payload["video_size_gb"],
		"status":                payload["status"],
		"success_url":           payload["success_url"],
		"error_detail":          payload["error_detail"],
		"downloader_add_result": payload["downloader_add_result"],
		"progress":              payload["progress"],
	}).Error
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, 500
	}
	return map[string]any{"success": true, "message": "记录添加成功"}, 200
}

func (s *CrossSeedService) QueryBatchRecords(params BatchRecordQueryParams) (map[string]any, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 50
	}
	if params.PageSize > 200 {
		params.PageSize = 200
	}
	db := s.repo.DB().Table("batch_enhance_records")

	if params.Status != "" {
		db = db.Where("status = ?", params.Status)
	}
	if params.BatchID != "" {
		db = db.Where("batch_id = ?", params.BatchID)
	}
	if params.Search != "" {
		search := "%" + strings.ToLower(params.Search) + "%"
		db = db.Where("LOWER(torrent_id) LIKE ? OR LOWER(source_site) LIKE ? OR LOWER(target_site) LIKE ?", search, search, search)
	}
	if params.StartTime != "" {
		if parsed, err := parseISOTime(params.StartTime); err == nil {
			db = db.Where("processed_at >= ?", parsed)
		}
	}
	if params.EndTime != "" {
		if parsed, err := parseISOTime(params.EndTime); err == nil {
			db = db.Where("processed_at <= ?", parsed)
		}
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, err
	}

	offset := (params.Page - 1) * params.PageSize
	records := make([]map[string]any, 0)
	if err := db.Select("id, title, batch_id, torrent_id, source_site, target_site, video_size_gb, status, success_url, error_detail, downloader_add_result, processed_at, progress").Order("processed_at DESC").Limit(params.PageSize).Offset(offset).Scan(&records).Error; err != nil {
		return nil, err
	}

	batchIDs := make([]string, 0)
	if err := s.repo.DB().Table("batch_enhance_records").Distinct("batch_id").Order("batch_id DESC").Limit(100).Pluck("batch_id", &batchIDs).Error; err != nil {
		batchIDs = []string{}
	}

	return map[string]any{
		"success":   true,
		"records":   records,
		"total":     total,
		"page":      params.Page,
		"page_size": params.PageSize,
		"batch_ids": batchIDs,
	}, nil
}

func (s *CrossSeedService) ClearBatchRecords(batchID string) (map[string]any, int) {
	if strings.TrimSpace(batchID) == "" {
		affected, err := s.repo.Exec("DELETE FROM batch_enhance_records")
		if err != nil {
			return map[string]any{"success": false, "error": err.Error()}, 500
		}
		return map[string]any{"success": true, "message": fmt.Sprintf("记录已清空，删除了 %d 条记录", affected)}, 200
	}
	affected, err := s.repo.Exec("DELETE FROM batch_enhance_records WHERE batch_id = ?", batchID)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, 500
	}
	return map[string]any{"success": true, "message": fmt.Sprintf("批次 %s 的记录已清空，删除了 %d 条记录", batchID, affected)}, 200
}
