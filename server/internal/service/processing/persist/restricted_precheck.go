package persist

import (
	"strings"

	processingtagging "github.com/pt-nexus/server/internal/service/processing/tagging"
)

// FetchRestrictionPrecheckResult 表示抓取修复前的受限标签预检结果。
type FetchRestrictionPrecheckResult struct {
	Matched        bool
	SkipRepairs    bool
	RestrictedTags []string
	Reason         string
}

// DetectFetchRestrictionPrecheck 在抓取修复前预检受限标签，命中时用于短路后续修复。
func DetectFetchRestrictionPrecheck(
	draft *SeedDraft,
	siteIdentifier string,
	savePath string,
	torrentNameForPath string,
	downloaderID string,
	rootConfig map[string]any,
) FetchRestrictionPrecheckResult {
	if draft == nil {
		return FetchRestrictionPrecheckResult{}
	}

	restricted := processingtagging.DetectRestrictedTags(draft.RawTags)
	if len(restricted) > 0 {
		return FetchRestrictionPrecheckResult{
			Matched:        true,
			SkipRepairs:    true,
			RestrictedTags: restricted,
			Reason:         "源站显式标签命中受限规则",
		}
	}

	description := strings.TrimSpace(strings.Join([]string{draft.Statement, draft.Body}, "\n"))
	completion := processingtagging.CheckCompletionStatusWithDownloaderContext(
		draft.Title,
		draft.Subtitle,
		description,
		processingtagging.CompletionCheckContext{
			SavePath:     savePath,
			TorrentName:  torrentNameForPath,
			ContentName:  draft.Title,
			DownloaderID: downloaderID,
			RootConfig:   rootConfig,
		},
	)
	episodeTagResult := processingtagging.DetectEpisodeTag(processingtagging.EpisodeTagInput{
		Title:            draft.Title,
		Subtitle:         draft.Subtitle,
		TorrentName:      torrentNameForPath,
		Type:             draft.Type,
		ExistingTags:     draft.RawTags,
		TorrentFileNames: draft.TorrentFileNames,
		Completion:       completion,
	})
	if episodeTagResult.Matched {
		return FetchRestrictionPrecheckResult{
			Matched:        true,
			SkipRepairs:    true,
			RestrictedTags: []string{"tag.分集"},
			Reason:         episodeTagResult.Reason,
		}
	}

	_ = siteIdentifier
	return FetchRestrictionPrecheckResult{}
}
