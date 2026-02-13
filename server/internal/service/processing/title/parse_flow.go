package title

import (
	"errors"
	"strings"

	processingmedia "github.com/pt-nexus/server-go/internal/service/processing/media"
)

// ErrEmptyTitle 表示标题为空。
var ErrEmptyTitle = errors.New("标题不能为空")

// ParseTitleResult 表示标题解析流程输出。
type ParseTitleResult struct {
	TitleBefore        string
	TitleAfter         string
	Mediainfo          string
	FormatIsMediainfo  bool
	FormatIsBDInfo     bool
	FormatReason       string
	BuilderIsMediainfo bool
	BuilderIsBDInfo    bool
	Components         []map[string]any
}

// ParseTitleForStorage 按统一规则执行标题解析，并返回可入库存储的组件结构。
// 参数/返回：title 为原始标题，mediainfo 为媒体文本；返回标准化后的解析结果。
// 失败场景：title 为空时返回 ErrEmptyTitle。
// 副作用：无。
func ParseTitleForStorage(title string, mediainfo string) (ParseTitleResult, error) {
	trimmedTitle := strings.TrimSpace(title)
	trimmedMedia := strings.TrimSpace(mediainfo)
	if trimmedTitle == "" {
		return ParseTitleResult{}, ErrEmptyTitle
	}

	formatIsMediainfo, formatIsBDInfo, formatReason := processingmedia.ValidateMediaInfoFormat(trimmedMedia)
	normalizedTitle := trimmedTitle
	if formatIsMediainfo || formatIsBDInfo {
		normalizedTitle = processingmedia.NormalizeBlurayTokenByMediaType(trimmedTitle, formatIsMediainfo, formatIsBDInfo)
	}

	buildResult := BuildTitleComponentsForStorage(normalizedTitle, trimmedMedia, BuildSimpleTitleComponentsWithMediaInfo)
	return ParseTitleResult{
		TitleBefore:        trimmedTitle,
		TitleAfter:         strings.TrimSpace(buildResult.NormalizedTitle),
		Mediainfo:          trimmedMedia,
		FormatIsMediainfo:  formatIsMediainfo,
		FormatIsBDInfo:     formatIsBDInfo,
		FormatReason:       formatReason,
		BuilderIsMediainfo: buildResult.IsMediainfo,
		BuilderIsBDInfo:    buildResult.IsBDInfo,
		Components:         buildResult.Components,
	}, nil
}
