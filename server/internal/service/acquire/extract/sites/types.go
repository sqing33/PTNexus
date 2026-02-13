package sites

import (
	"regexp"
	"strings"
)

// IntroData 表示详情页简介分段数据。
type IntroData struct {
	Statement                string
	Poster                   string
	Body                     string
	Screenshots              string
	RemovedARDTUDeclarations []string
}

// SeedData 表示站点提取器输出的统一结构。
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

// Input 表示站点提取器输入上下文。
type Input struct {
	BaseURL       string
	Cookie        string
	TorrentID     string
	PageHTML      string
	FallbackTitle string
}

// Runtime 表示站点提取运行期依赖。
// 说明：为避免 sites 包与上层编排包循环依赖，这里通过函数注入复用公共能力。
type Runtime struct {
	ExtractWithPublic func(input Input) (SeedData, error)
	BuildSourceParams func(data SeedData) map[string]any

	ExtractMediaInfoByRegexes    func(pageHTML string, patterns []*regexp.Regexp) string
	ExtractKeepfrdsTitles        func(pageHTML string, fallbackTitle string) (mainTitle string, subtitle string)
	FetchTagsFromTorrentList     func(baseURL string, cookie string, mainTitle string, torrentID string) ([]string, error)
	SanitizeHTMLText             func(input string, keepLineBreak bool) string
	SanitizeHTMLPreText          func(input string, keepLineBreak bool) string
	IsLikelyMediaInfoText        func(text string) bool
	IsLikelyBDInfoText           func(text string) bool
	PickMediaInfoCandidate       func(candidates []string) string
	PickBDInfoCandidate          func(candidates []string) string
	ExtractRegexCandidates       func(pageHTML string, pattern *regexp.Regexp) []string
	ExtractRegexCandidatesAsText func(pageHTML string, pattern *regexp.Regexp) []string
	NormalizeHTMLBlockText       func(raw string) string

	ExtractSubtitle               func(pageHTML string) string
	ExtractElementInnerHTMLByID   func(pageHTML string, tagName string, elementID string) string
	HTMLToBBCode                  func(fragment string) string
	ExtractExtraTextBBCode        func(pageHTML string) string
	ExtractDescriptionSections    func(descrHTML string, descrBBCode string, extraStatementBBCode string) (statement string, poster string, body string, screens string, mediainfo string, tags []string, removed []string)
	BuildStatementFromExtraBBCode func(extraBBCode string) string
	ExtractMediaInfoFromDetail    func(descrHTML string, descrBBCode string) string
	NormalizeExternalLink         func(rawURL string, pattern *regexp.Regexp) string
	InferStandardizedValues       func(title string, mediaInfo string, body string) map[string]string
	ExtractTeamFromPage           func(pageHTML string) string
	NormalizeTeamKey              func(team string) string
	ExtractTagsFromPage           func(pageHTML string) []string
	MergeExplicitSourceTags       func(tags []string) []string
	IsSSDSufficient               func(data SeedData) bool

	ReHHClubMediaInfo   *regexp.Regexp
	ReMediaInfoCodeMain *regexp.Regexp
	ReKeepfrdsMediaInfo *regexp.Regexp
	ReDoubanLink        *regexp.Regexp
	ReIMDbLink          *regexp.Regexp
	ReTMDbLink          *regexp.Regexp
}

// Normalize 统一裁剪字符串并确保容器字段不为 nil。
func (d SeedData) Normalize(fallbackTitle string) SeedData {
	d.Title = strings.TrimSpace(firstNonEmpty(d.Title, fallbackTitle))
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
