package sites

import (
	"errors"
	stdhtml "html"
	"regexp"
	"strings"

	xhtml "golang.org/x/net/html"
)

var (
	reHHanClubWrappedTitleQuoted = regexp.MustCompile(`(?is)^[^:]+::\s*(?:种子详情|種子詳情|torrent\s*details?)\s*["“](.+?)["”]\s*(?:-|—|–)\s*powered\s+by\s+nexusphp.*$`)
	reHHanClubWrappedTitlePlain  = regexp.MustCompile(`(?is)^[^:]+::\s*(?:种子详情|種子詳情|torrent\s*details?)\s*(.+?)\s*(?:-|—|–)\s*powered\s+by\s+nexusphp.*$`)
	reHHanClubDetailPrefix       = regexp.MustCompile(`(?is)^[^:]+::\s*(?:种子详情|種子詳情|torrent\s*details?)\s*`)
	reHHanClubPoweredSuffix      = regexp.MustCompile(`(?is)\s*(?:-|—|–)\s*powered\s+by\s+nexusphp.*$`)

	reHHanClubSubtitleABy = regexp.MustCompile(`(?i)\s*\|\s*aby\s+\w+.*$`)
	reHHanClubSubtitleBy  = regexp.MustCompile(`(?i)\s*\|\s*by\s+\w+.*$`)
	reHHanClubSubtitleA   = regexp.MustCompile(`(?i)\s*\|\s*a\s+\w+.*$`)
	reHHanClubSubtitleATU = regexp.MustCompile(`(?i)\s*\|\s*atu\s*$`)
	reHHanClubSubtitleDTU = regexp.MustCompile(`(?i)\s*\|\s*dtu\s*$`)
	reHHanClubSubtitlePTE = regexp.MustCompile(`(?i)\s*\|\s*pter\s*$`)

	reHHanClubTeamPrefix  = regexp.MustCompile(`^\[(\w+(?:-\w+)*)\]`)
	reHHanClubTeamSuffix  = regexp.MustCompile(`-([A-Z]+(?:-[A-Z]+)*)$`)
	reHHanClubTeamBracket = regexp.MustCompile(`\[([A-Z]+(?:-[A-Z]+)*)\]$`)
	reHHanClubTeamGeneral = regexp.MustCompile(`\b([A-Z]{2,}(?:-[A-Z]+)*)\b`)
)

// ExtractHHanClub 提取憨憨站点的详情页参数。
// 参数/返回：输入详情页上下文与运行期依赖，返回统一 SeedData。
// 失败场景：公共提取器缺失或公共提取失败。
// 副作用：无。
func ExtractHHanClub(input Input, runtime Runtime) (SeedData, error) {
	if runtime.ExtractWithPublic == nil {
		return SeedData{}, errors.New("缺少公共提取器依赖")
	}
	data, err := runtime.ExtractWithPublic(input)
	if err != nil {
		return data.Normalize(input.FallbackTitle), err
	}

	doc := parseHHanClubHTML(input.PageHTML)
	if doc != nil {
		if title := extractHHanClubSectionText(doc, "标题"); title != "" {
			data.Title = title
		}
		if subtitle := extractHHanClubSectionText(doc, "副标题"); subtitle != "" {
			data.Subtitle = cleanHHanClubSubtitle(subtitle)
		}

		basicInfo := extractHHanClubBasicInfo(doc)
		if team := strings.TrimSpace(firstNonEmpty(basicInfo["制作组"], extractHHanClubTeamFromTitle(data.Title))); team != "" {
			basicInfo["制作组"] = team
		}
		applyHHanClubBasicInfo(&data, basicInfo, runtime)

		if tags := extractHHanClubTags(doc); len(tags) > 0 {
			data.Tags = mergeHHanClubTags(data.Tags, tags)
		}
	}

	data.Title = normalizeHHanClubTitle(data.Title, input.FallbackTitle)
	data.Subtitle = cleanHHanClubSubtitle(data.Subtitle)

	if runtime.ExtractMediaInfoByRegexes != nil {
		patterns := make([]*regexp.Regexp, 0, 2)
		if runtime.ReHHClubMediaInfo != nil {
			patterns = append(patterns, runtime.ReHHClubMediaInfo)
		}
		if runtime.ReMediaInfoCodeMain != nil {
			patterns = append(patterns, runtime.ReMediaInfoCodeMain)
		}
		if len(patterns) > 0 {
			if media := runtime.ExtractMediaInfoByRegexes(input.PageHTML, patterns); media != "" {
				data.MediaInfo = media
			}
		}
	}
	if runtime.BuildSourceParams != nil {
		data.SourceParams = runtime.BuildSourceParams(data)
	}
	return data.Normalize(input.FallbackTitle), nil
}

func parseHHanClubHTML(pageHTML string) *xhtml.Node {
	page := strings.TrimSpace(pageHTML)
	if page == "" {
		return nil
	}
	doc, err := xhtml.Parse(strings.NewReader(page))
	if err != nil || doc == nil {
		return nil
	}
	return doc
}

func normalizeHHanClubTitle(primaryTitle, fallbackTitle string) string {
	primary := cleanHHanClubWrappedTitle(primaryTitle)
	if primary != "" {
		return primary
	}
	return cleanHHanClubWrappedTitle(fallbackTitle)
}

func cleanHHanClubWrappedTitle(raw string) string {
	title := normalizeHHanClubText(raw)
	if title == "" {
		return ""
	}

	lower := strings.ToLower(title)
	if strings.Contains(lower, "种子详情") || strings.Contains(lower, "種子詳情") || strings.Contains(lower, "torrent details") || strings.Contains(lower, "nexusphp") {
		if match := reHHanClubWrappedTitleQuoted.FindStringSubmatch(title); len(match) >= 2 {
			return strings.TrimSpace(strings.Trim(match[1], `"'“”`))
		}
		if match := reHHanClubWrappedTitlePlain.FindStringSubmatch(title); len(match) >= 2 {
			return strings.TrimSpace(strings.Trim(match[1], `"'“”`))
		}

		title = strings.TrimSpace(reHHanClubDetailPrefix.ReplaceAllString(title, ""))
		title = strings.TrimSpace(reHHanClubPoweredSuffix.ReplaceAllString(title, ""))
		title = strings.TrimSpace(strings.Trim(title, `"'“”`))
	}
	return strings.TrimSpace(title)
}

func cleanHHanClubSubtitle(raw string) string {
	subtitle := strings.TrimSpace(raw)
	if subtitle == "" {
		return ""
	}
	subtitle = strings.TrimSpace(reHHanClubSubtitleABy.ReplaceAllString(subtitle, ""))
	subtitle = strings.TrimSpace(reHHanClubSubtitleBy.ReplaceAllString(subtitle, ""))
	subtitle = strings.TrimSpace(reHHanClubSubtitleA.ReplaceAllString(subtitle, ""))
	subtitle = strings.TrimSpace(reHHanClubSubtitleATU.ReplaceAllString(subtitle, ""))
	subtitle = strings.TrimSpace(reHHanClubSubtitleDTU.ReplaceAllString(subtitle, ""))
	subtitle = strings.TrimSpace(reHHanClubSubtitlePTE.ReplaceAllString(subtitle, ""))
	return strings.TrimSpace(subtitle)
}

func extractHHanClubSectionText(doc *xhtml.Node, header string) string {
	container := findHHanClubSectionContainer(doc, header)
	if container == nil {
		return ""
	}
	return normalizeHHanClubText(extractHHanClubVisibleText(container))
}

func findHHanClubSectionContainer(root *xhtml.Node, header string) *xhtml.Node {
	target := normalizeHHanClubText(header)
	if root == nil || target == "" {
		return nil
	}

	var found *xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node == nil || found != nil {
			return
		}
		if node.Type == xhtml.ElementNode && strings.EqualFold(node.Data, "div") {
			if normalizeHHanClubText(extractHHanClubVisibleText(node)) == target {
				if sibling := nextHHanClubSiblingElement(node, "div"); sibling != nil {
					found = sibling
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
			if found != nil {
				return
			}
		}
	}

	walk(root)
	return found
}

func nextHHanClubSiblingElement(node *xhtml.Node, tag string) *xhtml.Node {
	if node == nil {
		return nil
	}
	for sibling := node.NextSibling; sibling != nil; sibling = sibling.NextSibling {
		if sibling.Type != xhtml.ElementNode {
			continue
		}
		if tag == "" || strings.EqualFold(strings.TrimSpace(sibling.Data), strings.TrimSpace(tag)) {
			return sibling
		}
	}
	return nil
}

func extractHHanClubVisibleText(node *xhtml.Node) string {
	if node == nil {
		return ""
	}

	var builder strings.Builder
	var walk func(current *xhtml.Node)
	walk = func(current *xhtml.Node) {
		if current == nil {
			return
		}
		switch current.Type {
		case xhtml.TextNode:
			text := stdhtml.UnescapeString(current.Data)
			text = strings.ReplaceAll(text, "\u00a0", " ")
			if strings.TrimSpace(text) != "" {
				builder.WriteString(text)
				builder.WriteString(" ")
			}
		case xhtml.ElementNode:
			if strings.EqualFold(current.Data, "script") || strings.EqualFold(current.Data, "style") {
				return
			}
			for child := current.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		default:
			for child := current.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
	}

	walk(node)
	return normalizeHHanClubText(builder.String())
}

func extractHHanClubBasicInfo(doc *xhtml.Node) map[string]string {
	info := map[string]string{}
	container := findHHanClubSectionContainer(doc, "基本信息")
	if container == nil {
		return info
	}

	for child := container.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != xhtml.ElementNode || !strings.EqualFold(child.Data, "div") {
			continue
		}
		spans := make([]*xhtml.Node, 0, 4)
		collectHHanClubElementsByTag(child, "span", &spans)
		if len(spans) < 2 {
			continue
		}

		key := strings.TrimSpace(strings.TrimRight(extractHHanClubVisibleText(spans[0]), ":："))
		value := strings.TrimSpace(extractHHanClubVisibleText(spans[1]))
		if key == "" || value == "" {
			continue
		}

		switch {
		case strings.Contains(key, "类型"):
			info["类型"] = value
		case strings.Contains(key, "来源"):
			info["来源"] = value
		case strings.Contains(key, "媒介"):
			info["媒介"] = value
		case strings.Contains(key, "音频编码"):
			info["音频编码"] = value
		case strings.Contains(key, "编码"):
			info["视频编码"] = value
		case strings.Contains(key, "分辨率"):
			info["分辨率"] = value
		case strings.Contains(key, "处理"):
			info["处理"] = value
		case strings.Contains(key, "制作组"):
			info["制作组"] = value
		}
	}
	return info
}

func applyHHanClubBasicInfo(data *SeedData, info map[string]string, runtime Runtime) {
	if data == nil || len(info) == 0 {
		return
	}

	hints := make([]string, 0, 10)
	hints = appendHHanClubNonEmpty(hints, data.Title)

	if rawType := strings.TrimSpace(info["类型"]); rawType != "" {
		hints = appendHHanClubNonEmpty(hints, rawType)
		if mapped := mapHHanClubType(rawType); mapped != "" {
			data.Type = mapped
		}
	}
	if rawMedium := strings.TrimSpace(info["媒介"]); rawMedium != "" {
		hints = appendHHanClubNonEmpty(hints, rawMedium)
		if mapped := mapHHanClubMedium(rawMedium); mapped != "" {
			data.Medium = mapped
		}
	}
	if rawVideo := strings.TrimSpace(info["视频编码"]); rawVideo != "" {
		hints = appendHHanClubNonEmpty(hints, rawVideo)
		if mapped := mapHHanClubVideoCodec(rawVideo); mapped != "" {
			data.VideoCodec = mapped
		}
	}
	if rawAudio := strings.TrimSpace(info["音频编码"]); rawAudio != "" {
		hints = appendHHanClubNonEmpty(hints, rawAudio)
		if mapped := mapHHanClubAudioCodec(rawAudio); mapped != "" {
			data.AudioCodec = mapped
		}
	}
	if rawResolution := strings.TrimSpace(info["分辨率"]); rawResolution != "" {
		hints = appendHHanClubNonEmpty(hints, rawResolution)
		if mapped := mapHHanClubResolution(rawResolution); mapped != "" {
			data.Resolution = mapped
		}
	}

	rawSource := strings.TrimSpace(firstNonEmpty(info["处理"], info["来源"]))
	if rawSource != "" {
		hints = appendHHanClubNonEmpty(hints, rawSource)
		if mapped := mapHHanClubSource(rawSource); mapped != "" {
			data.Source = mapped
		}
	}

	if rawTeam := strings.TrimSpace(info["制作组"]); rawTeam != "" {
		hints = appendHHanClubNonEmpty(hints, rawTeam)
		if mapped := mapHHanClubTeam(rawTeam, runtime); mapped != "" {
			data.Team = mapped
		}
	}

	if runtime.InferStandardizedValues == nil {
		return
	}
	inferred := runtime.InferStandardizedValues(strings.Join(hints, " "), data.MediaInfo, data.Intro.Body)
	data.Type = preferHHanClubValue(data.Type, inferred["type"], "category.movie")
	data.Medium = preferHHanClubValue(data.Medium, inferred["medium"], "medium.other")
	data.VideoCodec = preferHHanClubValue(data.VideoCodec, inferred["video_codec"], "video.other")
	data.AudioCodec = preferHHanClubValue(data.AudioCodec, inferred["audio_codec"], "audio.other")
	data.Resolution = preferHHanClubValue(data.Resolution, inferred["resolution"], "resolution.other")
	data.Team = preferHHanClubValue(data.Team, inferred["team"], "team.other")
	data.Source = preferHHanClubValue(data.Source, inferred["source"], "source.other")
}

func mapHHanClubType(raw string) string {
	value := strings.TrimSpace(raw)
	switch {
	case strings.Contains(value, "电视剧"):
		return "category.tv_series"
	case strings.Contains(value, "动漫"), strings.Contains(value, "动画"):
		return "category.animation"
	case strings.Contains(value, "纪录片"):
		return "category.documentaries"
	case strings.Contains(value, "综艺"):
		return "category.tv_shows"
	case strings.Contains(value, "体育"):
		return "category.sports"
	case strings.Contains(value, "短剧"):
		return "category.playlet"
	case strings.Contains(value, "其他"):
		return "category.other"
	case strings.Contains(value, "电影"):
		return "category.movie"
	default:
		return ""
	}
}

func mapHHanClubMedium(raw string) string {
	upper := strings.ToUpper(strings.TrimSpace(raw))
	compact := strings.NewReplacer(" ", "", "_", "", "-", "").Replace(upper)
	switch {
	case strings.Contains(compact, "UHDREMUX"), strings.Contains(compact, "REMUX"):
		return "medium.remux"
	case strings.Contains(compact, "UHDBLURAY"), strings.Contains(compact, "ULTRAHDBLURAY"):
		return "medium.uhd_bluray"
	case strings.Contains(compact, "FHDBLURAY"), strings.Contains(compact, "BLURAY"):
		return "medium.bluray"
	case strings.Contains(compact, "WEBDL"):
		return "medium.webdl"
	case strings.Contains(compact, "WEBRIP"):
		return "medium.webrip"
	case strings.Contains(compact, "HDTV"):
		return "medium.hdtv"
	case strings.Contains(compact, "DVDR"):
		return "medium.dvdr"
	case strings.Contains(compact, "DVD"):
		return "medium.dvd"
	case strings.Contains(compact, "ENCODE"):
		return "medium.encode"
	case strings.Contains(compact, "TRACK"):
		return "medium.track"
	case compact == "CD", strings.Contains(compact, "CD"):
		return "medium.cd"
	default:
		return ""
	}
}

func mapHHanClubVideoCodec(raw string) string {
	upper := strings.ToUpper(strings.TrimSpace(raw))
	switch {
	case strings.Contains(upper, "AV1"):
		return "video.av1"
	case strings.Contains(upper, "X265"):
		return "video.x265"
	case strings.Contains(upper, "HEVC"), strings.Contains(upper, "H265"), strings.Contains(upper, "H.265"):
		return "video.h265"
	case strings.Contains(upper, "X264"), strings.Contains(upper, "AVC"), strings.Contains(upper, "H264"), strings.Contains(upper, "H.264"):
		return "video.h264"
	case strings.Contains(upper, "VC-1"), strings.Contains(upper, "VC1"):
		return "video.vc1"
	case strings.Contains(upper, "MPEG-2"):
		return "video.mpeg2"
	case strings.Contains(upper, "VP9"), strings.Contains(upper, "VP8"):
		return "video.vp9"
	default:
		return ""
	}
}

func mapHHanClubAudioCodec(raw string) string {
	upper := strings.ToUpper(strings.TrimSpace(raw))
	switch {
	case strings.Contains(upper, "TRUEHD") && strings.Contains(upper, "ATMOS"):
		return "audio.truehd_atmos"
	case strings.Contains(upper, "TRUEHD"):
		return "audio.truehd"
	case strings.Contains(upper, "DTS:X"), strings.Contains(upper, "DTS X"), strings.Contains(upper, "DTS-X"):
		return "audio.dtsx"
	case strings.Contains(upper, "DTS-HD"), strings.Contains(upper, "DTS HD"):
		if strings.Contains(upper, "MA") {
			return "audio.dts_hd_ma"
		}
		if strings.Contains(upper, "HR") {
			return "audio.dts_hd_hr"
		}
		return "audio.dts"
	case strings.Contains(upper, "DTS"):
		return "audio.dts"
	case strings.Contains(upper, "DDP"), strings.Contains(upper, "EAC3"), strings.Contains(upper, "E-AC-3"), strings.Contains(upper, "DD+"):
		return "audio.ddp"
	case strings.Contains(upper, "AC3"), strings.Contains(upper, "AC-3"), strings.Contains(upper, "DD/AC3"):
		return "audio.ac3"
	case strings.Contains(upper, "FLAC"):
		return "audio.flac"
	case strings.Contains(upper, "AAC"):
		return "audio.aac"
	case strings.Contains(upper, "MP3"), strings.Contains(upper, "MP2/3"):
		return "audio.mp3"
	case strings.Contains(upper, "LPCM"), strings.Contains(upper, "PCM"):
		return "audio.lpcm"
	case strings.Contains(upper, "APE"):
		return "audio.ape"
	case strings.Contains(upper, "OGG"):
		return "audio.ogg"
	case strings.Contains(upper, "OPUS"):
		return "audio.opus"
	case strings.Contains(upper, "WAV"):
		return "audio.wav"
	case strings.Contains(upper, "TAA"):
		return "audio.taa"
	case strings.Contains(upper, "ALAC"):
		return "audio.alac"
	default:
		return ""
	}
}

func mapHHanClubResolution(raw string) string {
	upper := strings.ToUpper(strings.TrimSpace(raw))
	switch {
	case strings.Contains(upper, "4320"), strings.Contains(upper, "8K"):
		return "resolution.r4320p"
	case strings.Contains(upper, "2160"), strings.Contains(upper, "4K"):
		return "resolution.r2160p"
	case strings.Contains(upper, "1080I"):
		return "resolution.r1080i"
	case strings.Contains(upper, "1080P"):
		return "resolution.r1080p"
	case strings.Contains(upper, "720P"):
		return "resolution.r720p"
	case strings.Contains(upper, "OTHER"):
		return "resolution.other"
	default:
		return ""
	}
}

func mapHHanClubSource(raw string) string {
	value := strings.TrimSpace(raw)
	switch {
	case strings.Contains(value, "美剧"), strings.Contains(value, "欧美"), strings.Contains(strings.ToUpper(value), "USA"), strings.Contains(strings.ToUpper(value), "US"):
		return "source.western"
	case strings.Contains(value, "日剧"), strings.Contains(value, "日本"), strings.Contains(strings.ToUpper(value), "JPN"):
		return "source.japan"
	case strings.Contains(value, "韩剧"), strings.Contains(value, "韩国"), strings.Contains(strings.ToUpper(value), "KOR"):
		return "source.korea"
	case strings.Contains(value, "港剧"), strings.Contains(value, "香港"), strings.Contains(strings.ToUpper(value), "HKG"):
		return "source.hongkong"
	case strings.Contains(value, "台剧"), strings.Contains(value, "台湾"), strings.Contains(strings.ToUpper(value), "TWN"):
		return "source.taiwan"
	case strings.Contains(value, "大陆剧"), strings.Contains(value, "中国"), strings.Contains(strings.ToUpper(value), "CHN"):
		return "source.china"
	case strings.Contains(value, "英剧"), strings.Contains(value, "英国"), strings.Contains(strings.ToUpper(value), "UK"):
		return "source.uk"
	case strings.Contains(value, "其他"):
		return "source.other"
	default:
		return ""
	}
}

func mapHHanClubTeam(raw string, runtime Runtime) string {
	team := strings.TrimSpace(raw)
	if team == "" {
		return ""
	}
	if runtime.NormalizeTeamKey != nil {
		if normalized := strings.TrimSpace(runtime.NormalizeTeamKey(team)); normalized != "" && normalized != "team.other" {
			return normalized
		}
	}
	if strings.Contains(strings.ToUpper(team), "HHWEB") {
		return "team.hhweb"
	}
	if strings.HasPrefix(strings.ToLower(team), "team.") {
		return strings.ToLower(team)
	}
	return ""
}

func preferHHanClubValue(current, candidate string, defaults ...string) string {
	existing := strings.TrimSpace(current)
	next := strings.TrimSpace(candidate)
	if next == "" {
		return existing
	}
	for _, defaultValue := range defaults {
		if next == strings.TrimSpace(defaultValue) && existing != "" && existing != strings.TrimSpace(defaultValue) {
			return existing
		}
	}
	return next
}

func extractHHanClubTags(doc *xhtml.Node) []string {
	container := findHHanClubSectionContainer(doc, "标签")
	if container == nil {
		return []string{}
	}

	ignore := map[string]struct{}{
		"官方": {},
		"官种": {},
		"首发": {},
		"自购": {},
		"自抓": {},
		"应求": {},
	}
	tags := make([]string, 0, 8)
	spans := make([]*xhtml.Node, 0, 12)
	collectHHanClubElementsByTag(container, "span", &spans)
	for _, span := range spans {
		tag := normalizeHHanClubText(extractHHanClubVisibleText(span))
		if tag == "" {
			continue
		}
		if _, shouldIgnore := ignore[tag]; shouldIgnore {
			continue
		}
		tags = appendUniqueHHanClubString(tags, tag)
	}
	return tags
}

func mergeHHanClubTags(existing, extras []string) []string {
	merged := make([]string, 0, len(existing)+len(extras))
	for _, item := range existing {
		merged = appendUniqueHHanClubString(merged, item)
	}
	for _, item := range extras {
		merged = appendUniqueHHanClubString(merged, item)
	}
	return merged
}

func appendUniqueHHanClubString(items []string, raw string) []string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return items
	}
	for _, existing := range items {
		if strings.EqualFold(strings.TrimSpace(existing), value) {
			return items
		}
	}
	return append(items, value)
}

func extractHHanClubTeamFromTitle(title string) string {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return ""
	}
	for _, pattern := range []*regexp.Regexp{reHHanClubTeamPrefix, reHHanClubTeamSuffix, reHHanClubTeamBracket} {
		if match := pattern.FindStringSubmatch(trimmed); len(match) >= 2 {
			return strings.TrimSpace(match[1])
		}
	}
	if matches := reHHanClubTeamGeneral.FindAllStringSubmatch(trimmed, -1); len(matches) > 0 && len(matches[0]) >= 2 {
		return strings.TrimSpace(matches[0][1])
	}
	return ""
}

func collectHHanClubElementsByTag(node *xhtml.Node, tag string, out *[]*xhtml.Node) {
	if node == nil || out == nil {
		return
	}
	if node.Type == xhtml.ElementNode && strings.EqualFold(node.Data, tag) {
		*out = append(*out, node)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectHHanClubElementsByTag(child, tag, out)
	}
}

func appendHHanClubNonEmpty(values []string, raw string) []string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return values
	}
	return append(values, value)
}

func normalizeHHanClubText(raw string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
}
