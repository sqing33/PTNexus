package extract

import "strings"

// IntroData 表示从详情页提取出的简介分段数据。
type IntroData struct {
	Statement                string
	Poster                   string
	Body                     string
	Screenshots              string
	RemovedARDTUDeclarations []string
}

// SeedData 表示提取器输出的统一结构。
// 该结构仅承载“页面提取阶段”的数据，不负责修复/补全/写库。
type SeedData struct {
	Title        string
	Subtitle     string
	Intro        IntroData
	MediaInfo    string
	SourceParams map[string]any

	Type       string
	Medium     string
	VideoCodec string
	AudioCodec string
	Resolution string
	Team       string
	Source     string
	Tags       []string

	IMDbLink   string
	DoubanLink string
	TMDbLink   string
}

// Meta 描述提取器路由与回退信息，便于日志追踪。
type Meta struct {
	ExtractorName  string
	UsedFallback   bool
	FallbackReason string
}

// Input 表示提取器执行上下文。
// 与 Python 版一致，特殊提取器可按需使用 base_url/cookie/torrent_id 等信息。
type Input struct {
	SiteCode      string
	SiteNickname  string
	BaseURL       string
	Cookie        string
	TorrentID     string
	PageHTML      string
	FallbackTitle string
}

// Extractor 定义站点参数提取器统一接口。
// 约束：只做详情页参数提取，不做后续修复/映射/写库。
type Extractor interface {
	Name() string
	Extract(input Input) (SeedData, error)
}

// NormalizeWithFallback 统一裁剪值并补齐默认容器。
func (d SeedData) NormalizeWithFallback(fallbackTitle string) SeedData {
	d.Title = strings.TrimSpace(firstNonEmptySeedValue(d.Title, fallbackTitle))
	d.Subtitle = strings.TrimSpace(d.Subtitle)
	d.MediaInfo = strings.TrimSpace(d.MediaInfo)
	d.Type = strings.TrimSpace(d.Type)
	d.Medium = strings.TrimSpace(d.Medium)
	d.VideoCodec = strings.TrimSpace(d.VideoCodec)
	d.AudioCodec = strings.TrimSpace(d.AudioCodec)
	d.Resolution = strings.TrimSpace(d.Resolution)
	d.Team = strings.TrimSpace(d.Team)
	d.Source = strings.TrimSpace(d.Source)
	d.IMDbLink = strings.TrimSpace(d.IMDbLink)
	d.DoubanLink = strings.TrimSpace(d.DoubanLink)
	d.TMDbLink = strings.TrimSpace(d.TMDbLink)
	d.Intro.Statement = strings.TrimSpace(d.Intro.Statement)
	d.Intro.Poster = strings.TrimSpace(d.Intro.Poster)
	d.Intro.Body = strings.TrimSpace(d.Intro.Body)
	d.Intro.Screenshots = strings.TrimSpace(d.Intro.Screenshots)

	if d.SourceParams == nil {
		d.SourceParams = map[string]any{}
	}
	if d.Tags == nil {
		d.Tags = []string{}
	}
	if d.Intro.RemovedARDTUDeclarations == nil {
		d.Intro.RemovedARDTUDeclarations = []string{}
	}
	return d
}

// IsMeaningful 判断提取结果是否有有效字段，供回退逻辑使用。
func (d SeedData) IsMeaningful() bool {
	if strings.TrimSpace(d.Title) != "" {
		return true
	}
	if strings.TrimSpace(d.Subtitle) != "" || strings.TrimSpace(d.MediaInfo) != "" {
		return true
	}
	if strings.TrimSpace(d.Intro.Statement) != "" || strings.TrimSpace(d.Intro.Poster) != "" || strings.TrimSpace(d.Intro.Body) != "" || strings.TrimSpace(d.Intro.Screenshots) != "" {
		return true
	}
	if strings.TrimSpace(d.IMDbLink) != "" || strings.TrimSpace(d.DoubanLink) != "" || strings.TrimSpace(d.TMDbLink) != "" {
		return true
	}
	return len(d.Tags) > 0
}

// BuildSourceParamsFromExtractedData 从统一结果构建 source_params 字段。
func BuildSourceParamsFromExtractedData(data SeedData) map[string]any {
	params := map[string]any{
		"类型":   strings.TrimSpace(data.Type),
		"媒介":   strings.TrimSpace(data.Medium),
		"视频编码": strings.TrimSpace(data.VideoCodec),
		"音频编码": strings.TrimSpace(data.AudioCodec),
		"分辨率":  strings.TrimSpace(data.Resolution),
		"制作组":  strings.TrimSpace(data.Team),
		"标签":   append([]string{}, data.Tags...),
		"产地":   strings.TrimSpace(data.Source),
	}
	return params
}

func firstNonEmptySeedValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
