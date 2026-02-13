package mapping

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	reHaidanSeasonEpisodeEN = regexp.MustCompile(`(?i)S(\d+)(?:E(\d+))?`)
	reHaidanSeasonEpisodeCN = regexp.MustCompile(`第(\d+)季(?:第(\d+)集)?`)
	reHaidanImgTag          = regexp.MustCompile(`(?is)\[img\](.*?)\[/img\]`)
)

func applyHaidanOverrides(mapped map[string]string, uploadData map[string]any) {
	if mapped == nil {
		return
	}

	standardized := map[string]any{}
	if uploadData != nil {
		if item, ok := uploadData["standardized_params"].(map[string]any); ok && item != nil {
			standardized = item
		}
	}

	delete(mapped, "team")

	applyHaidanTeamSuffix(mapped, uploadData, standardized)
	applyHaidanSeasonEpisode(mapped, uploadData, standardized)
	applyHaidanCollectionFlag(mapped, uploadData)
	applyHaidanExtraFields(mapped, uploadData, standardized)
}

func applyHaidanTeamSuffix(mapped map[string]string, uploadData map[string]any, standardized map[string]any) {
	teamSuffix := strings.TrimSpace(extractTeamFromTitleComponents(uploadData["title_components"]))
	if teamSuffix == "" {
		if sourceParams, ok := uploadData["source_params"].(map[string]any); ok {
			teamSuffix = strings.TrimSpace(toStringAnyBasic(sourceParams["制作组"], ""))
		}
	}
	if teamSuffix == "" {
		teamSuffix = strings.TrimSpace(toStringAnyBasic(standardized["team"], ""))
	}
	if teamSuffix != "" {
		mapped["team_suffix"] = teamSuffix
	}
}

func applyHaidanSeasonEpisode(mapped map[string]string, uploadData map[string]any, standardized map[string]any) {
	seasonEpisode := resolveHaidanSeasonEpisode(uploadData, standardized)
	if seasonEpisode != "" {
		season, episode := parseHaidanSeasonEpisode(seasonEpisode)
		if season != "" {
			mapped["season"] = season
		}
		if episode != "" {
			mapped["episode"] = episode
		}
		return
	}

	contentType := strings.TrimSpace(toStringAnyBasic(standardized["type"], ""))
	if contentType == "category.animation" || contentType == "category.tv_series" {
		mapped["season"] = "0"
		mapped["episode"] = "0"
	}
}

func resolveHaidanSeasonEpisode(uploadData map[string]any, standardized map[string]any) string {
	if uploadData != nil {
		if text := strings.TrimSpace(extractHaidanTitleComponent(uploadData["title_components"], "季集")); text != "" {
			return text
		}
		if text := strings.TrimSpace(toStringAnyBasic(uploadData["season_episode"], "")); text != "" {
			return text
		}
	}
	if standardized == nil {
		return ""
	}
	return strings.TrimSpace(toStringAnyBasic(standardized["season_episode"], ""))
}

func parseHaidanSeasonEpisode(seasonEpisode string) (string, string) {
	trimmed := strings.TrimSpace(seasonEpisode)
	if trimmed == "" {
		return "", ""
	}

	if match := reHaidanSeasonEpisodeEN.FindStringSubmatch(strings.ToUpper(trimmed)); len(match) >= 2 {
		season := zeroPadDigits(match[1], 2)
		episode := ""
		if len(match) >= 3 && strings.TrimSpace(match[2]) != "" {
			episode = zeroPadDigits(match[2], 2)
		} else if season != "" {
			episode = "0"
		}
		return season, episode
	}

	if match := reHaidanSeasonEpisodeCN.FindStringSubmatch(trimmed); len(match) >= 2 {
		season := zeroPadDigits(match[1], 2)
		episode := ""
		if len(match) >= 3 && strings.TrimSpace(match[2]) != "" {
			episode = zeroPadDigits(match[2], 2)
		} else if season != "" {
			episode = "0"
		}
		return season, episode
	}

	return "", ""
}

func zeroPadDigits(raw string, width int) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) >= width {
		return trimmed
	}
	return strings.Repeat("0", width-len(trimmed)) + trimmed
}

func applyHaidanCollectionFlag(mapped map[string]string, uploadData map[string]any) {
	if !isHaidanCollectionResource(uploadData) {
		return
	}
	mapped["collages"] = "1"
}

func isHaidanCollectionResource(uploadData map[string]any) bool {
	if uploadData == nil {
		return false
	}

	title := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		toStringAnyBasic(uploadData["title"], ""),
		toStringAnyBasic(uploadData["original_main_title"], ""),
		toStringAnyBasic(uploadData["name"], ""),
	)))
	if title != "" {
		for _, keyword := range []string{"合集", "collection", "全集", "complete", "多季", "series"} {
			if strings.Contains(title, keyword) {
				return true
			}
		}
	}

	if sourceParams, ok := uploadData["source_params"].(map[string]any); ok {
		sourceType := strings.ToLower(strings.TrimSpace(toStringAnyBasic(sourceParams["类型"], "")))
		if strings.Contains(sourceType, "合集") || strings.Contains(sourceType, "collection") {
			return true
		}
	}

	seasonEpisode := strings.TrimSpace(extractHaidanTitleComponent(uploadData["title_components"], "季集"))
	return strings.Contains(seasonEpisode, "多季") || strings.Contains(seasonEpisode, "全")
}

func applyHaidanExtraFields(mapped map[string]string, uploadData map[string]any, standardized map[string]any) {
	if doubanLink := resolveHaidanDoubanLink(uploadData, standardized); doubanLink != "" {
		mapped["durl"] = doubanLink
	}

	if screenshots := extractHaidanScreenshotURLs(uploadData); screenshots != "" {
		mapped["preview-pics"] = screenshots
	}

	if mediainfo := strings.TrimSpace(toStringAnyBasic(uploadData["mediainfo"], "")); mediainfo != "" {
		mapped["nfo-string"] = mediainfo
	}
}

func resolveHaidanDoubanLink(uploadData map[string]any, standardized map[string]any) string {
	if uploadData == nil {
		return ""
	}

	if text := strings.TrimSpace(firstNonEmpty(
		toStringAnyBasic(uploadData["douban_link"], ""),
		toStringAnyBasic(uploadData["doubanLink"], ""),
		toStringAnyBasic(uploadData["douban"], ""),
		toStringAnyBasic(uploadData["pt_gen"], ""),
		toStringAnyBasic(uploadData["ptgen"], ""),
	)); text != "" {
		return text
	}
	if standardized == nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(
		toStringAnyBasic(standardized["douban_link"], ""),
		toStringAnyBasic(standardized["doubanLink"], ""),
		toStringAnyBasic(standardized["douban"], ""),
	))
}

func extractHaidanScreenshotURLs(uploadData map[string]any) string {
	if uploadData == nil {
		return ""
	}

	screenshots := strings.TrimSpace(toStringAnyBasic(uploadData["screenshots"], ""))
	if screenshots == "" {
		if intro, ok := uploadData["intro"].(map[string]any); ok {
			screenshots = strings.TrimSpace(toStringAnyBasic(intro["screenshots"], ""))
		}
	}
	if screenshots == "" {
		return ""
	}

	matches := reHaidanImgTag.FindAllStringSubmatch(screenshots, -1)
	if len(matches) == 0 {
		return strings.TrimSpace(screenshots)
	}

	urls := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		if url := strings.TrimSpace(match[1]); url != "" {
			urls = append(urls, url)
		}
	}
	if len(urls) == 0 {
		return ""
	}
	return strings.Join(urls, "\n")
}

func extractHaidanTitleComponent(raw any, componentKey string) string {
	if raw == nil {
		return ""
	}

	lookup := func(components []any) string {
		for _, item := range components {
			component, ok := item.(map[string]any)
			if !ok {
				if typed, ok := item.(map[string]interface{}); ok {
					component = map[string]any{}
					for k, v := range typed {
						component[k] = v
					}
				} else {
					continue
				}
			}
			if strings.TrimSpace(toStringAnyBasic(component["key"], "")) != componentKey {
				continue
			}
			return strings.TrimSpace(normalizeTitleComponentValue(component["value"]))
		}
		return ""
	}

	switch typed := raw.(type) {
	case []map[string]any:
		components := make([]any, 0, len(typed))
		for _, item := range typed {
			components = append(components, item)
		}
		return lookup(components)
	case []any:
		return lookup(typed)
	case string:
		var parsed []any
		if err := json.Unmarshal([]byte(typed), &parsed); err != nil {
			return ""
		}
		return lookup(parsed)
	case []byte:
		var parsed []any
		if err := json.Unmarshal(typed, &parsed); err != nil {
			return ""
		}
		return lookup(parsed)
	default:
		return ""
	}
}

func normalizeTitleComponentValue(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case []string:
		return strings.TrimSpace(strings.Join(typed, " "))
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(toStringAnyBasic(item, ""))
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, " "))
	default:
		return strings.TrimSpace(toStringAnyBasic(typed, ""))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
