package crossseed

import (
	"fmt"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	"github.com/pt-nexus/server/internal/service/reversemapping"
)

var inactiveTorrentStates = []string{"未做种", "已暂停", "已停止", "错误", "等待", "队列"}

const crossSeedLogModule = "一种多站"

func stateRankExpr(alias string) string {
	quoted := make([]string, 0, len(inactiveTorrentStates))
	for _, value := range inactiveTorrentStates {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		quoted = append(quoted, fmt.Sprintf("'%s'", strings.ReplaceAll(trimmed, "'", "''")))
	}
	statesSQL := strings.Join(quoted, ", ")
	return fmt.Sprintf("CASE WHEN %s.state NOT IN (%s) THEN 0 ELSE 1 END", alias, statesSQL)
}

func seedParameterRowIDExpr(dbType string) string {
	if strings.EqualFold(dbType, "mysql") {
		return "CONCAT(sp.hash, ':', sp.torrent_id, ':', sp.site_name) AS id"
	}
	return "(sp.hash || ':' || sp.torrent_id || ':' || sp.site_name) AS id"
}

func lastPublishAtExpr(dbType string) string {
	if strings.EqualFold(dbType, "mysql") || strings.EqualFold(dbType, "postgresql") {
		return "MAX(COALESCE(pl.updated_at, pl.created_at))"
	}
	return "MAX(COALESCE(NULLIF(pl.updated_at, ''), pl.created_at))"
}

func buildReviewStatusCondition(dbType string, reviewStatus string) (string, []any) {
	normalized := strings.ToLower(strings.TrimSpace(reviewStatus))
	if normalized == "" {
		return "", nil
	}

	isDeletedCondition := "ct.hash IS NULL"
	isNotDeletedCondition := "ct.hash IS NOT NULL"
	tagArgs := []any{"%禁转%", "%限转%", "%分集%"}

	switch normalized {
	case "reviewed", "unreviewed":
		if strings.EqualFold(dbType, "postgresql") {
			isReviewedValue := "false"
			if normalized == "reviewed" {
				isReviewedValue = "true"
			}
			condition := fmt.Sprintf(
				`sp.is_reviewed = %s AND %s AND (sp.tags IS NULL OR (sp.tags::text NOT LIKE ? AND sp.tags::text NOT LIKE ? AND sp.tags::text NOT LIKE ?)) AND (sp.title_components IS NULL OR sp.title_components::text !~ ?)`,
				isReviewedValue,
				isNotDeletedCondition,
			)
			return condition, append(tagArgs, `"无法识别"[^}]*"value":\s*".+"`)
		}

		isReviewedValue := "0"
		if normalized == "reviewed" {
			isReviewedValue = "1"
		}

		if strings.EqualFold(dbType, "mysql") {
			condition := fmt.Sprintf(
				`sp.is_reviewed = %s AND %s AND (sp.tags IS NULL OR (sp.tags NOT LIKE ? AND sp.tags NOT LIKE ? AND sp.tags NOT LIKE ?)) AND (sp.title_components IS NULL OR sp.title_components NOT REGEXP ?)`,
				isReviewedValue,
				isNotDeletedCondition,
			)
			return condition, append(tagArgs, `"无法识别"[^}]*"value":\s*".+"`)
		}

		condition := fmt.Sprintf(
			`sp.is_reviewed = %s AND %s AND (sp.tags IS NULL OR (sp.tags NOT LIKE ? AND sp.tags NOT LIKE ? AND sp.tags NOT LIKE ?)) AND (sp.title_components IS NULL OR NOT (sp.title_components LIKE ? AND sp.title_components NOT LIKE ?))`,
			isReviewedValue,
			isNotDeletedCondition,
		)
		return condition, append(tagArgs, `%"无法识别"%`, `%"value": ""%`)
	case "error":
		if strings.EqualFold(dbType, "postgresql") {
			condition := fmt.Sprintf(
				`(%s OR sp.tags::text LIKE ? OR sp.tags::text LIKE ? OR sp.tags::text LIKE ? OR sp.title_components::text ~ ?)`,
				isDeletedCondition,
			)
			return condition, append(tagArgs, `"无法识别"[^}]*"value":\s*".+"`)
		}

		if strings.EqualFold(dbType, "mysql") {
			condition := fmt.Sprintf(
				`(%s OR sp.tags LIKE ? OR sp.tags LIKE ? OR sp.tags LIKE ? OR sp.title_components REGEXP ?)`,
				isDeletedCondition,
			)
			return condition, append(tagArgs, `"无法识别"[^}]*"value":\s*".+"`)
		}

		condition := fmt.Sprintf(
			`(%s OR sp.tags LIKE ? OR sp.tags LIKE ? OR sp.tags LIKE ? OR (sp.title_components LIKE ? AND sp.title_components NOT LIKE ?))`,
			isDeletedCondition,
		)
		return condition, append(tagArgs, `%"无法识别"%`, `%"value": ""%`)
	default:
		return "", nil
	}
}

func buildCurrentTorrentsSubquery(dbType string) string {
	// Match Python build_current_torrents_subquery().
	if strings.EqualFold(dbType, "postgresql") {
		return fmt.Sprintf(`
			SELECT DISTINCT ON (t.hash) t.hash, t.save_path, t.downloader_id, t.state, t.last_seen, t.size, t.seeders
			FROM torrents t
			WHERE (t.is_hidden = 0 OR t.is_hidden IS NULL)
			ORDER BY t.hash, %s, t.last_seen DESC
		`, stateRankExpr("t"))
	}
	return fmt.Sprintf(`
		SELECT t.hash, t.save_path, t.downloader_id, t.state, t.last_seen, t.size, t.seeders
		FROM torrents t
		JOIN (
			SELECT hash,
			       MIN(%s) AS state_rank,
			       MAX(last_seen) AS max_last_seen
			FROM torrents t2
			WHERE (t2.is_hidden = 0 OR t2.is_hidden IS NULL)
			GROUP BY hash
		) ranked ON t.hash = ranked.hash
		WHERE %s = ranked.state_rank
		  AND t.last_seen = ranked.max_last_seen
		  AND (t.is_hidden = 0 OR t.is_hidden IS NULL)
	`, stateRankExpr("t2"), stateRankExpr("t"))
}

func (s *CrossSeedService) UniquePaths() (map[string]any, error) {
	currentTorrentsSubquery := buildCurrentTorrentsSubquery(s.repo.DBType())
	query := fmt.Sprintf(`
		SELECT DISTINCT ct.save_path
		FROM seed_parameters sp
		JOIN (%s) ct ON sp.hash = ct.hash
		WHERE ct.save_path IS NOT NULL AND ct.save_path != ''
		ORDER BY ct.save_path
	`, currentTorrentsSubquery)
	paths, err := s.repo.RawStrings(query, "save_path")
	if err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "unique_paths": paths}, nil
}

func (s *CrossSeedService) QueryData(params CrossSeedQueryParams) (map[string]any, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}

	offset := (params.Page - 1) * params.PageSize
	dbType := s.repo.DBType()
	currentTorrentsSubquery := buildCurrentTorrentsSubquery(dbType)

	fromClause := fmt.Sprintf(`
		FROM seed_parameters sp
		LEFT JOIN (%s) ct ON sp.hash = ct.hash
	`, currentTorrentsSubquery)

	whereConditions := make([]string, 0)
	args := make([]any, 0)

	search := strings.TrimSpace(params.Search)
	if search != "" {
		if strings.EqualFold(dbType, "postgresql") {
			whereConditions = append(whereConditions, "(sp.title ILIKE ? OR sp.torrent_id ILIKE ? OR sp.subtitle ILIKE ?)")
		} else {
			whereConditions = append(whereConditions, "(sp.title LIKE ? OR sp.torrent_id LIKE ? OR sp.subtitle LIKE ?)")
		}
		args = append(args, "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}

	if len(params.PathFilters) > 0 {
		placeholders := make([]string, 0, len(params.PathFilters))
		for _, value := range params.PathFilters {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, trimmed)
		}
		if len(placeholders) > 0 {
			whereConditions = append(whereConditions, "ct.save_path IN ("+strings.Join(placeholders, ", ")+")")
		}
	}

	if len(params.DownloaderFilters) > 0 {
		placeholders := make([]string, 0, len(params.DownloaderFilters))
		for _, value := range params.DownloaderFilters {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, trimmed)
		}
		if len(placeholders) > 0 {
			whereConditions = append(whereConditions, "ct.downloader_id IN ("+strings.Join(placeholders, ", ")+")")
		}
	}

	switch strings.TrimSpace(params.IsDeleted) {
	case "1":
		whereConditions = append(whereConditions, "ct.hash IS NULL")
	case "0":
		whereConditions = append(whereConditions, "ct.hash IS NOT NULL")
	}

	if reviewCondition, reviewArgs := buildReviewStatusCondition(dbType, params.ReviewStatus); reviewCondition != "" {
		whereConditions = append(whereConditions, reviewCondition)
		args = append(args, reviewArgs...)
	}

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	type countRow struct {
		Total int64 `gorm:"column:total"`
	}
	var count countRow
	countQuery := fmt.Sprintf("SELECT COUNT(*) AS total %s %s", fromClause, whereClause)
	if err := s.repo.DB().Raw(countQuery, args...).Scan(&count).Error; err != nil {
		return nil, err
	}

	isDeletedExpr := "CASE WHEN ct.hash IS NULL THEN 1 ELSE 0 END AS is_deleted"
	if strings.EqualFold(dbType, "postgresql") {
		isDeletedExpr = "CASE WHEN ct.hash IS NULL THEN true ELSE false END AS is_deleted"
	}
	rowIDExpr := seedParameterRowIDExpr(dbType)
	lastPublishAt := lastPublishAtExpr(dbType)

	dataQuery := fmt.Sprintf(`
		SELECT %s, sp.hash, sp.torrent_id, sp.site_name, sp.nickname,
		       COALESCE(ct.save_path, '') AS save_path,
		       ct.downloader_id AS downloader_id,
		       COALESCE(ct.size, 0) AS size,
		       COALESCE(ct.seeders, 0) AS seeders,
		       COALESCE(sp.name, '') AS name,
		       sp.title, sp.subtitle, sp.type, sp.medium, sp.video_codec,
		       sp.audio_codec, sp.resolution, sp.team, sp.source, sp.tags,
		       sp.title_components, sp.screenshot_review_status,
		       %s,
		       sp.is_reviewed, sp.publish_at,
		       COALESCE((
		           SELECT %s
		           FROM publish_logs pl
		           WHERE pl.torrent_id = sp.torrent_id
		             AND (pl.source_site = sp.site_name OR pl.source_site = sp.nickname)
		             AND pl.status IN ('success', 'edited', 'exists')
		       ), '') AS last_publish_at,
		       sp.updated_at
		%s
		%s
		ORDER BY sp.created_at DESC
		LIMIT ? OFFSET ?
	`, rowIDExpr, isDeletedExpr, lastPublishAt, fromClause, whereClause)

	dataArgs := make([]any, 0, len(args)+2)
	dataArgs = append(dataArgs, args...)
	dataArgs = append(dataArgs, params.PageSize, offset)

	pageData, err := s.repo.RawMaps(dataQuery, dataArgs...)
	if err != nil {
		return nil, err
	}

	for _, item := range pageData {
		item["tags"] = parseStringArray(item["tags"])
		item["is_deleted"] = boolFromAny(item["is_deleted"])
		item["is_reviewed"] = boolFromAny(item["is_reviewed"])

		titleComponents := parseAnyArray(item["title_components"])
		item["unrecognized"] = extractUnrecognized(titleComponents)
	}

	// Compute seed_site_count: number of distinct sites currently seeding each torrent.
	seedSiteCountMap := map[string]int{}
	for _, item := range pageData {
		name, _ := item["name"].(string)
		if name != "" {
			if _, exists := seedSiteCountMap[name]; !exists {
				var count int64
				s.repo.DB().Raw("SELECT COUNT(DISTINCT sites) FROM torrents WHERE name = ? AND sites IS NOT NULL AND sites != ''", name).Scan(&count)
				seedSiteCountMap[name] = int(count)
			}
			item["seed_site_count"] = seedSiteCountMap[name]
		}
	}

	targetSites, err := s.repo.RawStrings("SELECT nickname FROM sites WHERE migration IN (2, 3) ORDER BY sort_order, nickname", "nickname")
	if err != nil {
		targetSites = []string{}
	}
	uniquePaths, err := s.repo.RawStrings(`
		SELECT DISTINCT ct.save_path
		FROM seed_parameters sp
		JOIN (`+currentTorrentsSubquery+`) ct ON sp.hash = ct.hash
		WHERE ct.save_path IS NOT NULL AND ct.save_path != ''
		ORDER BY ct.save_path
	`, "save_path")
	if err != nil {
		uniquePaths = []string{}
	}

	return map[string]any{
		"success":          true,
		"data":             pageData,
		"count":            len(pageData),
		"total":            int(count.Total),
		"page":             params.Page,
		"page_size":        params.PageSize,
		"reverse_mappings": reversemapping.Build(nil),
		"unique_paths":     uniquePaths,
		"target_sites":     targetSites,
	}, nil
}

func (s *CrossSeedService) DeleteCrossSeedData(payload map[string]any) (map[string]any, int) {
	deleteFiles := boolFromAny(payload["delete_files"])
	if rawItems, ok := payload["items"]; ok {
		items, ok := rawItems.([]any)
		if !ok {
			return map[string]any{"success": false, "error": "items 必须是数组"}, 400
		}
		if len(items) == 0 {
			return map[string]any{"success": false, "error": "项目列表不能为空"}, 400
		}
		deletedCount := int64(0)
		fileDeletedCount := int64(0)
		fileDeleteErrors := make([]string, 0)
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			torrentID := toString(item["torrent_id"], "")
			siteName := toString(item["site_name"], "")
			if torrentID == "" || siteName == "" {
				continue
			}
			if deleteFiles {
				if deleted, err := s.deleteDownloaderTorrentForCrossSeed(item); err != nil {
					message := fmt.Sprintf("%s/%s: %v", torrentID, siteName, err)
					fileDeleteErrors = append(fileDeleteErrors, message)
					logx.Warnf(crossSeedLogModule, "删除下载器任务失败 torrent_id=%s site=%s err=%v", torrentID, siteName, err)
				} else if deleted {
					fileDeletedCount++
				}
			}
			// Python baseline increments deleted_count per valid item, regardless of DB rows affected.
			_, _ = s.repo.Exec("DELETE FROM seed_parameters WHERE torrent_id = ? AND site_name = ?", torrentID, siteName)
			deletedCount++
		}
		message := fmt.Sprintf("成功删除 %d 条数据", deletedCount)
		if deleteFiles {
			message = fmt.Sprintf("%s，已请求删除 %d 个下载器任务和文件", message, fileDeletedCount)
			if len(fileDeleteErrors) > 0 {
				message = fmt.Sprintf("%s，%d 个文件删除失败", message, len(fileDeleteErrors))
			}
		}
		return map[string]any{
			"success":                  true,
			"message":                  message,
			"deleted_count":            deletedCount,
			"file_deleted_count":       fileDeletedCount,
			"file_delete_failed_count": len(fileDeleteErrors),
			"file_delete_errors":       fileDeleteErrors,
		}, 200
	}

	torrentID := toString(payload["torrent_id"], "")
	siteName := toString(payload["site_name"], "")
	if torrentID == "" || siteName == "" {
		return map[string]any{"success": false, "error": "缺少必需参数: 单个删除需要 torrent_id 和 site_name，批量删除需要 items 数组"}, 400
	}
	fileDeleted := false
	fileDeleteErrors := []string{}
	if deleteFiles {
		if deleted, deleteErr := s.deleteDownloaderTorrentForCrossSeed(payload); deleteErr != nil {
			fileDeleteErrors = append(fileDeleteErrors, deleteErr.Error())
			logx.Warnf(crossSeedLogModule, "删除下载器任务失败 torrent_id=%s site=%s err=%v", torrentID, siteName, deleteErr)
		} else {
			fileDeleted = deleted
		}
	}
	_, err := s.repo.Exec("DELETE FROM seed_parameters WHERE torrent_id = ? AND site_name = ?", torrentID, siteName)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, 500
	}
	message := fmt.Sprintf("种子数据 %s from %s 已删除", torrentID, siteName)
	if deleteFiles {
		if len(fileDeleteErrors) > 0 {
			message += "，但文件删除失败"
		} else if fileDeleted {
			message += "，已请求删除下载器任务和文件"
		} else {
			message += "，未找到可删除的下载器任务"
		}
	}
	return map[string]any{
		"success":                  true,
		"message":                  message,
		"file_deleted_count":       boolCount(fileDeleted),
		"file_delete_failed_count": len(fileDeleteErrors),
		"file_delete_errors":       fileDeleteErrors,
	}, 200
}

// deleteDownloaderTorrentForCrossSeed 删除一种多站记录对应的下载器任务和文件。
// 参数/返回：item 包含 torrent_id、site_name，可选 hash/downloader_id；返回 true 表示已向下载器发起删除。
// 失败场景：下载器配置读取失败、下载器接口删除失败时返回 error；找不到下载器任务时返回 false。
// 副作用：deleteFiles 固定为 true，会请求下载器同步删除任务文件。
func (s *CrossSeedService) deleteDownloaderTorrentForCrossSeed(item map[string]any) (bool, error) {
	if s == nil || s.repo == nil {
		return false, fmt.Errorf("一种多站服务未初始化")
	}
	hash := strings.TrimSpace(toString(item["hash"], ""))
	downloaderID := strings.TrimSpace(toString(item["downloader_id"], ""))
	if hash == "" || downloaderID == "" {
		resolvedHash, resolvedDownloaderID, err := s.resolveCrossSeedDownloaderTarget(
			toString(item["torrent_id"], ""),
			toString(item["site_name"], ""),
		)
		if err != nil {
			return false, err
		}
		if hash == "" {
			hash = resolvedHash
		}
		if downloaderID == "" {
			downloaderID = resolvedDownloaderID
		}
	}
	if hash == "" || downloaderID == "" {
		return false, nil
	}
	downloader, err := downloaderclient.FromConfig(s.rootConfig(), downloaderID)
	if err != nil {
		return false, err
	}
	if err := downloader.DeleteTorrents([]string{hash}, true); err != nil {
		return false, err
	}
	return true, nil
}

// resolveCrossSeedDownloaderTarget 从数据库补齐一种多站删除文件所需的 hash 与下载器 ID。
// 参数/返回：torrentID/siteName 定位 seed_parameters 记录；返回当前下载器任务的 hash/downloader_id。
// 失败场景：数据库查询失败时返回 error；记录不存在时返回空值。
// 副作用：无，只读取 seed_parameters 与 torrents。
func (s *CrossSeedService) resolveCrossSeedDownloaderTarget(torrentID, siteName string) (string, string, error) {
	torrentID = strings.TrimSpace(torrentID)
	siteName = strings.TrimSpace(siteName)
	if torrentID == "" || siteName == "" {
		return "", "", nil
	}
	currentTorrentsSubquery := buildCurrentTorrentsSubquery(s.repo.DBType())
	query := fmt.Sprintf(`
		SELECT sp.hash AS hash, COALESCE(ct.downloader_id, '') AS downloader_id
		FROM seed_parameters sp
		LEFT JOIN (%s) ct ON sp.hash = ct.hash
		WHERE sp.torrent_id = ? AND sp.site_name = ?
		LIMIT 1
	`, currentTorrentsSubquery)
	rows, err := s.repo.RawMaps(query, torrentID, siteName)
	if err != nil {
		return "", "", err
	}
	if len(rows) == 0 {
		return "", "", nil
	}
	return strings.TrimSpace(toString(rows[0]["hash"], "")), strings.TrimSpace(toString(rows[0]["downloader_id"], "")), nil
}

func (s *CrossSeedService) rootConfig() map[string]any {
	if s == nil || s.cfg == nil {
		return map[string]any{}
	}
	return s.cfg.Get()
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *CrossSeedService) GetSeedSites(torrentName string) (map[string]any, error) {
	if strings.TrimSpace(torrentName) == "" {
		return map[string]any{"success": false, "error": "缺少 torrent name 参数"}, nil
	}

	query := `
		SELECT DISTINCT sites AS site_name
		FROM torrents
		WHERE name = ? AND sites IS NOT NULL AND sites != ''
		ORDER BY sites
	`

	rows, err := s.repo.RawMaps(query, torrentName)
	if err != nil {
		return nil, err
	}

	// Build nickname map from sites table
	type siteRow struct {
		Nickname string `gorm:"column:nickname"`
		SiteName string `gorm:"column:site_name"`
	}
	var siteRows []siteRow
	s.repo.DB().Raw("SELECT nickname, site_name FROM sites WHERE site_name IS NOT NULL AND site_name != ''").Scan(&siteRows)
	nicknameMap := map[string]string{}
	for _, sr := range siteRows {
		nicknameMap[sr.SiteName] = sr.Nickname
		if sr.Nickname != "" {
			nicknameMap[sr.Nickname] = sr.Nickname
		}
	}

	sites := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		siteName, _ := row["site_name"].(string)
		if siteName == "" {
			continue
		}
		nickname := siteName
		if n, ok := nicknameMap[siteName]; ok && n != "" {
			nickname = n
		}
		sites = append(sites, map[string]any{
			"site_name": siteName,
			"nickname":  nickname,
		})
	}

	return map[string]any{
		"success": true,
		"sites":   sites,
		"count":   len(sites),
	}, nil
}

func (s *CrossSeedService) UpdatePublishAt(payload map[string]any) (map[string]any, int) {
	torrentID := toString(payload["torrent_id"], "")
	siteName := toString(payload["site_name"], "")
	if torrentID == "" || siteName == "" {
		return map[string]any{"success": false, "error": "缺少必需参数: torrent_id 和 site_name"}, 400
	}

	publishAtRaw, exists := payload["publish_at"]
	if !exists {
		return map[string]any{"success": false, "error": "缺少 publish_at 参数"}, 400
	}

	var publishAt any
	if publishAtRaw == nil || publishAtRaw == "" {
		publishAt = nil
	} else {
		publishAtStr, ok := publishAtRaw.(string)
		if !ok {
			return map[string]any{"success": false, "error": "publish_at 格式错误"}, 400
		}
		publishAt = strings.TrimSpace(publishAtStr)
	}

	affected, err := s.repo.Exec(
		"UPDATE seed_parameters SET publish_at = ? WHERE torrent_id = ? AND site_name = ?",
		publishAt, torrentID, siteName,
	)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, 500
	}
	if affected == 0 {
		return map[string]any{"success": false, "error": "未找到匹配的种子数据"}, 404
	}
	return map[string]any{"success": true, "message": "可发种时间已更新"}, 200
}
