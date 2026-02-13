package persist

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	extract "github.com/pt-nexus/server-go/internal/service/acquire/extract"
	processingmedia "github.com/pt-nexus/server-go/internal/service/processing/media"
)

func runeLen(text string) int {
	return utf8.RuneCountInString(text)
}

func compactHead(text string, limit int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if limit <= 0 {
		limit = 200
	}
	// 统一压缩空白，避免日志换行炸裂。
	trimmed = strings.ReplaceAll(trimmed, "\r\n", "\n")
	trimmed = strings.ReplaceAll(trimmed, "\r", "\n")
	trimmed = strings.Join(strings.Fields(trimmed), " ")
	r := []rune(trimmed)
	if len(r) <= limit {
		return trimmed
	}
	return string(r[:limit]) + "..."
}

func sampleStrings(values []string, limit int) []string {
	if limit <= 0 {
		limit = 12
	}
	out := make([]string, 0, limit)
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		out = append(out, v)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func parseJSONStringArrayUnsafe(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || !strings.HasPrefix(trimmed, "[") {
		return []string{}
	}
	out := []string{}
	_ = json.Unmarshal([]byte(trimmed), &out)
	return out
}

func parseJSONAnyArrayUnsafe(value string) []any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || !strings.HasPrefix(trimmed, "[") {
		return []any{}
	}
	out := []any{}
	_ = json.Unmarshal([]byte(trimmed), &out)
	return out
}

func findTitleComponentValue(components []any, key string) string {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" || len(components) == 0 {
		return ""
	}
	for _, item := range components {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		k := strings.TrimSpace(toStringAny(m["key"], ""))
		if k != trimmedKey {
			continue
		}
		return strings.TrimSpace(toStringAny(m["value"], ""))
	}
	return ""
}

func appendKVLine(builder *strings.Builder, key string, value string, indent int) {
	if builder == nil {
		return
	}
	if indent < 0 {
		indent = 0
	}
	if strings.TrimSpace(key) == "" {
		key = "字段"
	}
	prefix := strings.Repeat("  ", indent)
	builder.WriteString(prefix)
	builder.WriteString(key)
	builder.WriteString("\t= ")
	builder.WriteString(strings.TrimSpace(value))
	builder.WriteString("\n")
}

func appendListLine(builder *strings.Builder, key string, values []string, indent int, perLine int) {
	if builder == nil {
		return
	}
	if perLine <= 0 {
		perLine = 8
	}
	appendKVLine(builder, key+"(数量)", fmt.Sprintf("%d", len(values)), indent)
	sample := sampleStrings(values, 64)
	if len(sample) == 0 {
		appendKVLine(builder, key+"(样例)", "[]", indent)
		return
	}
	prefix := strings.Repeat("  ", indent)
	builder.WriteString(prefix)
	builder.WriteString(key)
	builder.WriteString("(样例)\t= [")
	for i, item := range sample {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(item)
		if (i+1) < len(sample) && (i+1)%perLine == 0 {
			builder.WriteString("\n")
			builder.WriteString(prefix)
			builder.WriteString("  \t  ")
		}
	}
	builder.WriteString("]\n")
}

func appendLongText(builder *strings.Builder, key string, text string, indent int, headLimit int) {
	if builder == nil {
		return
	}
	appendKVLine(builder, key+"(长度)", fmt.Sprintf("%d", runeLen(text)), indent)
	appendKVLine(builder, key+"(head)", compactHead(text, headLimit), indent)
}

// buildExtractSnapshotLog 构造“提取快照”日志文本，便于对比提取结果与后续处理差异。
// 参数/返回：入参为抓取上下文、提取器元信息与提取结果；返回多行日志字符串。
// 失败场景：无（内部按空值兜底）。
// 副作用：无。
func buildExtractSnapshotLog(
	taskID string,
	sourceSite string,
	torrentID string,
	siteIdentifier string,
	extractMeta extract.Meta,
	seed extract.SeedData,
	review extract.ReviewExtractedData,
) string {
	var builder strings.Builder

	builder.WriteString("上下文:\n")
	appendKVLine(&builder, "任务ID", strings.TrimSpace(taskID), 1)
	appendKVLine(&builder, "源站点", strings.TrimSpace(sourceSite), 1)
	appendKVLine(&builder, "站点", strings.TrimSpace(siteIdentifier), 1)
	appendKVLine(&builder, "种子ID", strings.TrimSpace(torrentID), 1)
	appendKVLine(&builder, "提取器", strings.TrimSpace(extractMeta.ExtractorName), 1)
	appendKVLine(&builder, "回退", fmt.Sprintf("%t", extractMeta.UsedFallback), 1)
	appendKVLine(&builder, "回退原因", compactHead(extractMeta.FallbackReason, 120), 1)

	appendSeed := func(title string, data extract.SeedData) {
		builder.WriteString("\n")
		builder.WriteString(title)
		builder.WriteString(":\n")

		appendKVLine(&builder, "标题", compactHead(data.Title, 200), 1)
		appendKVLine(&builder, "副标题", compactHead(data.Subtitle, 200), 1)
		appendKVLine(&builder, "类型", strings.TrimSpace(data.Type), 1)
		appendKVLine(&builder, "媒介", strings.TrimSpace(data.Medium), 1)
		appendKVLine(&builder, "视频编码", strings.TrimSpace(data.VideoCodec), 1)
		appendKVLine(&builder, "音频编码", strings.TrimSpace(data.AudioCodec), 1)
		appendKVLine(&builder, "分辨率", strings.TrimSpace(data.Resolution), 1)
		appendKVLine(&builder, "制作组", strings.TrimSpace(data.Team), 1)
		appendKVLine(&builder, "产地", strings.TrimSpace(data.Source), 1)
		appendKVLine(&builder, "IMDb", compactHead(data.IMDbLink, 160), 1)
		appendKVLine(&builder, "豆瓣", compactHead(data.DoubanLink, 160), 1)
		appendKVLine(&builder, "TMDb", compactHead(data.TMDbLink, 160), 1)

		mediumSP := ""
		audioSP := ""
		if data.SourceParams != nil {
			mediumSP = strings.TrimSpace(toStringAny(data.SourceParams["媒介"], ""))
			audioSP = strings.TrimSpace(toStringAny(data.SourceParams["音频编码"], ""))
		}
		appendKVLine(&builder, "SourceParams(媒介)", mediumSP, 1)
		appendKVLine(&builder, "SourceParams(音频)", audioSP, 1)

		appendListLine(&builder, "标签", data.Tags, 1, 6)

		builder.WriteString("  简介:\n")
		appendKVLine(&builder, "海报", compactHead(data.Intro.Poster, 200), 2)
		appendLongText(&builder, "声明", data.Intro.Statement, 2, 240)
		appendLongText(&builder, "正文", data.Intro.Body, 2, 240)
		appendLongText(&builder, "截图", data.Intro.Screenshots, 2, 240)

		isMI, isBD, reason := processingmedia.ValidateMediaInfoFormat(data.MediaInfo)
		kind := "Invalid"
		if isMI {
			kind = "MediaInfo"
		} else if isBD {
			kind = "BDInfo"
		}
		builder.WriteString("  MediaInfo:\n")
		appendKVLine(&builder, "类型", kind, 2)
		appendLongText(&builder, "内容", data.MediaInfo, 2, 240)
		appendKVLine(&builder, "校验(is_mediainfo)", fmt.Sprintf("%t", isMI), 2)
		appendKVLine(&builder, "校验(is_bdinfo)", fmt.Sprintf("%t", isBD), 2)
		appendKVLine(&builder, "校验原因", compactHead(reason, 120), 2)
	}

	appendSeed("提取结果(SeedData)", seed)

	bridge := extract.SeedData{
		Title:        review.Title,
		Subtitle:     review.Subtitle,
		Intro:        extract.IntroData{Statement: review.Statement, Poster: review.Poster, Body: review.Body, Screenshots: review.Screens},
		MediaInfo:    review.Mediainfo,
		Type:         review.Type,
		Medium:       review.Medium,
		VideoCodec:   review.VideoCodec,
		AudioCodec:   review.AudioCodec,
		Resolution:   review.Resolution,
		Team:         review.Team,
		Source:       review.Source,
		Tags:         append([]string{}, review.Tags...),
		IMDbLink:     review.IMDbLink,
		DoubanLink:   review.DoubanLink,
		TMDbLink:     review.TMDbLink,
		SourceParams: map[string]any{},
	}.NormalizeWithFallback("")
	appendSeed("提取结果(ReviewData)", bridge)

	return strings.TrimRight(builder.String(), "\n")
}

// buildPersistSnapshotLog 构造“入库快照”日志文本，输出最终写入 seed_parameters 的关键字段。
// 参数/返回：入参为抓取上下文与最终 record；返回多行日志字符串。
// 失败场景：无（内部按空值兜底）。
// 副作用：无。
func buildPersistSnapshotLog(taskID string, siteIdentifier string, hash string, torrentID string, record map[string]any) string {
	var builder strings.Builder

	title := strings.TrimSpace(toStringAny(record["title"], ""))
	body := strings.TrimSpace(toStringAny(record["body"], ""))
	mediainfoText := strings.TrimSpace(toStringAny(record["mediainfo"], ""))
	isMI, isBD, reason := processingmedia.ValidateMediaInfoFormat(mediainfoText)
	kind := "Invalid"
	if isMI {
		kind = "MediaInfo"
	} else if isBD {
		kind = "BDInfo"
	}

	builder.WriteString("上下文:\n")
	appendKVLine(&builder, "任务ID", strings.TrimSpace(taskID), 1)
	appendKVLine(&builder, "站点", strings.TrimSpace(siteIdentifier), 1)
	appendKVLine(&builder, "种子ID", strings.TrimSpace(torrentID), 1)
	appendKVLine(&builder, "Hash", strings.TrimSpace(hash), 1)
	appendKVLine(&builder, "MediaInfo状态", strings.TrimSpace(toStringAny(record["mediainfo_status"], "")), 1)

	builder.WriteString("\n入库参数(seed_parameters):\n")
	appendKVLine(&builder, "标题", compactHead(title, 200), 1)
	appendKVLine(&builder, "副标题", compactHead(toStringAny(record["subtitle"], ""), 200), 1)
	appendKVLine(&builder, "类型", strings.TrimSpace(toStringAny(record["type"], "")), 1)
	appendKVLine(&builder, "媒介", strings.TrimSpace(toStringAny(record["medium"], "")), 1)
	appendKVLine(&builder, "视频编码", strings.TrimSpace(toStringAny(record["video_codec"], "")), 1)
	appendKVLine(&builder, "音频编码", strings.TrimSpace(toStringAny(record["audio_codec"], "")), 1)
	appendKVLine(&builder, "分辨率", strings.TrimSpace(toStringAny(record["resolution"], "")), 1)
	appendKVLine(&builder, "制作组", strings.TrimSpace(toStringAny(record["team"], "")), 1)
	appendKVLine(&builder, "产地", strings.TrimSpace(toStringAny(record["source"], "")), 1)
	appendKVLine(&builder, "IMDb", compactHead(toStringAny(record["imdb_link"], ""), 160), 1)
	appendKVLine(&builder, "豆瓣", compactHead(toStringAny(record["douban_link"], ""), 160), 1)
	appendKVLine(&builder, "TMDb", compactHead(toStringAny(record["tmdb_link"], ""), 160), 1)

	tagsJSON := strings.TrimSpace(toStringAny(record["tags"], ""))
	tags := parseJSONStringArrayUnsafe(tagsJSON)
	appendListLine(&builder, "标签", tags, 1, 6)

	componentsJSON := strings.TrimSpace(toStringAny(record["title_components"], ""))
	components := parseJSONAnyArrayUnsafe(componentsJSON)
	appendKVLine(&builder, "标题组件(数量)", fmt.Sprintf("%d", len(components)), 1)
	appendKVLine(&builder, "标题组件(音频编码)", compactHead(findTitleComponentValue(components, "音频编码"), 160), 1)

	inferred := extract.InferStandardizedValues(title, mediainfoText, body)
	appendKVLine(&builder, "推断音频编码", strings.TrimSpace(inferred["audio_codec"]), 1)

	builder.WriteString("\n简介:\n")
	appendKVLine(&builder, "海报", compactHead(toStringAny(record["poster"], ""), 200), 1)
	appendLongText(&builder, "声明", toStringAny(record["statement"], ""), 1, 240)
	appendLongText(&builder, "正文", body, 1, 240)
	appendLongText(&builder, "截图", toStringAny(record["screenshots"], ""), 1, 240)

	builder.WriteString("\nMediaInfo:\n")
	appendKVLine(&builder, "类型", kind, 1)
	appendLongText(&builder, "内容", mediainfoText, 1, 240)
	appendKVLine(&builder, "校验(is_mediainfo)", fmt.Sprintf("%t", isMI), 1)
	appendKVLine(&builder, "校验(is_bdinfo)", fmt.Sprintf("%t", isBD), 1)
	appendKVLine(&builder, "校验原因", compactHead(reason, 120), 1)

	return strings.TrimRight(builder.String(), "\n")
}
