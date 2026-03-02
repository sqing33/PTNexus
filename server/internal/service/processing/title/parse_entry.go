package title

import (
	"errors"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
)

// ParseTitleEntryInput 定义标题解析接口流程输入。
type ParseTitleEntryInput struct {
	Title     string
	Mediainfo string
	RequestID string
	LogModule string
}

// ParseTitleEntry 执行标题解析接口流程（含日志、媒体判定信息与组件返回）。
// 参数/返回：input 包含标题、媒体文本与请求 ID；返回标准接口响应与状态码。
// 失败场景：标题为空返回 400，解析异常返回 500。
// 副作用：写入流程日志。
func ParseTitleEntry(input ParseTitleEntryInput) (map[string]any, int) {
	logModule := strings.TrimSpace(input.LogModule)
	if logModule == "" {
		logModule = "迁移-标题解析"
	}

	trimmedTitle := strings.TrimSpace(input.Title)
	trimmedMedia := strings.TrimSpace(input.Mediainfo)
	requestID := strings.TrimSpace(input.RequestID)
	logx.Infof(logModule, "开始解析 请求ID=%s title=%s title_len=%d mediainfo_len=%d", requestID, trimmedTitle, len(trimmedTitle), len(trimmedMedia))

	parseResult, err := ParseTitleForStorage(trimmedTitle, trimmedMedia)
	if err != nil {
		if errors.Is(err, ErrEmptyTitle) {
			logx.Warnf(logModule, "标题为空 请求ID=%s", requestID)
			return map[string]any{"success": false, "error": "标题不能为空。"}, 400
		}
		logx.Errorf(logModule, "解析失败 请求ID=%s err=%v", requestID, err)
		return map[string]any{"success": false, "error": err.Error()}, 500
	}

	// 对齐 MediaInfo/BDInfo 刷新接口：返回一份基于当前标题+媒体文本的“推断标准化参数”，用于前端做纠偏。
	inferred := parser.InferStandardizedValues(parseResult.TitleAfter, parseResult.Mediainfo, "")

	logx.Infof(
		logModule,
		"媒体格式判定 请求ID=%s is_mediainfo=%t is_bdinfo=%t reason=%s",
		requestID,
		parseResult.FormatIsMediainfo,
		parseResult.FormatIsBDInfo,
		parseResult.FormatReason,
	)

	if parseResult.TitleAfter != parseResult.TitleBefore {
		logx.Infof(logModule, "标题蓝光标记纠偏 请求ID=%s before=%s after=%s", requestID, parseResult.TitleBefore, parseResult.TitleAfter)
	}

	if !(parseResult.FormatIsMediainfo || parseResult.FormatIsBDInfo) && (parseResult.BuilderIsMediainfo || parseResult.BuilderIsBDInfo) {
		logx.Warnf(
			logModule,
			"媒体格式判定不一致 请求ID=%s handler_mediainfo=%t handler_bdinfo=%t builder_mediainfo=%t builder_bdinfo=%t",
			requestID,
			parseResult.FormatIsMediainfo,
			parseResult.FormatIsBDInfo,
			parseResult.BuilderIsMediainfo,
			parseResult.BuilderIsBDInfo,
		)
	}

	if parseResult.FormatIsMediainfo || parseResult.FormatIsBDInfo {
		mediaType := "BDInfo"
		if parseResult.FormatIsMediainfo {
			mediaType = "MediaInfo"
		}
		for _, item := range parseResult.Components {
			if strings.TrimSpace(processingshared.ToString(item["key"], "")) != "媒介" {
				continue
			}
			value := strings.TrimSpace(processingshared.ToString(item["value"], ""))
			if value != "" {
				logx.Infof(logModule, "媒介解析结果 请求ID=%s value=%s media_type=%s", requestID, value, mediaType)
			}
			break
		}
	}

	if len(parseResult.Components) == 0 {
		logx.Warnf(logModule, "解析失败：组件为空 请求ID=%s title=%s", requestID, parseResult.TitleAfter)
		return map[string]any{
			"success": false,
			"message": "未能从此标题中解析出有效参数。",
			"components": []map[string]any{{
				"key":   "主标题",
				"value": parseResult.TitleAfter,
			}},
			"inferred_standardized_params": map[string]any{
				"video_codec": strings.TrimSpace(inferred["video_codec"]),
				"audio_codec": strings.TrimSpace(inferred["audio_codec"]),
				"resolution":  strings.TrimSpace(inferred["resolution"]),
			},
		}, 200
	}

	logx.Infof(logModule, "解析完成 请求ID=%s components=%d", requestID, len(parseResult.Components))
	return map[string]any{
		"success":    true,
		"components": parseResult.Components,
		"inferred_standardized_params": map[string]any{
			"video_codec": strings.TrimSpace(inferred["video_codec"]),
			"audio_codec": strings.TrimSpace(inferred["audio_codec"]),
			"resolution":  strings.TrimSpace(inferred["resolution"]),
		},
	}, 200
}
