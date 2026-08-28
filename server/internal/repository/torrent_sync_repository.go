package repository

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

const (
	torrentHideReasonMissingSnapshot    = "missing_in_snapshot"
	torrentHideReasonRemovedDownloader  = "removed_downloader"
	torrentHideReasonDisabledDownloader = "disabled_downloader"
)

// SiteIdentity 描述站点识别所需的基础字段，用于从 tracker/comment 反查站点昵称。
type SiteIdentity struct {
	Nickname             string
	Site                 string
	BaseURL              string
	SpecialTrackerDomain string
	SiteGroup            string `gorm:"column:site_group"`
}

// TorrentSyncRecord 表示一条待写入数据库的标准化种子记录。
type TorrentSyncRecord struct {
	Hash         string
	Name         string
	SavePath     string
	Size         int64
	Progress     float64
	State        string
	Sites        string
	Details      string
	TorrentGroup string
	OfficialSite string
	DownloaderID string
	Seeders      int64
	Uploaded     int64
}

// TorrentSyncStats 描述一次下载器种子同步的写库统计。
type TorrentSyncStats struct {
	Inserted       int64
	Updated        int64
	Deleted        int64
	UpsertedUpload int64
}

type torrentUpsertRow struct {
	Hash         string  `gorm:"column:hash"`
	Name         string  `gorm:"column:name"`
	SavePath     string  `gorm:"column:save_path"`
	Size         int64   `gorm:"column:size"`
	Progress     float64 `gorm:"column:progress"`
	State        string  `gorm:"column:state"`
	Sites        string  `gorm:"column:sites"`
	Details      string  `gorm:"column:details"`
	TorrentGroup string  `gorm:"column:group"`
	OfficialSite string  `gorm:"column:official_site"`
	DownloaderID string  `gorm:"column:downloader_id"`
	LastSeen     string  `gorm:"column:last_seen"`
	Seeders      int64   `gorm:"column:seeders"`
	IsHidden     int     `gorm:"column:is_hidden"`
	HiddenReason string  `gorm:"column:hidden_reason"`
	HiddenAt     *string `gorm:"column:hidden_at"`
}

type torrentUploadUpsertRow struct {
	Hash         string  `gorm:"column:hash"`
	DownloaderID string  `gorm:"column:downloader_id"`
	Uploaded     int64   `gorm:"column:uploaded"`
	IsHidden     int     `gorm:"column:is_hidden"`
	HiddenReason string  `gorm:"column:hidden_reason"`
	HiddenAt     *string `gorm:"column:hidden_at"`
}

// ListSiteIdentities 读取站点识别所需元数据。
// 参数/返回：成功返回站点身份列表（nickname/site/base_url/special_tracker_domain）。
// 失败场景：数据库查询失败时返回错误。
// 副作用：无副作用，仅读取 sites 表。
func (r *TorrentDataRepository) ListSiteIdentities() ([]SiteIdentity, error) {
	rows := make([]SiteIdentity, 0)
	groupColumn := r.store.GroupColumn()
	err := r.store.DB.Table("sites").
		Select("nickname, site, base_url, special_tracker_domain, " + groupColumn + " AS site_group").
		Where("nickname IS NOT NULL AND nickname != ''").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// SyncDownloaderTorrents 以 downloader_id 为边界增量同步种子数据。
// 参数/返回：records 为当前下载器拉取到的全量快照，lastSeen 为本次同步时间。
// 失败场景：查询现存 hash、批量 upsert 或隐藏失败时返回错误。
// 副作用：写入 torrents/torrent_upload_stats，并将该下载器缺失种子标记为隐藏。
func (r *TorrentDataRepository) SyncDownloaderTorrents(downloaderID string, records []TorrentSyncRecord, lastSeen string) (TorrentSyncStats, error) {
	stats := TorrentSyncStats{}
	trimmedID := strings.TrimSpace(downloaderID)
	if trimmedID == "" {
		return stats, fmt.Errorf("downloader_id 不能为空")
	}
	lastSeen = strings.TrimSpace(lastSeen)
	if lastSeen == "" {
		return stats, fmt.Errorf("last_seen 不能为空")
	}

	existingRows := make([]struct {
		Hash    string  `gorm:"column:hash"`
		Name    string  `gorm:"column:name"`
		Size    int64   `gorm:"column:size"`
		State   string  `gorm:"column:state"`
		Details *string `gorm:"column:details"`
	}, 0)
	if err := r.store.DB.Table("torrents").Select("hash, name, size, state, details").Where("downloader_id = ?", trimmedID).Scan(&existingRows).Error; err != nil {
		return stats, err
	}

	existingSet := map[string]struct{}{}
	existingDetails := map[string]string{}
	for _, row := range existingRows {
		trimmed := strings.TrimSpace(row.Hash)
		if trimmed == "" {
			continue
		}
		existingSet[trimmed] = struct{}{}
		if row.Details != nil {
			if value := strings.TrimSpace(*row.Details); value != "" {
				existingDetails[trimmed] = value
				existingDetails[strings.ToLower(trimmed)] = value
			}
		}
	}

	recordMap := map[string]TorrentSyncRecord{}
	for _, record := range records {
		hash := strings.TrimSpace(record.Hash)
		name := strings.TrimSpace(record.Name)
		if hash == "" || name == "" {
			continue
		}
		record.Hash = hash
		record.Name = name
		record.DownloaderID = trimmedID
		if strings.TrimSpace(record.Details) == "" {
			existingDetail := strings.TrimSpace(existingDetails[hash])
			if existingDetail == "" {
				existingDetail = strings.TrimSpace(existingDetails[strings.ToLower(hash)])
			}
			if existingDetail != "" {
				record.Details = existingDetail
			}
		}
		recordMap[hash] = record
	}

	for hash := range recordMap {
		if _, exists := existingSet[hash]; exists {
			stats.Updated++
		} else {
			stats.Inserted++
		}
	}

	snapshotGroupSet := map[string]struct{}{}
	for _, record := range recordMap {
		name := strings.TrimSpace(record.Name)
		if name == "" || record.Size <= 0 {
			continue
		}
		snapshotGroupSet[fmt.Sprintf("%s\x00%d", name, record.Size)] = struct{}{}
	}

	deletedHashes := make([]string, 0)
	for _, row := range existingRows {
		hash := strings.TrimSpace(row.Hash)
		if hash == "" {
			continue
		}
		if _, exists := recordMap[hash]; exists {
			continue
		}

		// 规则：IYUU 插入的“未做种”占位记录，不参与“缺失快照隐藏”，只要该 name+size 组仍存在真实快照记录。
		// 目的：避免刷新下载器数据时把 IYUU 补全的站点条目误隐藏（伪删除）。
		if strings.TrimSpace(row.State) == "未做种" {
			name := strings.TrimSpace(row.Name)
			if name != "" && row.Size > 0 {
				groupKey := fmt.Sprintf("%s\x00%d", name, row.Size)
				if _, ok := snapshotGroupSet[groupKey]; ok {
					continue
				}
			}
		}
		deletedHashes = append(deletedHashes, hash)
	}
	sort.Strings(deletedHashes)

	tx := r.store.DB.Begin()
	if tx.Error != nil {
		return stats, tx.Error
	}

	if len(recordMap) > 0 {
		hashes := make([]string, 0, len(recordMap))
		for hash := range recordMap {
			hashes = append(hashes, hash)
		}
		sort.Strings(hashes)

		torrentRows := make([]torrentUpsertRow, 0, len(recordMap))
		uploadRows := make([]torrentUploadUpsertRow, 0, len(recordMap))
		for _, hash := range hashes {
			record := recordMap[hash]
			progress := record.Progress
			if progress < 0 {
				progress = 0
			}
			if progress > 100 {
				progress = 100
			}
			// 写库前统一截断到 1 位小数，避免 UI 展示长小数。
			progress = math.Round(progress*10) / 10

			torrentRows = append(torrentRows, torrentUpsertRow{
				Hash:         record.Hash,
				Name:         strings.TrimSpace(record.Name),
				SavePath:     strings.TrimSpace(record.SavePath),
				Size:         record.Size,
				Progress:     progress,
				State:        strings.TrimSpace(record.State),
				Sites:        strings.TrimSpace(record.Sites),
				Details:      strings.TrimSpace(record.Details),
				TorrentGroup: strings.TrimSpace(record.TorrentGroup),
				OfficialSite: strings.TrimSpace(record.OfficialSite),
				DownloaderID: trimmedID,
				LastSeen:     lastSeen,
				Seeders:      record.Seeders,
				IsHidden:     0,
				HiddenReason: "",
				HiddenAt:     nil,
			})
			uploadRows = append(uploadRows, torrentUploadUpsertRow{
				Hash:         record.Hash,
				DownloaderID: trimmedID,
				Uploaded:     record.Uploaded,
				IsHidden:     0,
				HiddenReason: "",
				HiddenAt:     nil,
			})
		}

		torrentBatchSize := 500
		uploadBatchSize := 1000
		if r.store.DBType == "sqlite" {
			// SQLite 默认变量上限较小，需要严格分批避免 "too many SQL variables"。
			torrentBatchSize = 80
			uploadBatchSize = 300
		}

		for offset := 0; offset < len(torrentRows); offset += torrentBatchSize {
			end := offset + torrentBatchSize
			if end > len(torrentRows) {
				end = len(torrentRows)
			}
			chunk := torrentRows[offset:end]
			if err := tx.Table("torrents").Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "hash"}, {Name: "downloader_id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"name",
					"save_path",
					"size",
					"progress",
					"state",
					"sites",
					"details",
					"group",
					"official_site",
					"last_seen",
					"seeders",
					"is_hidden",
					"hidden_reason",
					"hidden_at",
				}),
			}).Create(&chunk).Error; err != nil {
				tx.Rollback()
				return stats, err
			}
		}

		for offset := 0; offset < len(uploadRows); offset += uploadBatchSize {
			end := offset + uploadBatchSize
			if end > len(uploadRows) {
				end = len(uploadRows)
			}
			chunk := uploadRows[offset:end]
			if err := tx.Table("torrent_upload_stats").Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "hash"}, {Name: "downloader_id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"uploaded",
					"is_hidden",
					"hidden_reason",
					"hidden_at",
				}),
			}).Create(&chunk).Error; err != nil {
				tx.Rollback()
				return stats, err
			}
		}
		stats.UpsertedUpload = int64(len(uploadRows))
	}

	if len(deletedHashes) > 0 {
		hideBatchSize := 1000
		if r.store.DBType == "sqlite" {
			hideBatchSize = 500
		}

		for offset := 0; offset < len(deletedHashes); offset += hideBatchSize {
			end := offset + hideBatchSize
			if end > len(deletedHashes) {
				end = len(deletedHashes)
			}
			chunk := deletedHashes[offset:end]

			hideResult := tx.Table("torrents").
				Where("downloader_id = ? AND hash IN ? AND (is_hidden = 0 OR is_hidden IS NULL)", trimmedID, chunk).
				Updates(map[string]any{
					"is_hidden":     1,
					"hidden_reason": torrentHideReasonMissingSnapshot,
					"hidden_at":     lastSeen,
				})
			if hideResult.Error != nil {
				tx.Rollback()
				return stats, hideResult.Error
			}
			stats.Deleted += hideResult.RowsAffected

			hideUpload := tx.Table("torrent_upload_stats").
				Where("downloader_id = ? AND hash IN ?", trimmedID, chunk).
				Updates(map[string]any{
					"is_hidden":     1,
					"hidden_reason": torrentHideReasonMissingSnapshot,
					"hidden_at":     lastSeen,
				})
			if hideUpload.Error != nil {
				tx.Rollback()
				return stats, hideUpload.Error
			}
		}
	}

	if err := tx.Commit().Error; err != nil {
		return stats, err
	}
	return stats, nil
}

// HideDisabledDownloaderData 将停用下载器的种子数据统一标记为隐藏。
// 参数/返回：enabledDownloaderIDs 为当前启用下载器，configuredDownloaderIDs 为配置内全部下载器。
// 失败场景：查询或更新数据库失败时返回错误。
// 副作用：更新 torrents/torrent_upload_stats 的隐藏字段，不做物理删除。
func (r *TorrentDataRepository) HideDisabledDownloaderData(enabledDownloaderIDs []string, configuredDownloaderIDs []string) (int64, error) {
	enabledSet := map[string]struct{}{}
	for _, id := range enabledDownloaderIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		enabledSet[trimmed] = struct{}{}
	}

	disabledIDs := make([]string, 0)
	seen := map[string]struct{}{}
	for _, id := range configuredDownloaderIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		if _, enabled := enabledSet[trimmed]; enabled {
			continue
		}
		disabledIDs = append(disabledIDs, trimmed)
	}

	return r.hideDownloaderData(disabledIDs, torrentHideReasonDisabledDownloader)
}

// HideRemovedDownloaderData 将已从配置移除的下载器数据标记为隐藏。
// 参数/返回：configuredDownloaderIDs 为当前配置内仍存在的下载器 ID 列表，返回本次隐藏的 torrents 行数。
// 失败场景：读取现存 downloader_id 或更新 SQL 失败时返回错误。
// 副作用：更新 torrents 与 torrent_upload_stats 的隐藏字段，不做物理删除。
func (r *TorrentDataRepository) HideRemovedDownloaderData(configuredDownloaderIDs []string) (int64, error) {
	configuredSet := map[string]struct{}{}
	for _, id := range configuredDownloaderIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		configuredSet[trimmed] = struct{}{}
	}

	existingDownloaderIDs := make([]string, 0)
	if err := r.store.DB.Table("torrents").
		Distinct("downloader_id").
		Where("downloader_id IS NOT NULL AND downloader_id != ''").
		Pluck("downloader_id", &existingDownloaderIDs).Error; err != nil {
		return 0, err
	}

	removedIDs := make([]string, 0)
	for _, id := range existingDownloaderIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, exists := configuredSet[trimmed]; exists {
			continue
		}
		removedIDs = append(removedIDs, trimmed)
	}

	return r.hideDownloaderData(removedIDs, torrentHideReasonRemovedDownloader)
}

// CleanupDeletedDownloaderData 兼容旧调用路径，当前行为为“隐藏已删除下载器数据”。
// 参数/返回：configuredDownloaderIDs 传入配置中仍存在的下载器 ID 列表。
// 失败场景：查询或更新失败时返回错误。
// 副作用：调用 HideRemovedDownloaderData 更新隐藏标记，不执行物理删除。
func (r *TorrentDataRepository) CleanupDeletedDownloaderData(configuredDownloaderIDs []string) (int64, error) {
	return r.HideRemovedDownloaderData(configuredDownloaderIDs)
}

func (r *TorrentDataRepository) hideDownloaderData(downloaderIDs []string, reason string) (int64, error) {
	ids := make([]string, 0, len(downloaderIDs))
	seen := map[string]struct{}{}
	for _, id := range downloaderIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		ids = append(ids, trimmed)
	}
	if len(ids) == 0 {
		return 0, nil
	}

	hiddenAt := time.Now().Format("2006-01-02 15:04:05")
	updateData := map[string]any{
		"is_hidden":     1,
		"hidden_reason": strings.TrimSpace(reason),
		"hidden_at":     hiddenAt,
	}

	tx := r.store.DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}

	hideResult := tx.Table("torrents").
		Where("downloader_id IN ? AND (is_hidden = 0 OR is_hidden IS NULL)", ids).
		Updates(updateData)
	if hideResult.Error != nil {
		tx.Rollback()
		return 0, hideResult.Error
	}

	hideUpload := tx.Table("torrent_upload_stats").
		Where("downloader_id IN ? AND (is_hidden = 0 OR is_hidden IS NULL)", ids).
		Updates(updateData)
	if hideUpload.Error != nil {
		tx.Rollback()
		return 0, hideUpload.Error
	}

	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return hideResult.RowsAffected, nil
}
