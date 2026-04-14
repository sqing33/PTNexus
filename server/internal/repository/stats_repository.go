package repository

import (
	"fmt"
	"strings"
	"time"
)

type TrafficDeltaRow struct {
	TimeGroup    string  `gorm:"column:time_group"`
	DownloaderID string  `gorm:"column:downloader_id"`
	TotalUL      float64 `gorm:"column:total_ul"`
	TotalDL      float64 `gorm:"column:total_dl"`
}

type SpeedRow struct {
	TimeGroup    string  `gorm:"column:time_group"`
	DownloaderID string  `gorm:"column:downloader_id"`
	ULSpeed      float64 `gorm:"column:ul_speed"`
	DLSpeed      float64 `gorm:"column:dl_speed"`
}

type LatestSpeedRow struct {
	DownloaderID  string  `gorm:"column:downloader_id"`
	UploadSpeed   float64 `gorm:"column:upload_speed"`
	DownloadSpeed float64 `gorm:"column:download_speed"`
}

// TrafficStatRecord 表示一条待写入 traffic_stats 的原始采样记录。
type TrafficStatRecord struct {
	StatDatetime         time.Time
	DownloaderID         string
	Uploaded             int64
	Downloaded           int64
	UploadSpeed          int64
	DownloadSpeed        int64
	CumulativeUploaded   int64
	CumulativeDownloaded int64
}

// CumulativeSnapshot 表示某下载器最近一次累计流量快照。
type CumulativeSnapshot struct {
	CumulativeUploaded   int64
	CumulativeDownloaded int64
}

type SiteStatRow struct {
	SiteName     string `gorm:"column:site_name"`
	TotalSize    int64  `gorm:"column:total_size"`
	TorrentCount int64  `gorm:"column:torrent_count"`
}

type GroupStatRow struct {
	SiteName     string `gorm:"column:site_name"`
	GroupSuffix  string `gorm:"column:group_suffix"`
	TorrentCount int64  `gorm:"column:torrent_count"`
	TotalSize    int64  `gorm:"column:total_size"`
}

type StatsRepository struct {
	store *Store
}

func NewStatsRepository(store *Store) *StatsRepository {
	return &StatsRepository{store: store}
}

// EnsureTrafficStatsTables 确保流量原始表与小时聚合表存在。
// 参数/返回：无参数，成功返回 nil。
// 失败场景：数据库不可写或 SQL 执行失败时返回错误。
// 副作用：会在数据库中创建表结构（若不存在）。
func (r *StatsRepository) EnsureTrafficStatsTables() error {
	var trafficStatsSQL string
	var trafficStatsHourlySQL string

	switch r.store.DBType {
	case "mysql":
		trafficStatsSQL = `
			CREATE TABLE IF NOT EXISTS traffic_stats (
				stat_datetime DATETIME NOT NULL,
				downloader_id VARCHAR(64) NOT NULL,
				uploaded BIGINT DEFAULT 0,
				downloaded BIGINT DEFAULT 0,
				upload_speed BIGINT DEFAULT 0,
				download_speed BIGINT DEFAULT 0,
				cumulative_uploaded BIGINT NOT NULL DEFAULT 0,
				cumulative_downloaded BIGINT NOT NULL DEFAULT 0,
				PRIMARY KEY (stat_datetime, downloader_id)
			) ENGINE=InnoDB ROW_FORMAT=Dynamic
		`
		trafficStatsHourlySQL = `
			CREATE TABLE IF NOT EXISTS traffic_stats_hourly (
				stat_datetime DATETIME NOT NULL,
				downloader_id VARCHAR(64) NOT NULL,
				uploaded BIGINT DEFAULT 0,
				downloaded BIGINT DEFAULT 0,
				avg_upload_speed BIGINT DEFAULT 0,
				avg_download_speed BIGINT DEFAULT 0,
				samples INTEGER DEFAULT 0,
				cumulative_uploaded BIGINT NOT NULL DEFAULT 0,
				cumulative_downloaded BIGINT NOT NULL DEFAULT 0,
				PRIMARY KEY (stat_datetime, downloader_id)
			) ENGINE=InnoDB ROW_FORMAT=Dynamic
		`
	case "postgresql":
		trafficStatsSQL = `
			CREATE TABLE IF NOT EXISTS traffic_stats (
				stat_datetime TIMESTAMP NOT NULL,
				downloader_id VARCHAR(64) NOT NULL,
				uploaded BIGINT DEFAULT 0,
				downloaded BIGINT DEFAULT 0,
				upload_speed BIGINT DEFAULT 0,
				download_speed BIGINT DEFAULT 0,
				cumulative_uploaded BIGINT NOT NULL DEFAULT 0,
				cumulative_downloaded BIGINT NOT NULL DEFAULT 0,
				PRIMARY KEY (stat_datetime, downloader_id)
			)
		`
		trafficStatsHourlySQL = `
			CREATE TABLE IF NOT EXISTS traffic_stats_hourly (
				stat_datetime TIMESTAMP NOT NULL,
				downloader_id VARCHAR(64) NOT NULL,
				uploaded BIGINT DEFAULT 0,
				downloaded BIGINT DEFAULT 0,
				avg_upload_speed BIGINT DEFAULT 0,
				avg_download_speed BIGINT DEFAULT 0,
				samples INTEGER DEFAULT 0,
				cumulative_uploaded BIGINT NOT NULL DEFAULT 0,
				cumulative_downloaded BIGINT NOT NULL DEFAULT 0,
				PRIMARY KEY (stat_datetime, downloader_id)
			)
		`
	default:
		trafficStatsSQL = `
			CREATE TABLE IF NOT EXISTS traffic_stats (
				stat_datetime TEXT NOT NULL,
				downloader_id TEXT NOT NULL,
				uploaded INTEGER DEFAULT 0,
				downloaded INTEGER DEFAULT 0,
				upload_speed INTEGER DEFAULT 0,
				download_speed INTEGER DEFAULT 0,
				cumulative_uploaded INTEGER NOT NULL DEFAULT 0,
				cumulative_downloaded INTEGER NOT NULL DEFAULT 0,
				PRIMARY KEY (stat_datetime, downloader_id)
			)
		`
		trafficStatsHourlySQL = `
			CREATE TABLE IF NOT EXISTS traffic_stats_hourly (
				stat_datetime TEXT NOT NULL,
				downloader_id TEXT NOT NULL,
				uploaded INTEGER DEFAULT 0,
				downloaded INTEGER DEFAULT 0,
				avg_upload_speed INTEGER DEFAULT 0,
				avg_download_speed INTEGER DEFAULT 0,
				samples INTEGER DEFAULT 0,
				cumulative_uploaded INTEGER NOT NULL DEFAULT 0,
				cumulative_downloaded INTEGER NOT NULL DEFAULT 0,
				PRIMARY KEY (stat_datetime, downloader_id)
			)
		`
	}

	if err := r.store.DB.Exec(trafficStatsSQL).Error; err != nil {
		return err
	}
	if err := r.store.DB.Exec(trafficStatsHourlySQL).Error; err != nil {
		return err
	}
	return nil
}

// QueryLatestCumulativeByDownloader 查询每个下载器最近一条累计流量记录。
// 参数/返回：返回 downloader_id -> 累计值 的映射。
// 失败场景：查询失败时返回错误；表不存在时返回空映射且不报错。
// 副作用：无副作用，仅读取数据库。
func (r *StatsRepository) QueryLatestCumulativeByDownloader() (map[string]CumulativeSnapshot, error) {
	rows := make([]struct {
		DownloaderID         string `gorm:"column:downloader_id"`
		CumulativeUploaded   int64  `gorm:"column:cumulative_uploaded"`
		CumulativeDownloaded int64  `gorm:"column:cumulative_downloaded"`
	}, 0)

	query := `
		SELECT t.downloader_id, t.cumulative_uploaded, t.cumulative_downloaded
		FROM traffic_stats t
		JOIN (
			SELECT downloader_id, MAX(stat_datetime) AS max_dt
			FROM traffic_stats
			WHERE cumulative_uploaded > 0 OR cumulative_downloaded > 0
			GROUP BY downloader_id
		) last ON t.downloader_id = last.downloader_id AND t.stat_datetime = last.max_dt
	`
	if err := r.store.DB.Raw(query).Scan(&rows).Error; err != nil {
		if isMissingTableError(err) {
			return map[string]CumulativeSnapshot{}, nil
		}
		return nil, err
	}

	result := map[string]CumulativeSnapshot{}
	for _, row := range rows {
		if strings.TrimSpace(row.DownloaderID) == "" {
			continue
		}
		result[row.DownloaderID] = CumulativeSnapshot{
			CumulativeUploaded:   row.CumulativeUploaded,
			CumulativeDownloaded: row.CumulativeDownloaded,
		}
	}
	return result, nil
}

// UpsertTrafficStats 批量写入原始流量采样数据。
// 参数/返回：records 为采样批次，返回成功写入条数。
// 失败场景：事务失败或 SQL 执行异常时回滚并返回错误。
// 副作用：写入/更新 traffic_stats 表数据。
func (r *StatsRepository) UpsertTrafficStats(records []TrafficStatRecord) (int, error) {
	if len(records) == 0 {
		return 0, nil
	}

	insertSQL := ""
	switch r.store.DBType {
	case "mysql":
		insertSQL = `
			INSERT INTO traffic_stats (
				stat_datetime, downloader_id, uploaded, downloaded,
				upload_speed, download_speed, cumulative_uploaded, cumulative_downloaded
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				uploaded = VALUES(uploaded),
				downloaded = VALUES(downloaded),
				upload_speed = VALUES(upload_speed),
				download_speed = VALUES(download_speed),
				cumulative_uploaded = VALUES(cumulative_uploaded),
				cumulative_downloaded = VALUES(cumulative_downloaded)
		`
	case "postgresql":
		insertSQL = `
			INSERT INTO traffic_stats (
				stat_datetime, downloader_id, uploaded, downloaded,
				upload_speed, download_speed, cumulative_uploaded, cumulative_downloaded
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(stat_datetime, downloader_id) DO UPDATE SET
				uploaded = EXCLUDED.uploaded,
				downloaded = EXCLUDED.downloaded,
				upload_speed = EXCLUDED.upload_speed,
				download_speed = EXCLUDED.download_speed,
				cumulative_uploaded = EXCLUDED.cumulative_uploaded,
				cumulative_downloaded = EXCLUDED.cumulative_downloaded
		`
	default:
		insertSQL = `
			INSERT INTO traffic_stats (
				stat_datetime, downloader_id, uploaded, downloaded,
				upload_speed, download_speed, cumulative_uploaded, cumulative_downloaded
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(stat_datetime, downloader_id) DO UPDATE SET
				uploaded = excluded.uploaded,
				downloaded = excluded.downloaded,
				upload_speed = excluded.upload_speed,
				download_speed = excluded.download_speed,
				cumulative_uploaded = excluded.cumulative_uploaded,
				cumulative_downloaded = excluded.cumulative_downloaded
		`
	}

	tx := r.store.DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}

	for _, record := range records {
		timestamp := record.StatDatetime.Format("2006-01-02 15:04:05")
		if err := tx.Exec(
			insertSQL,
			timestamp,
			record.DownloaderID,
			record.Uploaded,
			record.Downloaded,
			record.UploadSpeed,
			record.DownloadSpeed,
			record.CumulativeUploaded,
			record.CumulativeDownloaded,
		).Error; err != nil {
			tx.Rollback()
			return 0, err
		}
	}

	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return len(records), nil
}

// AggregateHourlyTraffic 将历史原始流量按小时聚合到 traffic_stats_hourly，并清理旧原始记录。
// 参数/返回：retentionHours 为原始数据保留时长，返回聚合条数与清理条数。
// 失败场景：聚合查询、upsert 或删除失败时回滚并返回错误。
// 副作用：写入 traffic_stats_hourly 并删除 traffic_stats 中的旧数据。
func (r *StatsRepository) AggregateHourlyTraffic(retentionHours int) (int, int64, error) {
	if retentionHours <= 0 {
		retentionHours = 48
	}

	now := time.Now()
	cutoff := now.Add(-time.Duration(retentionHours) * time.Hour)
	safeCutoff := now.AddDate(0, 0, -3)
	safeCutoff = time.Date(safeCutoff.Year(), safeCutoff.Month(), safeCutoff.Day(), 0, 0, 0, 0, safeCutoff.Location())
	if cutoff.After(safeCutoff) {
		cutoff = safeCutoff
	}
	cutoffText := cutoff.Format("2006-01-02 15:04:05")
	groupExpr := r.hourlyGroupExpr()

	aggregatedRows := make([]struct {
		HourGroup        string  `gorm:"column:hour_group"`
		DownloaderID     string  `gorm:"column:downloader_id"`
		TotalUploaded    float64 `gorm:"column:total_uploaded"`
		TotalDownloaded  float64 `gorm:"column:total_downloaded"`
		AvgUploadSpeed   float64 `gorm:"column:avg_upload_speed"`
		AvgDownloadSpeed float64 `gorm:"column:avg_download_speed"`
		Samples          int64   `gorm:"column:samples"`
	}, 0)

	aggregateSQL := ""
	switch r.store.DBType {
	case "postgresql":
		aggregateSQL = fmt.Sprintf(`
			SELECT
				%s AS hour_group,
				downloader_id,
				GREATEST(0, (MAX(cumulative_uploaded) - MIN(cumulative_uploaded))::bigint) AS total_uploaded,
				GREATEST(0, (MAX(cumulative_downloaded) - MIN(cumulative_downloaded))::bigint) AS total_downloaded,
				AVG(upload_speed) AS avg_upload_speed,
				AVG(download_speed) AS avg_download_speed,
				COUNT(*) AS samples
			FROM traffic_stats
			WHERE stat_datetime < ?
			GROUP BY hour_group, downloader_id
		`, groupExpr)
	case "mysql":
		aggregateSQL = fmt.Sprintf(`
			SELECT
				%s AS hour_group,
				downloader_id,
				GREATEST(0, MAX(cumulative_uploaded) - MIN(cumulative_uploaded)) AS total_uploaded,
				GREATEST(0, MAX(cumulative_downloaded) - MIN(cumulative_downloaded)) AS total_downloaded,
				AVG(upload_speed) AS avg_upload_speed,
				AVG(download_speed) AS avg_download_speed,
				COUNT(*) AS samples
			FROM traffic_stats
			WHERE stat_datetime < ?
			GROUP BY hour_group, downloader_id
		`, groupExpr)
	default:
		aggregateSQL = fmt.Sprintf(`
			SELECT
				%s AS hour_group,
				downloader_id,
				CASE WHEN MAX(cumulative_uploaded) - MIN(cumulative_uploaded) > 0
					THEN MAX(cumulative_uploaded) - MIN(cumulative_uploaded)
					ELSE 0 END AS total_uploaded,
				CASE WHEN MAX(cumulative_downloaded) - MIN(cumulative_downloaded) > 0
					THEN MAX(cumulative_downloaded) - MIN(cumulative_downloaded)
					ELSE 0 END AS total_downloaded,
				AVG(upload_speed) AS avg_upload_speed,
				AVG(download_speed) AS avg_download_speed,
				COUNT(*) AS samples
			FROM traffic_stats
			WHERE stat_datetime < ?
			GROUP BY hour_group, downloader_id
		`, groupExpr)
	}
	if err := r.store.DB.Raw(aggregateSQL, cutoffText).Scan(&aggregatedRows).Error; err != nil {
		if isMissingTableError(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	if len(aggregatedRows) == 0 {
		return 0, 0, nil
	}

	cumulativeRows := make([]struct {
		HourGroup                 string `gorm:"column:hour_group"`
		DownloaderID              string `gorm:"column:downloader_id"`
		FinalCumulativeUploaded   int64  `gorm:"column:final_cumulative_uploaded"`
		FinalCumulativeDownloaded int64  `gorm:"column:final_cumulative_downloaded"`
	}, 0)
	cumulativeSQL := fmt.Sprintf(`
		SELECT
			%s AS hour_group,
			downloader_id,
			MAX(cumulative_uploaded) AS final_cumulative_uploaded,
			MAX(cumulative_downloaded) AS final_cumulative_downloaded
		FROM traffic_stats
		WHERE stat_datetime < ?
		GROUP BY hour_group, downloader_id
	`, groupExpr)
	if err := r.store.DB.Raw(cumulativeSQL, cutoffText).Scan(&cumulativeRows).Error; err != nil {
		if isMissingTableError(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}

	type cumulativeKey struct {
		HourGroup    string
		DownloaderID string
	}
	cumulativeMap := map[cumulativeKey]CumulativeSnapshot{}
	for _, row := range cumulativeRows {
		key := cumulativeKey{HourGroup: row.HourGroup, DownloaderID: row.DownloaderID}
		cumulativeMap[key] = CumulativeSnapshot{
			CumulativeUploaded:   row.FinalCumulativeUploaded,
			CumulativeDownloaded: row.FinalCumulativeDownloaded,
		}
	}

	upsertSQL := ""
	switch r.store.DBType {
	case "mysql":
		upsertSQL = `
			INSERT INTO traffic_stats_hourly (
				stat_datetime, downloader_id, uploaded, downloaded,
				avg_upload_speed, avg_download_speed, samples,
				cumulative_uploaded, cumulative_downloaded
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				uploaded = uploaded + VALUES(uploaded),
				downloaded = downloaded + VALUES(downloaded),
				avg_upload_speed = ((avg_upload_speed * samples) + (VALUES(avg_upload_speed) * VALUES(samples))) / (samples + VALUES(samples)),
				avg_download_speed = ((avg_download_speed * samples) + (VALUES(avg_download_speed) * VALUES(samples))) / (samples + VALUES(samples)),
				samples = samples + VALUES(samples),
				cumulative_uploaded = GREATEST(cumulative_uploaded, VALUES(cumulative_uploaded)),
				cumulative_downloaded = GREATEST(cumulative_downloaded, VALUES(cumulative_downloaded))
		`
	case "postgresql":
		upsertSQL = `
			INSERT INTO traffic_stats_hourly (
				stat_datetime, downloader_id, uploaded, downloaded,
				avg_upload_speed, avg_download_speed, samples,
				cumulative_uploaded, cumulative_downloaded
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(stat_datetime, downloader_id) DO UPDATE SET
				uploaded = traffic_stats_hourly.uploaded + EXCLUDED.uploaded,
				downloaded = traffic_stats_hourly.downloaded + EXCLUDED.downloaded,
				avg_upload_speed = ((traffic_stats_hourly.avg_upload_speed * traffic_stats_hourly.samples) + (EXCLUDED.avg_upload_speed * EXCLUDED.samples)) / (traffic_stats_hourly.samples + EXCLUDED.samples),
				avg_download_speed = ((traffic_stats_hourly.avg_download_speed * traffic_stats_hourly.samples) + (EXCLUDED.avg_download_speed * EXCLUDED.samples)) / (traffic_stats_hourly.samples + EXCLUDED.samples),
				samples = traffic_stats_hourly.samples + EXCLUDED.samples,
				cumulative_uploaded = GREATEST(traffic_stats_hourly.cumulative_uploaded, EXCLUDED.cumulative_uploaded),
				cumulative_downloaded = GREATEST(traffic_stats_hourly.cumulative_downloaded, EXCLUDED.cumulative_downloaded)
		`
	default:
		upsertSQL = `
			INSERT INTO traffic_stats_hourly (
				stat_datetime, downloader_id, uploaded, downloaded,
				avg_upload_speed, avg_download_speed, samples,
				cumulative_uploaded, cumulative_downloaded
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(stat_datetime, downloader_id) DO UPDATE SET
				uploaded = traffic_stats_hourly.uploaded + excluded.uploaded,
				downloaded = traffic_stats_hourly.downloaded + excluded.downloaded,
				avg_upload_speed = ((traffic_stats_hourly.avg_upload_speed * traffic_stats_hourly.samples) + (excluded.avg_upload_speed * excluded.samples)) / (traffic_stats_hourly.samples + excluded.samples),
				avg_download_speed = ((traffic_stats_hourly.avg_download_speed * traffic_stats_hourly.samples) + (excluded.avg_download_speed * excluded.samples)) / (traffic_stats_hourly.samples + excluded.samples),
				samples = traffic_stats_hourly.samples + excluded.samples,
				cumulative_uploaded = MAX(traffic_stats_hourly.cumulative_uploaded, excluded.cumulative_uploaded),
				cumulative_downloaded = MAX(traffic_stats_hourly.cumulative_downloaded, excluded.cumulative_downloaded)
		`
	}

	tx := r.store.DB.Begin()
	if tx.Error != nil {
		return 0, 0, tx.Error
	}

	for _, row := range aggregatedRows {
		key := cumulativeKey{HourGroup: row.HourGroup, DownloaderID: row.DownloaderID}
		cumulative := cumulativeMap[key]
		if err := tx.Exec(
			upsertSQL,
			row.HourGroup,
			row.DownloaderID,
			int64(row.TotalUploaded),
			int64(row.TotalDownloaded),
			int64(row.AvgUploadSpeed),
			int64(row.AvgDownloadSpeed),
			row.Samples,
			cumulative.CumulativeUploaded,
			cumulative.CumulativeDownloaded,
		).Error; err != nil {
			tx.Rollback()
			return 0, 0, err
		}
	}

	deleteResult := tx.Exec("DELETE FROM traffic_stats WHERE stat_datetime < ?", cutoffText)
	if deleteResult.Error != nil {
		tx.Rollback()
		return 0, 0, deleteResult.Error
	}

	if err := tx.Commit().Error; err != nil {
		return 0, 0, err
	}
	return len(aggregatedRows), deleteResult.RowsAffected, nil
}

func (r *StatsRepository) QueryTrafficDeltas(start, end time.Time, format string) ([]TrafficDeltaRow, error) {
	return r.queryTrafficDeltasTable("traffic_stats", start, end, format)
}

func (r *StatsRepository) QueryTrafficDeltasHourly(start, end time.Time, format string) ([]TrafficDeltaRow, error) {
	return r.queryTrafficDeltasTable("traffic_stats_hourly", start, end, format)
}

func (r *StatsRepository) queryTrafficDeltasTable(table string, start, end time.Time, format string) ([]TrafficDeltaRow, error) {
	groupExpr := r.timeGroupExpr(format)
	var query string
	switch r.store.DBType {
	case "postgresql":
		query = fmt.Sprintf(`
			SELECT %s AS time_group,
			       downloader_id,
			       GREATEST(0, (MAX(cumulative_uploaded) - MIN(cumulative_uploaded))::bigint) AS total_ul,
			       GREATEST(0, (MAX(cumulative_downloaded) - MIN(cumulative_downloaded))::bigint) AS total_dl
			FROM %s
			WHERE stat_datetime >= ? AND stat_datetime < ?
			GROUP BY time_group, downloader_id
			ORDER BY time_group
		`, groupExpr, table)
	case "mysql":
		query = fmt.Sprintf(`
			SELECT %s AS time_group,
			       downloader_id,
			       GREATEST(0, MAX(cumulative_uploaded) - MIN(cumulative_uploaded)) AS total_ul,
			       GREATEST(0, MAX(cumulative_downloaded) - MIN(cumulative_downloaded)) AS total_dl
			FROM %s
			WHERE stat_datetime >= ? AND stat_datetime < ?
			GROUP BY time_group, downloader_id
			ORDER BY time_group
		`, groupExpr, table)
	default:
		query = fmt.Sprintf(`
			SELECT %s AS time_group,
			       downloader_id,
			       CASE WHEN MAX(cumulative_uploaded) - MIN(cumulative_uploaded) > 0
			            THEN MAX(cumulative_uploaded) - MIN(cumulative_uploaded)
			            ELSE 0 END AS total_ul,
			       CASE WHEN MAX(cumulative_downloaded) - MIN(cumulative_downloaded) > 0
			            THEN MAX(cumulative_downloaded) - MIN(cumulative_downloaded)
			            ELSE 0 END AS total_dl
			FROM %s
			WHERE stat_datetime >= ? AND stat_datetime < ?
			GROUP BY time_group, downloader_id
			ORDER BY time_group
		`, groupExpr, table)
	}

	rows := make([]TrafficDeltaRow, 0)
	if err := r.store.DB.Raw(query, start, end).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *StatsRepository) QuerySpeedAverages(start, end time.Time, format string) ([]SpeedRow, error) {
	return r.querySpeedAveragesTable("traffic_stats", "upload_speed", "download_speed", start, end, format)
}

func (r *StatsRepository) QuerySpeedAveragesHourly(start, end time.Time, format string) ([]SpeedRow, error) {
	return r.querySpeedAveragesTable("traffic_stats_hourly", "avg_upload_speed", "avg_download_speed", start, end, format)
}

func (r *StatsRepository) querySpeedAveragesTable(table, ulCol, dlCol string, start, end time.Time, format string) ([]SpeedRow, error) {
	groupExpr := r.timeGroupExpr(format)
	query := fmt.Sprintf(`
		SELECT %s AS time_group,
		       downloader_id,
		       AVG(%s) AS ul_speed,
		       AVG(%s) AS dl_speed
		FROM %s
		WHERE stat_datetime >= ? AND stat_datetime < ?
		GROUP BY time_group, downloader_id
		ORDER BY time_group
	`, groupExpr, ulCol, dlCol, table)

	rows := make([]SpeedRow, 0)
	if err := r.store.DB.Raw(query, start, end).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *StatsRepository) QueryLatestSpeeds() ([]LatestSpeedRow, error) {
	query := `
		SELECT t1.downloader_id,
		       t1.upload_speed,
		       t1.download_speed
		FROM traffic_stats t1
		JOIN (
			SELECT downloader_id, MAX(stat_datetime) AS max_dt
			FROM traffic_stats
			GROUP BY downloader_id
		) t2 ON t1.downloader_id = t2.downloader_id AND t1.stat_datetime = t2.max_dt
	`
	rows := make([]LatestSpeedRow, 0)
	if err := r.store.DB.Raw(query).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *StatsRepository) QuerySiteStats() ([]SiteStatRow, error) {
	query := `
		SELECT sites AS site_name,
		       SUM(size) AS total_size,
		       COUNT(name) AS torrent_count
		FROM (
			SELECT DISTINCT name, size, sites
			FROM torrents
			WHERE sites IS NOT NULL AND sites != '' AND (is_hidden = 0 OR is_hidden IS NULL)
		) unique_torrents
		GROUP BY sites
		ORDER BY sites
	`
	rows := make([]SiteStatRow, 0)
	if err := r.store.DB.Raw(query).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *StatsRepository) QueryGroupStats(siteName string) ([]GroupStatRow, error) {
	torrentGroupColumn := r.groupColumnRef("t")
	siteGroupColumn := r.groupColumnRef("s")
	normalizedTorrentGroup := r.normalizedGroupExpr(torrentGroupColumn)
	siteGroupMatchExpr := r.siteGroupMatchExpr("ut.group_suffix", siteGroupColumn)
	query := fmt.Sprintf(`
		SELECT s.nickname AS site_name,
		       ut.group_suffix AS group_suffix,
		       COUNT(*) AS torrent_count,
		       SUM(ut.size) AS total_size
		FROM (
			SELECT DISTINCT
			       t.name AS torrent_name,
			       t.size AS size,
			       %s AS group_suffix
			FROM torrents t
			WHERE %s IS NOT NULL
			  AND TRIM(%s) != ''
			  AND TRIM(%s) != ''
			  AND (t.is_hidden = 0 OR t.is_hidden IS NULL)
		) AS ut
		JOIN sites AS s ON %s
	`, normalizedTorrentGroup, torrentGroupColumn, torrentGroupColumn, normalizedTorrentGroup, siteGroupMatchExpr)

	args := make([]any, 0)
	trimmedSiteName := strings.TrimSpace(siteName)
	if trimmedSiteName != "" {
		query += `
		WHERE LOWER(COALESCE(s.nickname, '')) = LOWER(?) OR LOWER(COALESCE(s.site, '')) = LOWER(?)
		`
		args = append(args, trimmedSiteName, trimmedSiteName)
	}

	query += `
		GROUP BY s.nickname, ut.group_suffix
		ORDER BY s.nickname ASC, torrent_count DESC, ut.group_suffix ASC
	`

	rows := make([]GroupStatRow, 0)
	if err := r.store.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *StatsRepository) groupColumnRef(alias string) string {
	switch r.store.DBType {
	case "postgresql":
		if strings.TrimSpace(alias) == "" {
			return `"group"`
		}
		return alias + `."group"`
	default:
		if strings.TrimSpace(alias) == "" {
			return "`group`"
		}
		return alias + ".`group`"
	}
}

func (r *StatsRepository) normalizedGroupExpr(column string) string {
	switch r.store.DBType {
	case "mysql":
		return fmt.Sprintf("TRIM(LEADING '-' FROM TRIM(%s))", column)
	case "postgresql":
		return fmt.Sprintf("LTRIM(BTRIM(%s), '-')", column)
	default:
		return fmt.Sprintf("LTRIM(TRIM(%s), '-')", column)
	}
}

func (r *StatsRepository) siteGroupMatchExpr(groupValueExpr string, siteGroupColumn string) string {
	normalizedSiteGroups := r.normalizedSiteGroupListExpr(siteGroupColumn)
	switch r.store.DBType {
	case "mysql":
		return fmt.Sprintf("FIND_IN_SET(LOWER(%s), %s) > 0", groupValueExpr, normalizedSiteGroups)
	default:
		return fmt.Sprintf("(',' || %s || ',') LIKE '%%,' || LOWER(%s) || ',%%'", normalizedSiteGroups, groupValueExpr)
	}
}

func (r *StatsRepository) normalizedSiteGroupListExpr(column string) string {
	switch r.store.DBType {
	case "mysql":
		return fmt.Sprintf(
			"REPLACE(TRIM(LEADING '-' FROM REPLACE(LOWER(COALESCE(%s, '')), ' ', '')), ',-', ',')",
			column,
		)
	case "postgresql":
		return fmt.Sprintf(
			"REPLACE(LTRIM(REPLACE(LOWER(COALESCE(%s, '')), ' ', ''), '-'), ',-', ',')",
			column,
		)
	default:
		return fmt.Sprintf(
			"REPLACE(LTRIM(REPLACE(LOWER(COALESCE(%s, '')), ' ', ''), '-'), ',-', ',')",
			column,
		)
	}
}

func (r *StatsRepository) timeGroupExpr(format string) string {
	switch r.store.DBType {
	case "mysql":
		mysqlFormat := strings.ReplaceAll(format, "%M", "%i")
		return "DATE_FORMAT(stat_datetime, '" + mysqlFormat + "')"
	case "postgresql":
		pgFormat := format
		pgFormat = strings.ReplaceAll(pgFormat, "%Y", "YYYY")
		pgFormat = strings.ReplaceAll(pgFormat, "%m", "MM")
		pgFormat = strings.ReplaceAll(pgFormat, "%d", "DD")
		pgFormat = strings.ReplaceAll(pgFormat, "%H", "HH24")
		pgFormat = strings.ReplaceAll(pgFormat, "%M", "MI")
		pgFormat = strings.ReplaceAll(pgFormat, "%S", "SS")
		return "TO_CHAR(stat_datetime, '" + pgFormat + "')"
	default:
		return "STRFTIME('" + format + "', stat_datetime)"
	}
}

func (r *StatsRepository) hourlyGroupExpr() string {
	switch r.store.DBType {
	case "mysql":
		return "DATE_FORMAT(stat_datetime, '%Y-%m-%d %H:00:00')"
	case "postgresql":
		return "TO_CHAR(stat_datetime, 'YYYY-MM-DD HH24:00:00')"
	default:
		return "STRFTIME('%Y-%m-%d %H:00:00', stat_datetime)"
	}
}
