package tagging

import (
	"regexp"
	"strings"
)

var (
	reEpisodeTokenStrong = regexp.MustCompile(`(?i)\bS\d{1,2}E\d{1,3}(?:\s*[-~]\s*(?:S?\d{1,2})?E?\d{1,3})?\b`)
	reEpisodeTokenLoose  = regexp.MustCompile(`(?i)\b(?:E[Pp]?|Episode)\s*0?\d{1,3}\b`)
	reEpisodeTokenCN     = regexp.MustCompile(`第\s*\d{1,4}\s*[集话話期]`)
	reSeasonOnlyToken    = regexp.MustCompile(`(?i)\bS\d{1,2}\b`)
	reEpisodeFromName    = regexp.MustCompile(`(?i)[Ss](\d{1,2})[Ee](\d{1,3})`)
)

// EpisodeTagInput 定义本地“分集”标签判定所需输入。
type EpisodeTagInput struct {
	Title            string
	Subtitle         string
	TorrentName      string
	Type             string
	ExistingTags     []string
	TorrentFileNames []string
	Completion       CompletionStatus
}

// EpisodeTagResult 表示“分集”标签判定结果。
type EpisodeTagResult struct {
	Matched bool
	Reason  string
}

// DetectEpisodeTag 判断当前资源是否应补 tag.分集。
// 规则：需要明确的剧集证据；“未完结”仅作为辅助反证，不单独触发。
func DetectEpisodeTag(input EpisodeTagInput) EpisodeTagResult {
	if hasRestrictedEpisodeTag(input.ExistingTags) {
		return EpisodeTagResult{Matched: true, Reason: "已存在分集标签，直接保留"}
	}
	if !looksLikeEpisodeContent(input) {
		return EpisodeTagResult{Matched: false, Reason: "缺少剧集特征，跳过分集判定"}
	}

	titleSignal := hasEpisodeToken(input.Title) || hasEpisodeToken(input.TorrentName)
	subtitleSignal := hasEpisodeToken(input.Subtitle)
	fileStats := collectEpisodeFileStats(input.TorrentFileNames)

	if subtitleSignal {
		if fileStats.UniqueEpisodes == 0 {
			return EpisodeTagResult{Matched: true, Reason: "副标题命中单集特征"}
		}
		if fileStats.UniqueEpisodes <= 3 {
			return EpisodeTagResult{Matched: true, Reason: "副标题命中集号且 torrent 文件列表仅包含小范围集段"}
		}
		if input.Completion.TotalEpisodes != nil && fileStats.UniqueEpisodes < *input.Completion.TotalEpisodes {
			return EpisodeTagResult{Matched: true, Reason: "副标题命中集号且 torrent 文件列表少于简介总集数"}
		}
	}

	if titleSignal {
		// 标题或 torrent 名明确带集号时，单集/小范围连载优先视为分集。
		if fileStats.UniqueEpisodes == 0 {
			return EpisodeTagResult{Matched: true, Reason: "标题或 torrent 名命中单集特征"}
		}
		if fileStats.UniqueEpisodes <= 3 {
			return EpisodeTagResult{Matched: true, Reason: "标题命中集号且 torrent 文件列表仅包含小范围集段"}
		}
		if input.Completion.TotalEpisodes != nil && fileStats.UniqueEpisodes < *input.Completion.TotalEpisodes {
			return EpisodeTagResult{Matched: true, Reason: "标题命中集号且 torrent 文件列表少于简介总集数"}
		}
	}

	if fileStats.UniqueEpisodes == 1 {
		return EpisodeTagResult{Matched: true, Reason: "torrent 文件列表仅识别到单集文件"}
	}

	if input.Completion.TotalEpisodes != nil && input.Completion.LocalEpisodes != nil {
		total := *input.Completion.TotalEpisodes
		local := *input.Completion.LocalEpisodes
		if total > 0 && local > 0 && local < total {
			if titleSignal || subtitleSignal {
				return EpisodeTagResult{Matched: true, Reason: "本地集数少于简介总集数，且存在集号证据"}
			}
		}
	}

	if fileStats.UniqueEpisodes > 0 {
		return EpisodeTagResult{Matched: false, Reason: "识别到剧集文件，但当前更像整季包或完整合集"}
	}
	return EpisodeTagResult{Matched: false, Reason: "存在剧集上下文，但未命中可靠的分集证据"}
}

func hasRestrictedEpisodeTag(tags []string) bool {
	for _, tag := range tags {
		switch strings.TrimSpace(tag) {
		case "分集", "tag.分集":
			return true
		}
	}
	return false
}

func looksLikeEpisodeContent(input EpisodeTagInput) bool {
	if hasEpisodeToken(input.Title) || hasEpisodeToken(input.Subtitle) || hasEpisodeToken(input.TorrentName) {
		return true
	}
	if collectEpisodeFileStats(input.TorrentFileNames).UniqueEpisodes > 0 {
		return true
	}
	if hasAnimationTag(input.ExistingTags) {
		return input.Completion.TotalEpisodes != nil || input.Completion.LocalEpisodes != nil
	}
	switch strings.TrimSpace(input.Type) {
	case "category.tv_series", "category.tv_shows":
		return input.Completion.TotalEpisodes != nil || input.Completion.LocalEpisodes != nil
	default:
		return false
	}
}

func hasAnimationTag(tags []string) bool {
	for _, tag := range tags {
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "tag.动漫", "tag.动画", "动漫", "动画", "anime", "animation":
			return true
		}
	}
	return false
}

func hasEpisodeToken(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if reEpisodeTokenStrong.MatchString(trimmed) || reEpisodeTokenCN.MatchString(trimmed) || reEpisodeTokenLoose.MatchString(trimmed) {
		return true
	}
	// 仅有季号不足以判定分集，避免整季包误判。
	return false
}

type episodeFileStats struct {
	UniqueEpisodes int
	HasSeasonOnly  bool
}

func collectEpisodeFileStats(fileNames []string) episodeFileStats {
	episodes := map[string]struct{}{}
	hasSeasonOnly := false
	for _, raw := range fileNames {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if reEpisodeTokenStrong.MatchString(name) || reEpisodeTokenCN.MatchString(name) || reEpisodeTokenLoose.MatchString(name) {
			matches := reEpisodeFromName.FindAllStringSubmatch(name, -1)
			if len(matches) > 0 {
				for _, match := range matches {
					if len(match) < 3 {
						continue
					}
					key := strings.ToUpper(strings.TrimSpace(match[1] + "-" + match[2]))
					episodes[key] = struct{}{}
				}
			} else {
				episodes[strings.ToUpper(name)] = struct{}{}
			}
		} else if reSeasonOnlyToken.MatchString(name) {
			hasSeasonOnly = true
		}
	}
	return episodeFileStats{
		UniqueEpisodes: len(episodes),
		HasSeasonOnly:  hasSeasonOnly,
	}
}
