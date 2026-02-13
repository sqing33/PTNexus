package persist

import (
	"encoding/json"
	"strings"
	"time"

	parser "github.com/pt-nexus/server-go/internal/service/acquire/extract"
	processingtagging "github.com/pt-nexus/server-go/internal/service/processing/tagging"
	processingtitle "github.com/pt-nexus/server-go/internal/service/processing/title"
)

// BuildManualUpdateInput 表示转种面板手工编辑回写时的输入参数。
type BuildManualUpdateInput struct {
	GeneratedHash string
	TorrentID     string
	SiteName      string
	TorrentName   string
	Existing      map[string]any
	Updated       map[string]any
}

// BuildManualUpdateOutput 表示手工编辑回写构建结果。
type BuildManualUpdateOutput struct {
	Record       map[string]any
	Standardized map[string]any
}

// BuildManualUpdatedSeedRecord 将前端手工编辑参数构建为可直接写入 seed_parameters 的记录。
// 参数/返回：输入包含 existing/updated/torrent 基本信息；返回 record 与 standardized_params。
// 失败场景：输入字段缺失时自动按旧值或默认值兜底，不返回错误。
// 副作用：无。
func BuildManualUpdatedSeedRecord(input BuildManualUpdateInput) BuildManualUpdateOutput {
	existing := input.Existing
	if existing == nil {
		existing = map[string]any{}
	}
	updated := input.Updated
	if updated == nil {
		updated = map[string]any{}
	}

	torrentID := strings.TrimSpace(input.TorrentID)
	siteName := strings.TrimSpace(input.SiteName)
	hash := strings.TrimSpace(toStringAny(existing["hash"], strings.TrimSpace(input.GeneratedHash)))
	if hash == "" {
		hash = strings.TrimSpace(input.GeneratedHash)
	}

	torrentName := strings.TrimSpace(input.TorrentName)
	if torrentName == "" {
		torrentName = strings.TrimSpace(toStringAny(existing["name"], torrentID))
	}
	title := strings.TrimSpace(toStringAny(updated["title"], torrentName))
	if title == "" {
		title = torrentName
	}

	standardized, ok := updated["standardized_params"].(map[string]any)
	if !ok || len(standardized) == 0 {
		standardized = map[string]any{
			"type":        toStringAny(updated["type"], toStringAny(existing["type"], "")),
			"medium":      toStringAny(updated["medium"], toStringAny(existing["medium"], "")),
			"video_codec": toStringAny(updated["video_codec"], toStringAny(existing["video_codec"], "")),
			"audio_codec": toStringAny(updated["audio_codec"], toStringAny(existing["audio_codec"], "")),
			"resolution":  toStringAny(updated["resolution"], toStringAny(existing["resolution"], "")),
			"team":        toStringAny(updated["team"], toStringAny(existing["team"], "")),
			"source":      toStringAny(updated["source"], toStringAny(existing["source"], "")),
			"tags":        parseStringArray(updated["tags"]),
			"imdb_link":   toStringAny(updated["imdb_link"], toStringAny(existing["imdb_link"], "")),
			"douban_link": toStringAny(updated["douban_link"], toStringAny(existing["douban_link"], "")),
			"tmdb_link":   toStringAny(updated["tmdb_link"], toStringAny(existing["tmdb_link"], "")),
		}
	}

	standardized["team"] = parser.NormalizeTeamKeyForSite(toStringAny(standardized["team"], ""), siteName)
	if mappedTags, _ := processingtagging.MapTagsToStandard(parseStringArray(standardized["tags"]), siteName); len(mappedTags) > 0 {
		standardized["tags"] = mappedTags
	} else {
		standardized["tags"] = []string{}
	}

	titleComponents := parseAnyArray(updated["title_components"])
	if len(titleComponents) == 0 {
		// 对齐 Python：标题组件中的“制作组”从标题本身解析，不使用标准化 team.* 反推。
		titleComponents = processingtitle.MapTitleComponentsToAny(processingtitle.BuildSimpleTitleComponents(title, ""))
	}
	titleComponents = processingtitle.CompleteTitleComponents(titleComponents, title)
	previewTitle := processingtitle.BuildPreviewTitleFromTitleComponents(titleComponents, title)

	removedSource := existing["removed_ardtudeclarations"]
	if _, exists := updated["removed_ardtudeclarations"]; exists {
		removedSource = updated["removed_ardtudeclarations"]
	}
	removedDeclarations := parseStringArray(removedSource)

	now := time.Now().Format("2006-01-02 15:04:05")
	draft := NewSeedDraft(hash, torrentID, siteName, toStringAny(existing["nickname"], siteName))
	draft.Name = torrentName
	draft.Title = previewTitle
	draft.Subtitle = toStringAny(updated["subtitle"], toStringAny(existing["subtitle"], ""))
	draft.IMDbLink = toStringAny(standardized["imdb_link"], toStringAny(existing["imdb_link"], ""))
	draft.DoubanLink = toStringAny(standardized["douban_link"], toStringAny(existing["douban_link"], ""))
	draft.TMDbLink = toStringAny(standardized["tmdb_link"], toStringAny(existing["tmdb_link"], ""))
	draft.Type = toStringAny(standardized["type"], "")
	draft.Medium = toStringAny(standardized["medium"], "")
	draft.VideoCodec = toStringAny(standardized["video_codec"], "")
	draft.AudioCodec = toStringAny(standardized["audio_codec"], "")
	draft.Resolution = toStringAny(standardized["resolution"], "")
	draft.Team = toStringAny(standardized["team"], "")
	draft.Source = toStringAny(standardized["source"], "")
	draft.Tags = parseStringArray(standardized["tags"])
	draft.Poster = toStringAny(updated["poster"], toStringAny(existing["poster"], ""))
	draft.Screenshots = toStringAny(updated["screenshots"], toStringAny(existing["screenshots"], ""))
	draft.Statement = toStringAny(updated["statement"], toStringAny(existing["statement"], ""))
	draft.Body = toStringAny(updated["body"], toStringAny(existing["body"], ""))
	draft.Mediainfo = toStringAny(updated["mediainfo"], toStringAny(existing["mediainfo"], ""))
	draft.TitleComponents = titleComponentsAnyToMapSlice(titleComponents)
	draft.RemovedARDTUDeclarations = removedDeclarations
	draft.IsReviewed = true
	draft.MediainfoStatus = toStringAny(existing["mediainfo_status"], "queued")
	draft.BDInfoTaskID = existing["bdinfo_task_id"]
	draft.BDInfoStartedAt = existing["bdinfo_started_at"]
	draft.BDInfoCompletedAt = existing["bdinfo_completed_at"]
	draft.BDInfoError = toStringAny(existing["bdinfo_error"], "")
	draft.CreatedAt = NormalizeSeedParameterDateTime(existing["created_at"], now)
	draft.UpdatedAt = now

	return BuildManualUpdateOutput{
		Record:       draft.ToSeedParameterRecord(),
		Standardized: standardized,
	}
}

func parseAnyArray(value any) []any {
	if value == nil {
		return []any{}
	}
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return []any{}
		}
		parsed := []any{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return parsed
		}
		return []any{}
	default:
		return []any{}
	}
}

func titleComponentsAnyToMapSlice(items []any) []map[string]any {
	if len(items) == 0 {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		component, ok := item.(map[string]any)
		if !ok || len(component) == 0 {
			continue
		}
		result = append(result, component)
	}
	return result
}
