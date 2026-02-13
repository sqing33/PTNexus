package persist

import (
	"strings"

	"github.com/pt-nexus/server-go/internal/platform/logx"
	processingtagging "github.com/pt-nexus/server-go/internal/service/processing/tagging"
)

// FetchFinalizeLoggingInput 定义抓取最终收敛日志输出参数。
type FetchFinalizeLoggingInput struct {
	SourceSite        string
	SearchTerm        string
	SiteIdentifier    string
	FinalizeResult    FinalizeFetchedSeedResult
	Draft             *SeedDraft
	FetchRepairModule string
	TagMappingModule  string
	TagCompleteModule string
}

// FetchFinalizeLoggingResult 返回日志输出后的关键状态。
type FetchFinalizeLoggingResult struct {
	MediainfoStatus string
	MediainfoValid  bool
	MediainfoReason string
}

// LogFetchFinalizeResult 统一打印抓取最终收敛相关日志，并返回关键状态字段。
// 参数/返回：input 包含站点、种子、收敛结果与日志模块名；返回 mediainfo 状态与判定信息。
// 失败场景：无，内部按字段空值兜底。
// 副作用：写入日志。
func LogFetchFinalizeResult(input FetchFinalizeLoggingInput) FetchFinalizeLoggingResult {
	finalizeResult := input.FinalizeResult
	formatIsMediainfo := finalizeResult.FormatIsMediainfo
	formatReason := strings.TrimSpace(finalizeResult.FormatReason)
	mediaType := "BDInfo"
	if formatIsMediainfo {
		mediaType = "MediaInfo"
	}

	if finalizeResult.MediainfoValid {
		logx.Infof(input.FetchRepairModule, "媒体格式判定命中 source_site=%s search_term=%s media_type=%s reason=%s", input.SourceSite, input.SearchTerm, mediaType, formatReason)
		if finalizeResult.MediumAfter != finalizeResult.MediumBefore {
			logx.Infof(
				input.FetchRepairModule,
				"媒介纠偏完成 source_site=%s search_term=%s medium_before=%s medium_after=%s media_type=%s",
				input.SourceSite,
				input.SearchTerm,
				finalizeResult.MediumBefore,
				finalizeResult.MediumAfter,
				mediaType,
			)
		}
		if finalizeResult.TitleAfter != finalizeResult.TitleBefore {
			logx.Infof(
				input.FetchRepairModule,
				"标题蓝光标记纠偏完成 source_site=%s search_term=%s title_before=%s title_after=%s",
				input.SourceSite,
				input.SearchTerm,
				finalizeResult.TitleBefore,
				finalizeResult.TitleAfter,
			)
		}
	} else {
		logx.Warnf(input.FetchRepairModule, "媒体格式判定未命中 source_site=%s search_term=%s reason=%s", input.SourceSite, input.SearchTerm, formatReason)
	}

	unmappedTags := finalizeResult.UnmappedTags
	if len(unmappedTags) > 0 {
		limit := 12
		if len(unmappedTags) < limit {
			limit = len(unmappedTags)
		}
		logx.Debugf(input.TagMappingModule, "标签映射未命中 source_site=%s count=%d sample=%v", input.SiteIdentifier, len(unmappedTags), unmappedTags[:limit])
	}
	if input.Draft != nil {
		logx.Infof(input.TagCompleteModule, "标签补全完成 torrent_id=%s site=%s tags_count=%d tags_sample=%v", input.SearchTerm, input.SiteIdentifier, len(input.Draft.Tags), processingtagging.TagSample(input.Draft.Tags, 8))
	}

	mediainfoStatus := strings.TrimSpace(finalizeResult.MediainfoStatus)
	if mediainfoStatus == "" {
		mediainfoStatus = "queued"
	}
	mediainfoValid := finalizeResult.MediainfoValid
	if !mediainfoValid {
		logx.Warnf(input.FetchRepairModule, "媒体信息初始校验未通过 source_site=%s search_term=%s reason=%s", input.SourceSite, input.SearchTerm, formatReason)
	}
	return FetchFinalizeLoggingResult{
		MediainfoStatus: mediainfoStatus,
		MediainfoValid:  mediainfoValid,
		MediainfoReason: formatReason,
	}
}
