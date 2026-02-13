package repository

import (
	"fmt"
	"strings"
)

type TorrentRepository struct {
	store *Store
}

func NewTorrentRepository(store *Store) *TorrentRepository {
	return &TorrentRepository{store: store}
}

func (r *TorrentRepository) DistinctPaths() ([]string, error) {
	sqlDB, err := r.store.DB.DB()
	if err != nil {
		return nil, err
	}
	rows, err := sqlDB.Query("SELECT DISTINCT save_path FROM torrents WHERE save_path IS NOT NULL AND save_path != '' AND (is_hidden = 0 OR is_hidden IS NULL) ORDER BY save_path")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	paths := make([]string, 0)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func (r *TorrentRepository) TrafficTotals() (map[string]map[string]int64, error) {
	sqlDB, err := r.store.DB.DB()
	if err != nil {
		return nil, err
	}
	rows, err := sqlDB.Query(`
		SELECT downloader_id,
		       MAX(cumulative_downloaded) as total_dl,
		       MAX(cumulative_uploaded) as total_ul
		FROM traffic_stats
		WHERE cumulative_downloaded > 0 OR cumulative_uploaded > 0
		GROUP BY downloader_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]map[string]int64{}
	for rows.Next() {
		var downloaderID string
		var totalDL any
		var totalUL any
		if err := rows.Scan(&downloaderID, &totalDL, &totalUL); err != nil {
			return nil, err
		}
		result[downloaderID] = map[string]int64{
			"total_dl": normalizeToInt64(totalDL),
			"total_ul": normalizeToInt64(totalUL),
		}
	}
	return result, nil
}

func (r *TorrentRepository) TrafficToday() (map[string]map[string]int64, error) {
	sqlDB, err := r.store.DB.DB()
	if err != nil {
		return nil, err
	}

	query := ""
	switch r.store.DBType {
	case "postgresql":
		query = `
			SELECT downloader_id,
			       GREATEST(0, (MAX(cumulative_downloaded) - MIN(cumulative_downloaded))::bigint) as today_dl,
			       GREATEST(0, (MAX(cumulative_uploaded) - MIN(cumulative_uploaded))::bigint) as today_ul
			FROM traffic_stats
			WHERE stat_datetime::date = CURRENT_DATE
			GROUP BY downloader_id
		`
	case "mysql":
		query = `
			SELECT downloader_id,
			       GREATEST(0, MAX(cumulative_downloaded) - MIN(cumulative_downloaded)) as today_dl,
			       GREATEST(0, MAX(cumulative_uploaded) - MIN(cumulative_uploaded)) as today_ul
			FROM traffic_stats
			WHERE DATE(stat_datetime) = CURDATE()
			GROUP BY downloader_id
		`
	default:
		query = `
			SELECT downloader_id,
			       CASE WHEN MAX(cumulative_downloaded) - MIN(cumulative_downloaded) > 0
			            THEN MAX(cumulative_downloaded) - MIN(cumulative_downloaded)
			            ELSE 0 END as today_dl,
			       CASE WHEN MAX(cumulative_uploaded) - MIN(cumulative_uploaded) > 0
			            THEN MAX(cumulative_uploaded) - MIN(cumulative_uploaded)
			            ELSE 0 END as today_ul
			FROM traffic_stats
			WHERE DATE(stat_datetime) = DATE('now', 'localtime')
			GROUP BY downloader_id
		`
	}

	rows, err := sqlDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query today traffic failed: %w", err)
	}
	defer rows.Close()

	result := map[string]map[string]int64{}
	for rows.Next() {
		var downloaderID string
		var todayDL any
		var todayUL any
		if err := rows.Scan(&downloaderID, &todayDL, &todayUL); err != nil {
			return nil, err
		}
		result[downloaderID] = map[string]int64{
			"today_dl": normalizeToInt64(todayDL),
			"today_ul": normalizeToInt64(todayUL),
		}
	}
	return result, nil
}

func (r *TorrentRepository) DistinctDownloaderIDs() ([]string, error) {
	ids := make([]string, 0)
	if err := r.store.DB.Table("torrents").Distinct("downloader_id").Where("downloader_id IS NOT NULL AND downloader_id != '' AND (is_hidden = 0 OR is_hidden IS NULL)").Order("downloader_id ASC").Pluck("downloader_id", &ids).Error; err != nil {
		return nil, err
	}
	result := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result, nil
}

func (r *TorrentRepository) UpdateDownloaderID(oldID, newID string) (int64, error) {
	result := r.store.DB.Table("torrents").Where("downloader_id = ?", oldID).Updates(map[string]any{"downloader_id": newID})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func normalizeToInt64(value any) int64 {
	parsed, err := toInt64(value)
	if err == nil {
		return parsed
	}
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	default:
		return 0
	}
}

func (r *TorrentRepository) EnsureDownloaderMigrationTable() error {
	createSQL := ""
	switch r.store.DBType {
	case "postgresql":
		createSQL = `
			CREATE TABLE IF NOT EXISTS downloader_id_migration (
				old_id VARCHAR(64) NOT NULL PRIMARY KEY,
				new_id VARCHAR(64) NOT NULL,
				host VARCHAR(255) NOT NULL,
				name VARCHAR(255) NOT NULL,
				migrated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
			)
		`
	case "mysql":
		createSQL = `
			CREATE TABLE IF NOT EXISTS downloader_id_migration (
				old_id VARCHAR(64) NOT NULL PRIMARY KEY,
				new_id VARCHAR(64) NOT NULL,
				host VARCHAR(255) NOT NULL,
				name VARCHAR(255) NOT NULL,
				migrated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
			)
		`
	default:
		createSQL = `
			CREATE TABLE IF NOT EXISTS downloader_id_migration (
				old_id TEXT PRIMARY KEY,
				new_id TEXT NOT NULL,
				host TEXT NOT NULL,
				name TEXT NOT NULL,
				migrated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
			)
		`
	}
	return r.store.DB.Exec(createSQL).Error
}

func (r *TorrentRepository) UpsertDownloaderMigration(oldID, newID, host, name string) error {
	switch r.store.DBType {
	case "postgresql":
		return r.store.DB.Exec(`
			INSERT INTO downloader_id_migration (old_id, new_id, host, name)
			VALUES (?, ?, ?, ?)
			ON CONFLICT (old_id) DO UPDATE SET
				new_id = EXCLUDED.new_id,
				host = EXCLUDED.host,
				name = EXCLUDED.name,
				migrated_at = CURRENT_TIMESTAMP
		`, oldID, newID, host, name).Error
	case "mysql":
		return r.store.DB.Exec(`
			INSERT INTO downloader_id_migration (old_id, new_id, host, name)
			VALUES (?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				new_id = VALUES(new_id),
				host = VALUES(host),
				name = VALUES(name),
				migrated_at = CURRENT_TIMESTAMP
		`, oldID, newID, host, name).Error
	default:
		return r.store.DB.Exec(`
			INSERT OR REPLACE INTO downloader_id_migration (old_id, new_id, host, name, migrated_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		`, oldID, newID, host, name).Error
	}
}

func (r *TorrentRepository) ListDownloaderMigrationHistory() ([]map[string]any, error) {
	rows := make([]map[string]any, 0)
	err := r.store.DB.Table("downloader_id_migration").
		Select("old_id, new_id, host, name, migrated_at").
		Order("migrated_at DESC").
		Scan(&rows).Error
	if err != nil {
		if isMissingTableError(err) {
			return []map[string]any{}, nil
		}
		return nil, err
	}
	return rows, nil
}

func (r *TorrentRepository) UpdateDownloaderIDAcrossTables(oldID, newID string) (map[string]int64, error) {
	counts := map[string]int64{}
	tables := []string{"traffic_stats", "torrents", "torrent_upload_stats"}
	for _, table := range tables {
		affected, err := r.updateDownloaderIDInTable(table, oldID, newID)
		if err != nil {
			return nil, err
		}
		counts[table] = affected
	}
	return counts, nil
}

func (r *TorrentRepository) updateDownloaderIDInTable(tableName, oldID, newID string) (int64, error) {
	result := r.store.DB.Table(tableName).Where("downloader_id = ?", oldID).Updates(map[string]any{"downloader_id": newID})
	if result.Error != nil {
		if isMissingTableError(result.Error) {
			return 0, nil
		}
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func isMissingTableError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "no such table") || strings.Contains(text, "does not exist") || strings.Contains(text, "undefined table")
}
