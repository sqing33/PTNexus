package extract

import (
	sites "github.com/pt-nexus/server-go/internal/service/acquire/extract/sites"
)

// ReviewExtractResult 表示“提取器输出 -> ReviewExtractedData”的桥接结果。
type ReviewExtractResult struct {
	SeedData    SeedData
	ReviewData  ReviewExtractedData
	ExtractMeta Meta
}

// NewPageExtractorEngine 构建“公共提取器 + 站点特殊提取器”的统一路由引擎。
// 参数/返回：无入参，返回可直接执行 Extract 的引擎实例。
// 失败场景：内部会对空提取器做兜底，不返回错误。
// 副作用：无外部副作用，仅初始化函数路由表。
func NewPageExtractorEngine() *Engine {
	publicExtractor := NewPublicExtractor(defaultPublicExtract)
	return NewDefaultEngine(publicExtractor, buildSiteRuntimeForExtractors)
}

// ExtractWithEngine 使用指定引擎执行提取；若引擎为空则自动创建默认引擎。
// 参数/返回：input 为站点页面上下文；返回统一 SeedData 与提取元信息。
// 失败场景：提取失败时由引擎内部回退并在 Meta 中标识，不直接返回 error。
// 副作用：可能执行页面解析逻辑，但不写数据库。
func ExtractWithEngine(engine *Engine, input Input) (SeedData, Meta) {
	if engine == nil {
		engine = NewPageExtractorEngine()
	}
	return engine.Extract(input)
}

// ExtractReviewData 执行提取器并返回迁移流程需要的 ReviewExtractedData。
// 参数/返回：engine 可为空；input 为站点页面上下文；返回包含 reviewData 与提取元信息。
// 失败场景：提取失败由引擎内部兜底，错误会体现在 ExtractMeta 中，不直接返回 error。
// 副作用：可能执行页面解析逻辑，但不写数据库。
func ExtractReviewData(engine *Engine, input Input) ReviewExtractResult {
	data, meta := ExtractWithEngine(engine, input)
	return ReviewExtractResult{
		SeedData:    data,
		ReviewData:  ExtractedDataToReviewData(data),
		ExtractMeta: meta,
	}
}

// ExtractedDataToReviewData 将统一提取结果转换为迁移流程使用的 ReviewExtractedData。
// 参数/返回：data 为提取器输出；返回用于后续修复/入库的结构化数据。
// 失败场景：输入为空时返回字段为空的结构体。
// 副作用：无副作用。
func ExtractedDataToReviewData(data SeedData) ReviewExtractedData {
	normalized := data.NormalizeWithFallback("")
	return ReviewExtractedData{
		Title:                    normalized.Title,
		Subtitle:                 normalized.Subtitle,
		Statement:                normalized.Intro.Statement,
		Poster:                   normalized.Intro.Poster,
		Body:                     normalized.Intro.Body,
		Screens:                  normalized.Intro.Screenshots,
		Mediainfo:                normalized.MediaInfo,
		RemovedARDTUDeclarations: append([]string{}, normalized.Intro.RemovedARDTUDeclarations...),
		Tags:                     append([]string{}, normalized.Tags...),
		Type:                     normalized.Type,
		Medium:                   normalized.Medium,
		VideoCodec:               normalized.VideoCodec,
		AudioCodec:               normalized.AudioCodec,
		Resolution:               normalized.Resolution,
		Team:                     normalized.Team,
		Source:                   normalized.Source,
		IMDbLink:                 normalized.IMDbLink,
		DoubanLink:               normalized.DoubanLink,
		TMDbLink:                 normalized.TMDbLink,
	}
}

// IsSSDSufficient 判断 SSD 特殊提取结果是否足够有效，避免误命中后阻断回退公共提取器。
func IsSSDSufficient(data SeedData) bool {
	normalized := data.NormalizeWithFallback("")
	if normalized.Title == "" {
		return false
	}
	if normalized.MediaInfo != "" {
		return true
	}
	if normalized.Intro.Statement != "" || normalized.Intro.Poster != "" || normalized.Intro.Body != "" || normalized.Intro.Screenshots != "" {
		return true
	}
	if normalized.IMDbLink != "" || normalized.DoubanLink != "" || normalized.TMDbLink != "" {
		return true
	}
	if normalized.Subtitle != "" {
		return true
	}
	return len(normalized.Tags) > 0
}

func defaultPublicExtract(input Input) (SeedData, error) {
	review := ExtractReviewDataFromHTML(input.PageHTML, input.FallbackTitle)
	data := reviewDataToExtractorData(review)
	data.SourceParams = BuildSourceParamsFromExtractedData(data)
	return data.NormalizeWithFallback(input.FallbackTitle), nil
}

func buildSiteRuntimeForExtractors(public Extractor) sites.Runtime {
	return sites.Runtime{
		ExtractWithPublic: func(input sites.Input) (sites.SeedData, error) {
			extractor := public
			if extractor == nil {
				extractor = NewPublicExtractor(defaultPublicExtract)
			}
			data, err := extractor.Extract(Input{
				BaseURL:       input.BaseURL,
				Cookie:        input.Cookie,
				TorrentID:     input.TorrentID,
				PageHTML:      input.PageHTML,
				FallbackTitle: input.FallbackTitle,
			})
			return toSiteSeedDataRuntime(data), err
		},
		BuildSourceParams: func(data sites.SeedData) map[string]any {
			return BuildSourceParamsFromExtractedData(fromSiteData(data))
		},
		ExtractMediaInfoByRegexes:    ExtractMediaInfoByRegexes,
		ExtractKeepfrdsTitles:        ExtractKeepfrdsTitles,
		FetchTagsFromTorrentList:     FetchTagsFromTorrentList,
		SanitizeHTMLText:             SanitizeHTMLText,
		SanitizeHTMLPreText:          sanitizeHTMLPreText,
		IsLikelyMediaInfoText:        IsLikelyMediaInfoText,
		IsLikelyBDInfoText:           IsLikelyBDInfoText,
		PickMediaInfoCandidate:       PickMediaInfoCandidate,
		PickBDInfoCandidate:          PickBDInfoCandidate,
		ExtractRegexCandidates:       ExtractRegexCandidates,
		ExtractRegexCandidatesAsText: ExtractRegexCandidatesAsText,
		NormalizeHTMLBlockText:       NormalizeHTMLBlockText,

		ExtractSubtitle:               ExtractSubtitle,
		ExtractElementInnerHTMLByID:   ExtractElementInnerHTMLByID,
		HTMLToBBCode:                  HTMLToBBCode,
		ExtractExtraTextBBCode:        ExtractExtraTextBBCode,
		ExtractDescriptionSections:    ExtractDescriptionSections,
		BuildStatementFromExtraBBCode: BuildStatementFromExtraBBCode,
		ExtractMediaInfoFromDetail:    ExtractMediaInfoFromDetail,
		NormalizeExternalLink:         NormalizeExternalLink,
		InferStandardizedValues:       InferStandardizedValues,
		ExtractTeamFromPage:           ExtractTeamFromPage,
		NormalizeTeamKey:              NormalizeTeamKey,
		ExtractTagsFromPage:           ExtractTagsFromPage,
		MergeExplicitSourceTags:       MergeExplicitSourceTags,
		IsSSDSufficient: func(data sites.SeedData) bool {
			return IsSSDSufficient(fromSiteData(data))
		},

		ReHHClubMediaInfo:   ReHHClubMediaInfo(),
		ReMediaInfoCodeMain: ReMediaInfoCodeMain(),
		ReKeepfrdsMediaInfo: ReKeepfrdsMediaInfo(),
		ReDoubanLink:        ReDoubanLink(),
		ReIMDbLink:          ReIMDbLink(),
		ReTMDbLink:          ReTMDbLink(),
	}
}

func toSiteSeedDataRuntime(data SeedData) sites.SeedData {
	normalized := data.NormalizeWithFallback("")
	return sites.SeedData{
		Title:     normalized.Title,
		Subtitle:  normalized.Subtitle,
		MediaInfo: normalized.MediaInfo,
		Intro: sites.IntroData{
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

func reviewDataToExtractorData(review ReviewExtractedData) SeedData {
	data := SeedData{
		Title:     review.Title,
		Subtitle:  review.Subtitle,
		MediaInfo: review.Mediainfo,
		Intro: IntroData{
			Statement:                review.Statement,
			Poster:                   review.Poster,
			Body:                     review.Body,
			Screenshots:              review.Screens,
			RemovedARDTUDeclarations: append([]string{}, review.RemovedARDTUDeclarations...),
		},
		Type:       review.Type,
		Medium:     review.Medium,
		VideoCodec: review.VideoCodec,
		AudioCodec: review.AudioCodec,
		Resolution: review.Resolution,
		Team:       review.Team,
		Source:     review.Source,
		Tags:       append([]string{}, review.Tags...),
		IMDbLink:   review.IMDbLink,
		DoubanLink: review.DoubanLink,
		TMDbLink:   review.TMDbLink,
	}
	data.SourceParams = BuildSourceParamsFromExtractedData(data)
	return data.NormalizeWithFallback("")
}
