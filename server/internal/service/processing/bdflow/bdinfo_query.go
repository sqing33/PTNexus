package bdflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	parser "github.com/pt-nexus/server-go/internal/service/acquire/extract"
	processingmedia "github.com/pt-nexus/server-go/internal/service/processing/media"
	processingpersist "github.com/pt-nexus/server-go/internal/service/processing/persist"
	processingshared "github.com/pt-nexus/server-go/internal/service/processing/shared"
	"gorm.io/gorm"
)

// ErrInvalidSeedID 表示 seed_id 格式不合法。
var ErrInvalidSeedID = errors.New("invalid seed id")

// BDInfoStatusRepo 定义查询 BDInfo 状态所需的最小仓储接口。
type BDInfoStatusRepo interface {
	GetSeedParameterByKey(hash, torrentID, siteName string) (map[string]any, error)
}

// BDInfoStatusResult 表示 BDInfo 状态查询结果。
type BDInfoStatusResult struct {
	Response map[string]any
	TaskID   string
}

// BDInfoRecordsQueryResult 表示 BDInfo 列表查询结果。
type BDInfoRecordsQueryResult struct {
	Records  []map[string]any
	Total    int64
	Page     int
	PageSize int
}

// QueryBDInfoStatus 查询单个 seed_id 的 BDInfo 状态基础信息（不含内存任务进度）。
func QueryBDInfoStatus(repo BDInfoStatusRepo, seedID string) (BDInfoStatusResult, error) {
	hash, torrentID, siteName, err := processingpersist.ParseSeedID(seedID)
	if err != nil {
		return BDInfoStatusResult{}, fmt.Errorf("%w: %v", ErrInvalidSeedID, err)
	}
	row, err := repo.GetSeedParameterByKey(hash, torrentID, siteName)
	if err != nil {
		return BDInfoStatusResult{}, err
	}

	status := trimToDefault(row["mediainfo_status"], "queued")
	taskID := strings.TrimSpace(toStringAny(row["bdinfo_task_id"]))
	var taskIDValue any = nil
	if taskID != "" {
		taskIDValue = taskID
	}
	mediainfoText := toStringAny(row["mediainfo"])
	response := map[string]any{
		"seed_id":             seedID,
		"mediainfo_status":    status,
		"bdinfo_task_id":      taskIDValue,
		"bdinfo_started_at":   row["bdinfo_started_at"],
		"bdinfo_completed_at": row["bdinfo_completed_at"],
		"bdinfo_error":        trimToDefault(row["bdinfo_error"], ""),
		"mediainfo":           nil,
		"is_bdinfo":           strings.Contains(mediainfoText, "DISC INFO"),
		"task_status":         nil,
		"seed_updates":        nil,
	}
	if status == "completed" {
		response["mediainfo"] = mediainfoText
		response["seed_updates"] = buildSeedUpdatesFromRow(row)
	}

	return BDInfoStatusResult{
		Response: response,
		TaskID:   taskID,
	}, nil
}

// buildSeedUpdatesFromRow 组装回传前端的字段更新集（反映当前数据库最新值，不引入额外落库点）。
// 参数/返回：row 为 seed_parameters 当前行；返回 seed_updates 对象（可能为空）。
// 失败场景：row 为空时返回空 map。
// 副作用：无。
func buildSeedUpdatesFromRow(row map[string]any) map[string]any {
	if len(row) == 0 {
		return map[string]any{}
	}
	title := strings.TrimSpace(toStringAny(row["title"]))
	mediainfoText := strings.TrimSpace(toStringAny(row["mediainfo"]))
	body := strings.TrimSpace(toStringAny(row["body"]))
	inferred := parser.InferStandardizedValues(title, mediainfoText, body)

	return map[string]any{
		"title_components": parseJSONArray(row["title_components"]),
		"standardized_params": map[string]any{
			"type":   strings.TrimSpace(toStringAny(row["type"])),
			"medium": strings.TrimSpace(toStringAny(row["medium"])),
			"tags":   parseJSONStringArray(row["tags"]),
		},
		"inferred_standardized_params": map[string]any{
			"video_codec": strings.TrimSpace(inferred["video_codec"]),
			"audio_codec": strings.TrimSpace(inferred["audio_codec"]),
			"resolution":  strings.TrimSpace(inferred["resolution"]),
		},
	}
}

func parseJSONArray(value any) []any {
	if value == nil {
		return []any{}
	}
	if typed, ok := value.([]any); ok {
		return typed
	}
	if typed, ok := value.(string); ok {
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return []any{}
		}
		if strings.HasPrefix(trimmed, "[") {
			parsed := []any{}
			if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
				return parsed
			}
		}
	}
	return []any{}
}

func parseJSONStringArray(value any) []string {
	if value == nil {
		return []string{}
	}
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			trimmed := strings.TrimSpace(toStringAny(item))
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return []string{}
		}
		if strings.HasPrefix(trimmed, "[") {
			parsed := []string{}
			if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
				return parsed
			}
			parsedAny := []any{}
			if err := json.Unmarshal([]byte(trimmed), &parsedAny); err == nil {
				out := make([]string, 0, len(parsedAny))
				for _, item := range parsedAny {
					entry := strings.TrimSpace(toStringAny(item))
					if entry != "" {
						out = append(out, entry)
					}
				}
				return out
			}
		}
		parts := strings.Split(trimmed, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			entry := strings.TrimSpace(part)
			if entry != "" {
				out = append(out, entry)
			}
		}
		return out
	default:
		return []string{}
	}
}

// QueryBDInfoRecords 查询 BDInfo 记录列表基础信息（不含内存任务进度）。
func QueryBDInfoRecords(db *gorm.DB, statusFilter string, page int, pageSize int) (BDInfoRecordsQueryResult, error) {
	normalizedPage := page
	if normalizedPage <= 0 {
		normalizedPage = 1
	}
	normalizedPageSize := pageSize
	if normalizedPageSize <= 0 {
		normalizedPageSize = 20
	}
	if normalizedPageSize > 200 {
		normalizedPageSize = 200
	}

	filter := strings.TrimSpace(statusFilter)
	query := db.
		Table("seed_parameters sp").
		Select(`sp.hash, sp.torrent_id, sp.site_name, sp.title,
			COALESCE(s.nickname, sp.site_name) AS nickname,
			sp.mediainfo_status, sp.bdinfo_task_id, sp.bdinfo_started_at, sp.bdinfo_completed_at, sp.bdinfo_error, sp.mediainfo`).
		Joins("LEFT JOIN sites s ON sp.site_name = s.site").
		Where("sp.bdinfo_task_id IS NOT NULL")

	if filter != "" {
		switch filter {
		case "processing":
			query = query.Where("sp.mediainfo_status IN ?", []string{"processing_bdinfo", "processing"})
		case "completed":
			query = query.Where("sp.mediainfo_status = ?", "completed")
		case "failed":
			query = query.Where("(sp.mediainfo_status = ? OR (sp.bdinfo_error IS NOT NULL AND sp.bdinfo_error != ''))", "failed")
		default:
			query = query.Where("sp.mediainfo_status = ?", filter)
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return BDInfoRecordsQueryResult{}, err
	}

	offset := (normalizedPage - 1) * normalizedPageSize
	rows := make([]map[string]any, 0)
	if err := query.Order("sp.bdinfo_started_at DESC").Limit(normalizedPageSize).Offset(offset).Scan(&rows).Error; err != nil {
		return BDInfoRecordsQueryResult{}, err
	}

	records := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		mediainfoText := toStringAny(row["mediainfo"])
		_, isBDInfo, _ := processingmedia.ValidateMediaInfoFormat(mediainfoText)

		records = append(records, map[string]any{
			"seed_id":             processingpersist.ComposeSeedID(toStringAny(row["hash"]), toStringAny(row["torrent_id"]), toStringAny(row["site_name"])),
			"title":               trimToDefault(row["title"], "未知标题"),
			"site_name":           trimToDefault(row["site_name"], "未知站点"),
			"nickname":            trimToDefault(row["nickname"], trimToDefault(row["site_name"], "未知站点")),
			"mediainfo_status":    trimToDefault(row["mediainfo_status"], "unknown"),
			"bdinfo_task_id":      row["bdinfo_task_id"],
			"bdinfo_started_at":   row["bdinfo_started_at"],
			"bdinfo_completed_at": row["bdinfo_completed_at"],
			"bdinfo_error":        row["bdinfo_error"],
			"mediainfo":           mediainfoText,
			"is_bdinfo":           isBDInfo,
		})
	}

	return BDInfoRecordsQueryResult{
		Records:  records,
		Total:    total,
		Page:     normalizedPage,
		PageSize: normalizedPageSize,
	}, nil
}

func trimToDefault(value any, fallback string) string {
	trimmed := strings.TrimSpace(processingshared.ToString(value, ""))
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
