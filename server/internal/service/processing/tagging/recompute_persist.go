package tagging

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/pt-nexus/server-go/internal/platform/logx"
)

const tagCompletionLogModule = "迁移-标签补全"

// RecomputePersistRepo 定义标签重算并回写所需的最小仓储接口。
type RecomputePersistRepo interface {
	GetSeedParameterByKey(hash, torrentID, siteName string) (map[string]any, error)
	GetCurrentTorrentByHash(hash string) (map[string]any, error)
	UpdateSeedParameterByKey(hash, torrentID, siteName string, updates map[string]any) error
}

// RecomputeAndPersistInput 定义标签重算并回写输入参数。
type RecomputeAndPersistInput struct {
	Repo               RecomputePersistRepo
	Hash               string
	TorrentID          string
	SiteName           string
	SavePath           string
	TorrentNameForPath string
	DownloaderID       string
	RootConfig         map[string]any
	Reason             string
	Now                time.Time
	ComposeSeedID      func(hash, torrentID, siteName string) string
}

// RecomputeAndPersistTagsIfNeeded 在种子未审核时，基于最新文本重算标准标签并写回数据库。
// 参数/返回：输入包含种子标识、路径上下文与仓储；函数无返回值，日志用于追踪结果。
// 失败场景：种子不存在、已审核或写库失败时会直接返回，不阻断主流程。
// 副作用：会更新 seed_parameters.tags，必要时更新 type 与 updated_at。
func RecomputeAndPersistTagsIfNeeded(input RecomputeAndPersistInput) {
	if input.Repo == nil {
		return
	}

	hash := strings.TrimSpace(input.Hash)
	torrentID := strings.TrimSpace(input.TorrentID)
	siteName := strings.TrimSpace(input.SiteName)
	if hash == "" || torrentID == "" || siteName == "" {
		return
	}

	row, err := input.Repo.GetSeedParameterByKey(hash, torrentID, siteName)
	if err != nil || len(row) == 0 {
		return
	}
	if boolFromAnyLocal(row["is_reviewed"]) {
		return
	}

	titleComponents := parseAnyArrayLocal(row["title_components"])
	existingTags := parseStringArrayLocal(row["tags"])
	savePath := strings.TrimSpace(input.SavePath)
	torrentNameForPath := strings.TrimSpace(input.TorrentNameForPath)
	downloaderID := strings.TrimSpace(input.DownloaderID)
	if savePath == "" || torrentNameForPath == "" || downloaderID == "" {
		if current, currentErr := input.Repo.GetCurrentTorrentByHash(hash); currentErr == nil && len(current) > 0 {
			if savePath == "" {
				savePath = strings.TrimSpace(toStringAny(current["save_path"], ""))
			}
			if torrentNameForPath == "" {
				torrentNameForPath = strings.TrimSpace(toStringAny(current["name"], ""))
			}
			if downloaderID == "" {
				downloaderID = strings.TrimSpace(toStringAny(current["downloader_id"], ""))
			}
		}
	}
	mapped, typeOverride, unmapped := RecomputeStandardTags(
		siteName,
		toStringAny(row["title"], ""),
		toStringAny(row["subtitle"], ""),
		toStringAny(row["statement"], ""),
		toStringAny(row["body"], ""),
		toStringAny(row["mediainfo"], ""),
		titleComponents,
		savePath,
		torrentNameForPath,
		downloaderID,
		input.RootConfig,
		existingTags,
	)

	encodedTags, _ := json.Marshal(mapped)
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	updates := map[string]any{
		"tags":       string(encodedTags),
		"updated_at": now.Format("2006-01-02 15:04:05"),
	}
	if strings.TrimSpace(typeOverride) != "" {
		updates["type"] = typeOverride
	}
	if err := input.Repo.UpdateSeedParameterByKey(hash, torrentID, siteName, updates); err != nil {
		return
	}

	seedID := composeSeedIDLocal(hash, torrentID, siteName)
	if input.ComposeSeedID != nil {
		seedID = strings.TrimSpace(input.ComposeSeedID(hash, torrentID, siteName))
		if seedID == "" {
			seedID = composeSeedIDLocal(hash, torrentID, siteName)
		}
	}

	logx.Infof(
		tagCompletionLogModule,
		"标签补全完成 seed_id=%s reason=%s tags_count=%d tags_sample=%v",
		seedID,
		strings.TrimSpace(input.Reason),
		len(mapped),
		TagSample(mapped, 8),
	)
	if len(unmapped) > 0 {
		logx.Debugf(tagMappingLogModule, "标签映射未命中 seed_id=%s count=%d sample=%v", seedID, len(unmapped), TagSample(unmapped, 12))
	}
}

func composeSeedIDLocal(hash, torrentID, siteName string) string {
	return strings.TrimSpace(hash) + "_" + strings.TrimSpace(torrentID) + "_" + strings.TrimSpace(siteName)
}

func boolFromAnyLocal(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case string:
		lower := strings.ToLower(strings.TrimSpace(typed))
		return lower == "1" || lower == "true" || lower == "yes"
	default:
		return false
	}
}

func parseAnyArrayLocal(value any) []any {
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
		parsed := []any{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return parsed
		}
	}
	return []any{}
}

func parseStringArrayLocal(value any) []string {
	if value == nil {
		return []string{}
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			entry := strings.TrimSpace(toStringAny(item, ""))
			if entry != "" {
				result = append(result, entry)
			}
		}
		return result
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
				result := make([]string, 0, len(parsedAny))
				for _, item := range parsedAny {
					entry := strings.TrimSpace(toStringAny(item, ""))
					if entry != "" {
						result = append(result, entry)
					}
				}
				return result
			}
		}
		parts := strings.Split(trimmed, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			entry := strings.TrimSpace(part)
			if entry != "" {
				result = append(result, entry)
			}
		}
		return result
	default:
		return []string{}
	}
}
