package persist

import (
	"errors"
	"strings"
	"time"

	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
)

// FinalizeFetchedSeedInput 定义抓取阶段草稿收敛为可入库记录所需的输入。
type FinalizeFetchedSeedInput struct {
	Draft *SeedDraft

	MetaName           string
	SiteIdentifier     string
	SavePath           string
	DownloaderID       string
	TorrentNameForPath string
	RootConfig         map[string]any
	Now                time.Time

	BuildSimpleTitleComponents func(title string, releaseGroup string, mediaInfo string) []map[string]any
}

// FinalizeFetchedSeedResult 定义抓取草稿收敛后的产物与判定信息。
type FinalizeFetchedSeedResult struct {
	Record map[string]any

	FormatIsMediainfo bool
	FormatIsBDInfo    bool
	FormatReason      string
	MediainfoValid    bool
	MediainfoStatus   string

	MediumBefore string
	MediumAfter  string
	TitleBefore  string
	TitleAfter   string

	UnmappedTags []string
}

// FinalizeFetchedSeed 将抓取后的 SeedDraft 做最终纠偏与补全，并生成可写入 seed_parameters 的记录。
// 参数/返回：输入需提供 Draft 与上下文（站点、路径、当前时间）；返回媒体判定结果与 record。
// 失败场景：Draft 为空时返回 error。
// 副作用：会原地修改 Draft 的字段值（标题、标签、媒体状态、时间戳等）。
func FinalizeFetchedSeed(input FinalizeFetchedSeedInput) (FinalizeFetchedSeedResult, error) {
	draft := input.Draft
	if draft == nil {
		return FinalizeFetchedSeedResult{}, errors.New("seed draft is nil")
	}

	formatIsMediainfo, formatIsBDInfo, formatReason := processingmedia.ValidateMediaInfoFormat(draft.Mediainfo)
	mediainfoValid := formatIsMediainfo || formatIsBDInfo

	mediumBefore, mediumAfter, titleBefore, titleAfter := "", "", "", ""
	if mediainfoValid {
		mediumBefore, mediumAfter, titleBefore, titleAfter = draft.CorrectMediumAndTitleByMediaType(formatIsMediainfo, formatIsBDInfo)
	} else {
		// 当媒体文本不合规时，不信任基于“标题+媒体文本”的音频推断，避免点阵表格把 DTS-HD MA 等误写入 audio_codec。
		// 对齐抓取链路：此处仅回退音频编码为“仅标题推断”的结果，等待后续 MediaInfo 刷新后再纠偏。
		titleOnly := parser.InferStandardizedValues(strings.TrimSpace(draft.Title), "", "")
		if inferredAudio := strings.TrimSpace(titleOnly["audio_codec"]); inferredAudio != "" {
			draft.AudioCodec = inferredAudio
		}
	}

	// 在抓取修复更新正文后，用简介中的“产地/制片国家/地区”再修正一次 source，避免修复前推断锁死。
	description := strings.TrimSpace(strings.Join([]string{strings.TrimSpace(draft.Statement), strings.TrimSpace(draft.Body)}, "\n"))
	if inferredSource := strings.TrimSpace(parser.InferSourceFromDescription(description)); inferredSource != "" {
		draft.Source = inferredSource
	}

	draft.BuildTitleComponents(input.BuildSimpleTitleComponents)
	unmappedTags := draft.CompleteAndMapTags(
		input.SiteIdentifier,
		formatIsBDInfo,
		input.SavePath,
		input.TorrentNameForPath,
		input.DownloaderID,
		input.RootConfig,
	)

	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	nowText := now.Format("2006-01-02 15:04:05")

	mediainfoStatus := "queued"
	if mediainfoValid {
		mediainfoStatus = "completed"
	}

	draft.MediainfoStatus = mediainfoStatus
	draft.ScreenshotReviewStatus = processingshared.NormalizeScreenshotReviewStatus(draft.ScreenshotReviewStatus)
	draft.IsReviewed = false
	draft.BDInfoTaskID = nil
	draft.BDInfoStartedAt = nil
	draft.BDInfoCompletedAt = nil
	draft.BDInfoError = ""
	draft.CreatedAt = nowText
	draft.UpdatedAt = nowText
	draft.NormalizeSeedParamName(input.MetaName)

	return FinalizeFetchedSeedResult{
		Record:            draft.ToSeedParameterRecord(),
		FormatIsMediainfo: formatIsMediainfo,
		FormatIsBDInfo:    formatIsBDInfo,
		FormatReason:      formatReason,
		MediainfoValid:    mediainfoValid,
		MediainfoStatus:   mediainfoStatus,
		MediumBefore:      mediumBefore,
		MediumAfter:       mediumAfter,
		TitleBefore:       titleBefore,
		TitleAfter:        titleAfter,
		UnmappedTags:      unmappedTags,
	}, nil
}
