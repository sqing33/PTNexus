package persist

import (
	"encoding/json"
	"strings"

	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
	processingtagging "github.com/pt-nexus/server/internal/service/processing/tagging"
	processingtitle "github.com/pt-nexus/server/internal/service/processing/title"
)

// SeedDraft 表示“种子参数”在抓取/修复/纠偏过程中逐步补全的领域实体草稿。
// 它的职责是：统一承载字段，并在流程末端生成可写入 `seed_parameters` 的 record。
// 注意：SeedDraft 不是 ORM 实体；数据库读写仍由 repository 负责。
type SeedDraft struct {
	Hash      string
	TorrentID string
	SiteName  string
	Nickname  string

	Name string

	Title      string
	Subtitle   string
	IMDbLink   string
	DoubanLink string
	TMDbLink   string

	Type       string
	Medium     string
	VideoCodec string
	AudioCodec string
	Resolution string
	Team       string
	Source     string

	Poster                 string
	Screenshots            string
	ScreenshotReviewStatus string
	Statement              string
	Body                   string
	Mediainfo              string

	RawTags          []string
	Tags             []string
	TitleComponents  []map[string]any
	TorrentFileNames []string
	EpisodeTagReason string

	RemovedARDTUDeclarations []string

	IsReviewed        bool
	MediainfoStatus   string
	BDInfoTaskID      any
	BDInfoStartedAt   any
	BDInfoCompletedAt any
	BDInfoError       string

	CreatedAt string
	UpdatedAt string
}

// NewSeedDraft 创建一个空的种子草稿，调用方需按流程逐步 Apply 字段并最终落库。
func NewSeedDraft(hash, torrentID, siteName, nickname string) *SeedDraft {
	return &SeedDraft{
		Hash:      strings.TrimSpace(hash),
		TorrentID: strings.TrimSpace(torrentID),
		SiteName:  strings.TrimSpace(siteName),
		Nickname:  strings.TrimSpace(nickname),

		RawTags:                  []string{},
		Tags:                     []string{},
		TitleComponents:          []map[string]any{},
		RemovedARDTUDeclarations: []string{},
	}
}

// ApplyReviewExtract 将详情页解析得到的 ReviewExtractedData 写入草稿（不包含后续修复/映射）。
func (d *SeedDraft) ApplyReviewExtract(review parser.ReviewExtractedData, detailHTML string) {
	if d == nil {
		return
	}

	if strings.TrimSpace(review.Title) != "" {
		d.Title = strings.TrimSpace(review.Title)
	}

	d.Statement = strings.TrimSpace(review.Statement)
	d.Poster = strings.TrimSpace(review.Poster)
	d.Body = strings.TrimSpace(review.Body)
	d.Screenshots = strings.TrimSpace(review.Screens)
	d.Mediainfo = strings.TrimSpace(review.Mediainfo)

	d.Type = strings.TrimSpace(review.Type)
	d.Medium = strings.TrimSpace(review.Medium)
	d.VideoCodec = strings.TrimSpace(review.VideoCodec)
	d.AudioCodec = strings.TrimSpace(review.AudioCodec)
	d.Resolution = strings.TrimSpace(review.Resolution)
	d.Team = strings.TrimSpace(review.Team)
	d.Source = strings.TrimSpace(review.Source)

	d.RawTags = append([]string{}, review.Tags...)

	if review.RemovedARDTUDeclarations != nil {
		d.RemovedARDTUDeclarations = append([]string{}, review.RemovedARDTUDeclarations...)
	} else {
		d.RemovedARDTUDeclarations = []string{}
	}

	d.IMDbLink = firstNonEmpty(
		strings.TrimSpace(review.IMDbLink),
		processingrepair.NormalizeExternalLink(parser.ReIMDbLink().FindString(detailHTML), parser.ReIMDbLink()),
	)
	d.DoubanLink = firstNonEmpty(
		strings.TrimSpace(review.DoubanLink),
		processingrepair.NormalizeExternalLink(parser.ReDoubanLink().FindString(detailHTML), parser.ReDoubanLink()),
	)
	d.TMDbLink = firstNonEmpty(
		strings.TrimSpace(review.TMDbLink),
		processingrepair.NormalizeExternalLink(parser.ReTMDbLink().FindString(detailHTML), parser.ReTMDbLink()),
	)

	subtitle := strings.TrimSpace(review.Subtitle)
	if subtitle == "" {
		subtitle = strings.TrimSpace(processingrepair.ExtractDoubanSummary(detailHTML))
	}
	d.Subtitle = subtitle
}

// ApplyRepairResult 将“抓取修复”阶段（海报/简介/截图修复）的产物写回草稿。
func (d *SeedDraft) ApplyRepairResult(result processingrepair.ParallelFetchRepairResult) {
	if d == nil {
		return
	}
	d.Poster = strings.TrimSpace(result.ReviewData.Poster)
	d.Body = strings.TrimSpace(result.ReviewData.Body)
	d.Screenshots = strings.TrimSpace(result.ReviewData.Screens)
	d.ScreenshotReviewStatus = strings.TrimSpace(result.ScreenshotReviewStatus)

	d.IMDbLink = strings.TrimSpace(result.IMDbLink)
	d.DoubanLink = strings.TrimSpace(result.DoubanLink)
	d.TMDbLink = strings.TrimSpace(result.TMDbLink)
}

// CorrectMediumAndTitleByMediaType 在识别 MediaInfo/BDInfo 后，对媒介键与标题 BluRay 标记纠偏。
func (d *SeedDraft) CorrectMediumAndTitleByMediaType(isMediainfo, isBDInfo bool) (string, string, string, string) {
	if d == nil {
		return "", "", "", ""
	}
	mediumBefore := strings.TrimSpace(d.Medium)
	titleBefore := strings.TrimSpace(d.Title)

	d.Medium = processingtitle.PreferExplicitTitleMedium(d.Medium, d.Title, d.Mediainfo)
	d.Medium = processingmedia.NormalizeMediumByMediaType(d.Medium, isMediainfo, isBDInfo)
	d.Title = processingmedia.NormalizeBlurayTokenByMediaType(d.Title, isMediainfo, isBDInfo)

	return mediumBefore, strings.TrimSpace(d.Medium), titleBefore, strings.TrimSpace(d.Title)
}

// BuildTitleComponents 基于标题与媒体文本生成标题组件，并在必要时用标题组件覆盖视频编码字段。
func (d *SeedDraft) BuildTitleComponents(buildSimpleComponents func(title string, releaseGroup string, mediaInfo string) []map[string]any) []map[string]any {
	if d == nil {
		return []map[string]any{}
	}
	result := processingtitle.BuildTitleComponentsForStorage(d.Title, d.Mediainfo, buildSimpleComponents)
	d.TitleComponents = result.Components

	if fromTitle := processingtitle.StandardizedVideoCodecFromTitleComponents(d.TitleComponents); fromTitle != "" {
		d.VideoCodec = fromTitle
	}
	return d.TitleComponents
}

// CompleteAndMapTags 进行标签补全与标准化映射（只保留能映射到标准 tag.* 的标签）。
func (d *SeedDraft) CompleteAndMapTags(siteIdentifier string, formatIsBDInfo bool, savePath string, torrentNameForPath string, downloaderID string, rootConfig map[string]any) []string {
	if d == nil {
		return []string{}
	}

	descriptionForTags := strings.TrimSpace(strings.Join([]string{d.Statement, d.Body}, "\n"))
	rawTagCandidates := make([]string, 0, len(d.RawTags)+16)
	rawTagCandidates = append(rawTagCandidates, d.RawTags...)
	rawTagCandidates = append(rawTagCandidates, processingtagging.ExtractRawTagsFromTitleComponents(d.TitleComponents)...)
	rawTagCandidates = append(rawTagCandidates, processingtagging.ExtractRawTagsFromSubtitle(d.Subtitle)...)
	rawTagCandidates = append(rawTagCandidates, processingtagging.ExtractTagsFromDescriptionCategory(descriptionForTags)...)
	rawTagCandidates = append(rawTagCandidates, processingtagging.ExtractTagsFromDescriptionScore(descriptionForTags)...)
	rawTagCandidates = append(rawTagCandidates, processingtagging.ExtractRawTagsFromMediaText(d.Mediainfo, formatIsBDInfo)...)

	if processingtagging.CheckAnimationTypeFromDescription(descriptionForTags) {
		d.Type = "category.animation"
	}

	completion := processingtagging.CheckCompletionStatusWithDownloaderContext(
		d.Title,
		d.Subtitle,
		descriptionForTags,
		processingtagging.CompletionCheckContext{
			SavePath:     savePath,
			TorrentName:  torrentNameForPath,
			ContentName:  d.Title,
			DownloaderID: downloaderID,
			RootConfig:   rootConfig,
		},
	)
	if processingtagging.ShouldAddCompletionTag(rawTagCandidates, completion) {
		rawTagCandidates = append(rawTagCandidates, "完结")
	}
	episodeTagResult := processingtagging.DetectEpisodeTag(processingtagging.EpisodeTagInput{
		Title:            d.Title,
		Subtitle:         d.Subtitle,
		TorrentName:      torrentNameForPath,
		Type:             d.Type,
		ExistingTags:     rawTagCandidates,
		TorrentFileNames: d.TorrentFileNames,
		Completion:       completion,
	})
	d.EpisodeTagReason = strings.TrimSpace(episodeTagResult.Reason)
	if episodeTagResult.Matched {
		rawTagCandidates = append(rawTagCandidates, "分集")
	}

	mappedTags, unmappedTags := processingtagging.MapTagsToStandard(rawTagCandidates, siteIdentifier)
	d.Tags = mappedTags
	return unmappedTags
}

// NormalizeSeedParamName 对齐迁移链路：seed_parameters.name 使用 torrent 元数据名。
func (d *SeedDraft) NormalizeSeedParamName(metaName string) {
	if d == nil {
		return
	}
	d.Name = normalizeSeedParamName(metaName, d.Title, d.TorrentID)
}

func normalizeSeedParamName(metaName string, fallbackTitle string, fallbackTorrentID string) string {
	name := strings.TrimSpace(metaName)
	if name == "" {
		name = strings.TrimSpace(fallbackTitle)
	}
	if strings.HasSuffix(strings.ToLower(name), ".torrent") {
		name = strings.TrimSpace(name[:len(name)-len(".torrent")])
	}
	if name == "" {
		name = strings.TrimSpace(fallbackTorrentID)
	}
	return name
}

// ToSeedParameterRecord 将草稿序列化为可写入 seed_parameters 的 record（字段名保持与历史一致）。
func (d *SeedDraft) ToSeedParameterRecord() map[string]any {
	if d == nil {
		return map[string]any{}
	}

	tags := d.Tags
	if tags == nil {
		tags = []string{}
	}
	encodedTags, _ := json.Marshal(tags)

	removed := d.RemovedARDTUDeclarations
	if removed == nil {
		removed = []string{}
	}
	encodedRemoved, _ := json.Marshal(removed)

	components := d.TitleComponents
	if components == nil {
		components = []map[string]any{}
	}
	encodedComponents, _ := json.Marshal(components)

	return map[string]any{
		"hash":                      strings.TrimSpace(d.Hash),
		"torrent_id":                strings.TrimSpace(d.TorrentID),
		"site_name":                 strings.TrimSpace(d.SiteName),
		"nickname":                  strings.TrimSpace(d.Nickname),
		"name":                      strings.TrimSpace(d.Name),
		"title":                     strings.TrimSpace(d.Title),
		"subtitle":                  strings.TrimSpace(d.Subtitle),
		"imdb_link":                 strings.TrimSpace(d.IMDbLink),
		"douban_link":               strings.TrimSpace(d.DoubanLink),
		"tmdb_link":                 strings.TrimSpace(d.TMDbLink),
		"type":                      strings.TrimSpace(d.Type),
		"medium":                    strings.TrimSpace(d.Medium),
		"video_codec":               strings.TrimSpace(d.VideoCodec),
		"audio_codec":               strings.TrimSpace(d.AudioCodec),
		"resolution":                strings.TrimSpace(d.Resolution),
		"team":                      parser.NormalizeTeamKeyForSite(d.Team, d.SiteName),
		"source":                    strings.TrimSpace(d.Source),
		"tags":                      string(encodedTags),
		"poster":                    strings.TrimSpace(d.Poster),
		"screenshots":               strings.TrimSpace(d.Screenshots),
		"screenshot_review_status":  strings.TrimSpace(d.ScreenshotReviewStatus),
		"statement":                 strings.TrimSpace(d.Statement),
		"body":                      strings.TrimSpace(d.Body),
		"mediainfo":                 strings.TrimSpace(d.Mediainfo),
		"title_components":          string(encodedComponents),
		"removed_ardtudeclarations": string(encodedRemoved),
		"is_reviewed":               d.IsReviewed,
		"mediainfo_status":          strings.TrimSpace(d.MediainfoStatus),
		"bdinfo_task_id":            d.BDInfoTaskID,
		"bdinfo_started_at":         d.BDInfoStartedAt,
		"bdinfo_completed_at":       d.BDInfoCompletedAt,
		"bdinfo_error":              strings.TrimSpace(d.BDInfoError),
		"created_at":                strings.TrimSpace(d.CreatedAt),
		"updated_at":                strings.TrimSpace(d.UpdatedAt),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
