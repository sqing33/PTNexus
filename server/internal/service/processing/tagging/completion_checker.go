package tagging

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/pt-nexus/server-go/internal/service/downloaderclient"
)

// CompletionStatus 表示完结判定结果。
type CompletionStatus struct {
	IsComplete    bool
	Confidence    string
	Reason        string
	TotalEpisodes *int
	LocalEpisodes *int
}

// CompletionCheckContext 表示完结判定所需的下载器上下文。
type CompletionCheckContext struct {
	SavePath     string
	TorrentName  string
	ContentName  string
	DownloaderID string
	RootConfig   map[string]any
}

// CheckCompletionStatus 综合判断电视剧/动漫是否完结。
// 参数/返回：title/subtitle/description 为抓取到的文本；localPath/torrentName 用于定位本地文件目录并统计集数；返回包含置信度与原因。
// 失败场景：本地路径不可用或无法统计集数时，不会阻塞主流程，只会降低置信度。
// 副作用：可能遍历本地文件系统统计 SxxExx 格式的集数。
func CheckCompletionStatus(title string, subtitle string, description string, localPath string, torrentName string) CompletionStatus {
	return CheckCompletionStatusWithDownloaderContext(
		title,
		subtitle,
		description,
		CompletionCheckContext{
			SavePath:    localPath,
			TorrentName: torrentName,
			ContentName: torrentName,
		},
	)
}

// CheckCompletionStatusWithDownloaderContext 综合判断电视剧/动漫是否完结（支持 downloader 本地/代理上下文）。
// 参数/返回：context 包含 save_path/downloader_id/root_config 等路径上下文；返回包含置信度与原因。
// 失败场景：代理不可用、路径映射失败或本地不可达时，不会阻塞主流程，只会降低置信度。
// 副作用：可能访问盒子代理接口与本地文件系统统计剧集。
func CheckCompletionStatusWithDownloaderContext(title string, subtitle string, description string, context CompletionCheckContext) CompletionStatus {
	result := CompletionStatus{
		IsComplete: false,
		Confidence: "low",
		Reason:     "未检测到完结标识",
	}

	reasons := make([]string, 0, 3)

	if keyword := matchCompletionKeywordInSubtitle(subtitle); keyword != "" {
		result.IsComplete = true
		result.Confidence = "high"
		reasons = append(reasons, "副标题包含完结关键词: "+keyword)
	}

	if token := extractCompletionStatusFromTitleLocal(title); token != "" {
		result.IsComplete = true
		result.Confidence = "high"
		reasons = append(reasons, "主标题包含Complete标识")
	}

	total := extractTotalEpisodesFromDescription(description)
	if total != nil {
		result.TotalEpisodes = total
	}

	contentName := strings.TrimSpace(context.ContentName)
	if contentName == "" {
		contentName = strings.TrimSpace(context.TorrentName)
	}
	episodeResult := downloaderclient.CountEpisodesWithDownloaderContext(downloaderclient.EpisodeCountInput{
		RootConfig:   context.RootConfig,
		DownloaderID: strings.TrimSpace(context.DownloaderID),
		SavePath:     strings.TrimSpace(context.SavePath),
		TorrentName:  strings.TrimSpace(context.TorrentName),
		ContentName:  contentName,
	})
	if episodeResult.Available {
		localCount := episodeResult.EpisodeCount
		result.LocalEpisodes = &localCount
	}

	if total != nil && result.LocalEpisodes != nil {
		if *result.LocalEpisodes >= *total {
			result.IsComplete = true
			if result.Confidence == "low" {
				result.Confidence = "medium"
			}
			reasons = append(reasons, "本地集数达到或超过简介总集数(来源:"+completionCountSourceText(episodeResult.Source)+")")
		} else {
			reasons = append(reasons, "本地集数少于简介总集数(来源:"+completionCountSourceText(episodeResult.Source)+")")
		}
	}

	if len(reasons) > 0 {
		result.Reason = strings.Join(reasons, "; ")
	}
	return result
}

func completionCountSourceText(source string) string {
	switch strings.TrimSpace(source) {
	case "proxy":
		return "代理"
	case "local":
		return "本地"
	default:
		return "未知"
	}
}

func matchCompletionKeywordInSubtitle(subtitle string) string {
	text := strings.TrimSpace(subtitle)
	if text == "" {
		return ""
	}
	patterns := []struct {
		re       *regexp.Regexp
		template string
	}{
		{regexp.MustCompile(`(?i)全\s*(\d+)\s*集`), "全%s集"},
		{regexp.MustCompile(`(?i)全\s*(\d+)\s*期`), "全%s期"},
		{regexp.MustCompile(`(?i)全\s*(\d+)\s*话`), "全%s话"},
		{regexp.MustCompile(`(?i)完结`), "完结"},
		{regexp.MustCompile(`(?i)\bCOMPLETE\b`), "COMPLETE"},
		{regexp.MustCompile(`(?i)\bComplete\b`), "Complete"},
	}
	for _, p := range patterns {
		m := p.re.FindStringSubmatch(text)
		if len(m) == 0 {
			continue
		}
		if strings.Contains(p.template, "%s") && len(m) >= 2 && strings.TrimSpace(m[1]) != "" {
			return strings.ReplaceAll(p.template, "%s", strings.TrimSpace(m[1]))
		}
		return p.template
	}
	return ""
}

func extractTotalEpisodesFromDescription(description string) *int {
	text := strings.TrimSpace(description)
	if text == "" {
		return nil
	}
	// Go 的 `\s` 不包含全角空格，这里显式补充 `　`（U+3000）。
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)[◎❁][\s　]*集[\s　]*数[\s　]*[:：]?[\s　]*(\d+)`),
		regexp.MustCompile(`(?i)集[\s　]*数[\s　]*[:：][\s　]*(\d+)`),
		regexp.MustCompile(`(?i)Episodes?[\s　]*[:：][\s　]*(\d+)`),
		regexp.MustCompile(`(?i)Total[\s　]+Episodes?[\s　]*[:：][\s　]*(\d+)`),
	}
	for _, re := range patterns {
		m := re.FindStringSubmatch(text)
		if len(m) < 2 {
			continue
		}
		value, err := strconv.Atoi(strings.TrimSpace(m[1]))
		if err != nil || value <= 0 {
			continue
		}
		return &value
	}
	return nil
}

// ShouldAddCompletionTag 根据完结状态与现有标签决定是否补充 tag.完结。
// 对齐 Python：仅当置信度为 high/medium 且当前未包含完结标签时返回 true。
func ShouldAddCompletionTag(existingTags []string, status CompletionStatus) bool {
	if !status.IsComplete {
		return false
	}
	if status.Confidence != "high" && status.Confidence != "medium" {
		return false
	}

	for _, tag := range existingTags {
		switch strings.TrimSpace(tag) {
		case "完结", "tag.完结", "Complete", "tag.Complete", "COMPLETE", "tag.COMPLETE":
			return false
		}
	}
	return true
}

func extractCompletionStatusFromTitleLocal(title string) string {
	if strings.TrimSpace(title) == "" {
		return ""
	}
	re := regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}_])(Complete)(?:$|[^\p{L}\p{N}_])`)
	match := re.FindStringSubmatch(title)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}
