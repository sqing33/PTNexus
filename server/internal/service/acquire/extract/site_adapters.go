package extract

import (
	"errors"

	sites "github.com/pt-nexus/server-go/internal/service/acquire/extract/sites"
)

// RuntimeFactory 用于为 sites 包构造运行期依赖（函数注入）。
// 说明：通过函数注入可以避免跨包访问 migrate 包内的私有工具函数。
type RuntimeFactory func(public Extractor) sites.Runtime

// delegatedSiteExtractor 表示站点实现适配器。
// 作用：在 extractors 包与 sites 子目录之间进行输入输出转换与运行期依赖注入。
type delegatedSiteExtractor struct {
	name           string
	public         Extractor
	runtimeFactory RuntimeFactory
	run            func(input sites.Input, runtime sites.Runtime) (sites.SeedData, error)
}

func newDelegatedSiteExtractor(
	name string,
	public Extractor,
	runtimeFactory RuntimeFactory,
	run func(input sites.Input, runtime sites.Runtime) (sites.SeedData, error),
) Extractor {
	return &delegatedSiteExtractor{
		name:           name,
		public:         public,
		runtimeFactory: runtimeFactory,
		run:            run,
	}
}

func (e *delegatedSiteExtractor) Name() string {
	if e == nil || e.name == "" {
		return "special"
	}
	return e.name
}

func (e *delegatedSiteExtractor) Extract(input Input) (SeedData, error) {
	if e == nil || e.run == nil {
		return SeedData{}.NormalizeWithFallback(input.FallbackTitle), errors.New("站点提取器未初始化")
	}

	runtime := sites.Runtime{}
	if e.runtimeFactory != nil {
		runtime = e.runtimeFactory(e.public)
	}

	siteData, err := e.run(toSiteInput(input), runtime)
	data := fromSiteData(siteData).NormalizeWithFallback(input.FallbackTitle)
	if data.SourceParams == nil {
		data.SourceParams = BuildSourceParamsFromExtractedData(data)
	}
	return data, err
}

func NewSSDSpecialExtractor(public Extractor, runtimeFactory RuntimeFactory) Extractor {
	return newDelegatedSiteExtractor("ssd_special", public, runtimeFactory, sites.ExtractSSD)
}

func NewAudiencesSpecialExtractor(public Extractor, runtimeFactory RuntimeFactory) Extractor {
	return newDelegatedSiteExtractor("audiences_special", public, runtimeFactory, sites.ExtractAudiences)
}

func NewHHanClubSpecialExtractor(public Extractor, runtimeFactory RuntimeFactory) Extractor {
	return newDelegatedSiteExtractor("hhanclub_special", public, runtimeFactory, sites.ExtractHHanClub)
}

func NewKeepfrdsSpecialExtractor(public Extractor, runtimeFactory RuntimeFactory) Extractor {
	return newDelegatedSiteExtractor("keepfrds_special", public, runtimeFactory, sites.ExtractKeepfrds)
}

func NewCHDBitsSpecialExtractor(public Extractor, runtimeFactory RuntimeFactory) Extractor {
	return newDelegatedSiteExtractor("chdbits_special", public, runtimeFactory, sites.ExtractCHDBits)
}

func NewHDSkySpecialExtractor(public Extractor, runtimeFactory RuntimeFactory) Extractor {
	return newDelegatedSiteExtractor("hdsky_special", public, runtimeFactory, sites.ExtractHDSky)
}

func NewPTerClubSpecialExtractor(public Extractor, runtimeFactory RuntimeFactory) Extractor {
	return newDelegatedSiteExtractor("pterclub_special", public, runtimeFactory, sites.ExtractPTerClub)
}

func NewHDDolbySpecialExtractor(public Extractor, runtimeFactory RuntimeFactory) Extractor {
	return newDelegatedSiteExtractor("hddolby_special", public, runtimeFactory, sites.ExtractHDDolby)
}

func toSiteInput(input Input) sites.Input {
	return sites.Input{
		BaseURL:       input.BaseURL,
		Cookie:        input.Cookie,
		TorrentID:     input.TorrentID,
		PageHTML:      input.PageHTML,
		FallbackTitle: input.FallbackTitle,
	}
}

func fromSiteData(data sites.SeedData) SeedData {
	normalized := data.Normalize("")
	return SeedData{
		Title:     normalized.Title,
		Subtitle:  normalized.Subtitle,
		MediaInfo: normalized.MediaInfo,
		Intro: IntroData{
			Statement:                normalized.Intro.Statement,
			Poster:                   normalized.Intro.Poster,
			Body:                     normalized.Intro.Body,
			Screenshots:              normalized.Intro.Screenshots,
			RemovedARDTUDeclarations: append([]string{}, normalized.Intro.RemovedARDTUDeclarations...),
		},
		Type:         normalized.Type,
		Medium:       normalized.Medium,
		VideoCodec:   normalized.VideoCodec,
		AudioCodec:   normalized.AudioCodec,
		Resolution:   normalized.Resolution,
		Team:         normalized.Team,
		Source:       normalized.Source,
		Tags:         append([]string{}, normalized.Tags...),
		IMDbLink:     normalized.IMDbLink,
		DoubanLink:   normalized.DoubanLink,
		TMDbLink:     normalized.TMDbLink,
		SourceParams: cloneAnyMap(normalized.SourceParams),
	}
}

func cloneAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	cloned := make(map[string]any, len(src))
	for key, value := range src {
		cloned[key] = value
	}
	return cloned
}
