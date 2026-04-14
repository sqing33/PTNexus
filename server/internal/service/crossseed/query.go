package crossseed

import (
	"fmt"
	"strings"

	"github.com/pt-nexus/server/internal/service/reversemapping"
)

var inactiveTorrentStates = []string{"未做种", "已暂停", "已停止", "错误", "等待", "队列"}

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

func buildReviewStatusCondition(dbType string, reviewStatus string) (string, []any) {
	normalized := strings.ToLower(strings.TrimSpace(reviewStatus))
	if normalized == "" {
		return "", nil
	}

	isDeletedCondition := "ct.hash IS NULL"
	isNotDeletedCondition := "ct.hash IS NOT NULL"

	switch normalized {
	case "reviewed", "unreviewed":
		if strings.EqualFold(dbType, "postgresql") {
			isReviewedValue := "false"
			if normalized == "reviewed" {
				isReviewedValue = "true"
			}
			condition := fmt.Sprintf(
				`sp.is_reviewed = %s AND %s AND (sp.title_components IS NULL OR sp.title_components::text !~ ?)`,
				isReviewedValue,
				isNotDeletedCondition,
			)
			return condition, []any{`"无法识别"[^}]*"value":\s*".+"`}
		}

		isReviewedValue := "0"
		if normalized == "reviewed" {
			isReviewedValue = "1"
		}

		if strings.EqualFold(dbType, "mysql") {
			condition := fmt.Sprintf(
				`sp.is_reviewed = %s AND %s AND (sp.title_components IS NULL OR sp.title_components NOT REGEXP ?)`,
				isReviewedValue,
				isNotDeletedCondition,
			)
			return condition, []any{`"无法识别"[^}]*"value":\s*".+"`}
		}

		condition := fmt.Sprintf(
			`sp.is_reviewed = %s AND %s AND (sp.title_components IS NULL OR NOT (sp.title_components LIKE ? AND sp.title_components NOT LIKE ?))`,
			isReviewedValue,
			isNotDeletedCondition,
		)
		return condition, []any{`%"无法识别"%`, `%"value": ""%`}
	case "error":
		if strings.EqualFold(dbType, "postgresql") {
			condition := fmt.Sprintf(
				`(%s OR sp.title_components::text ~ ?)`,
				isDeletedCondition,
			)
			return condition, []any{`"无法识别"[^}]*"value":\s*".+"`}
		}

		if strings.EqualFold(dbType, "mysql") {
			condition := fmt.Sprintf(
				`(%s OR sp.title_components REGEXP ?)`,
				isDeletedCondition,
			)
			return condition, []any{`"无法识别"[^}]*"value":\s*".+"`}
		}

		condition := fmt.Sprintf(
			`(%s OR (sp.title_components LIKE ? AND sp.title_components NOT LIKE ?))`,
			isDeletedCondition,
		)
		return condition, []any{`%"无法识别"%`, `%"value": ""%`}
	default:
		return "", nil
	}
}

func buildCurrentTorrentsSubquery(dbType string) string {
	// Match Python build_current_torrents_subquery().
	if strings.EqualFold(dbType, "postgresql") {
		return fmt.Sprintf(`
			SELECT DISTINCT ON (t.hash) t.hash, t.name, t.size, t.save_path, t.downloader_id, t.state, t.last_seen
			FROM torrents t
			WHERE (t.is_hidden = 0 OR t.is_hidden IS NULL)
			ORDER BY t.hash, %s, t.last_seen DESC
		`, stateRankExpr("t"))
	}
	return fmt.Sprintf(`
		SELECT t.hash, t.name, t.size, t.save_path, t.downloader_id, t.state, t.last_seen
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
	query := `
		SELECT DISTINCT t.save_path
		FROM torrents t
		WHERE (t.is_hidden = 0 OR t.is_hidden IS NULL)
		  AND t.save_path IS NOT NULL AND t.save_path != ''
		  AND EXISTS (
			SELECT 1
			FROM seed_parameters sp
			WHERE sp.hash = t.hash
		  )
		ORDER BY t.save_path
	`
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
	if excludedTarget := strings.TrimSpace(params.ExcludeTargetSites); excludedTarget != "" {
		whereConditions = append(
			whereConditions,
			`ct.hash IS NOT NULL AND NOT EXISTS (
				SELECT 1
				FROM torrents tx
				WHERE (tx.is_hidden = 0 OR tx.is_hidden IS NULL)
				  AND LOWER(tx.sites) = LOWER(?)
				  AND tx.name = ct.name
				  AND tx.size = ct.size
			)`,
		)
		args = append(args, excludedTarget)
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

	dataQuery := fmt.Sprintf(`
		SELECT sp.hash, sp.torrent_id, sp.site_name, sp.nickname,
		       COALESCE(ct.save_path, '') AS save_path,
		       ct.downloader_id AS downloader_id,
		       sp.title, sp.subtitle, sp.type, sp.medium, sp.video_codec,
		       sp.audio_codec, sp.resolution, sp.team, sp.source, sp.tags,
		       sp.title_components, sp.screenshot_review_status,
		       %s,
		       sp.is_reviewed, sp.updated_at
		%s
		%s
		ORDER BY sp.created_at DESC
		LIMIT ? OFFSET ?
	`, isDeletedExpr, fromClause, whereClause)

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

	targetSites, err := s.repo.RawStrings("SELECT nickname FROM sites WHERE migration IN (2, 3) ORDER BY nickname", "nickname")
	if err != nil {
		targetSites = []string{}
	}
	result := map[string]any{
		"success":          true,
		"data":             pageData,
		"count":            len(pageData),
		"total":            int(count.Total),
		"page":             params.Page,
		"page_size":        params.PageSize,
		"reverse_mappings": reversemapping.Build(nil),
		"target_sites":     targetSites,
	}

	if params.IncludeUniquePaths {
		uniquePaths, err := s.UniquePaths()
		if err == nil {
			if values, ok := uniquePaths["unique_paths"]; ok {
				result["unique_paths"] = values
			}
		}
	}

	return result, nil
}

func (s *CrossSeedService) DeleteCrossSeedData(payload map[string]any) (map[string]any, int) {
	if rawItems, ok := payload["items"]; ok {
		items, ok := rawItems.([]any)
		if !ok {
			return map[string]any{"success": false, "error": "items 必须是数组"}, 400
		}
		if len(items) == 0 {
			return map[string]any{"success": false, "error": "项目列表不能为空"}, 400
		}
		deletedCount := int64(0)
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
			// Python baseline increments deleted_count per valid item, regardless of DB rows affected.
			_, _ = s.repo.Exec("DELETE FROM seed_parameters WHERE torrent_id = ? AND site_name = ?", torrentID, siteName)
			deletedCount++
		}
		return map[string]any{"success": true, "message": fmt.Sprintf("成功删除 %d 条数据", deletedCount), "deleted_count": deletedCount}, 200
	}

	torrentID := toString(payload["torrent_id"], "")
	siteName := toString(payload["site_name"], "")
	if torrentID == "" || siteName == "" {
		return map[string]any{"success": false, "error": "缺少必需参数: 单个删除需要 torrent_id 和 site_name，批量删除需要 items 数组"}, 400
	}
	_, err := s.repo.Exec("DELETE FROM seed_parameters WHERE torrent_id = ? AND site_name = ?", torrentID, siteName)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, 500
	}
	return map[string]any{"success": true, "message": fmt.Sprintf("种子数据 %s from %s 已删除", torrentID, siteName)}, 200
}
