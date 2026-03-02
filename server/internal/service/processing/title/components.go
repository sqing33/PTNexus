package title

import (
	"regexp"
	"strings"

	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
)

// BuildResult 表示标题组件构建结果。
type BuildResult struct {
	NormalizedTitle string
	Components      []map[string]any
	IsMediainfo     bool
	IsBDInfo        bool
	Reason          string
}

// BuildTitleComponentsForStorage 构建标题组件，并按媒体文本类型执行纠偏与覆盖。
// 参数/返回：buildSimpleComponents 用于注入标题拆分实现（与现有解析逻辑保持兼容）。
// 失败场景：不返回错误，无法识别时输出尽量完整的组件。
// 副作用：无。
func BuildTitleComponentsForStorage(
	title string,
	mediainfoText string,
	buildSimpleComponents func(title string, releaseGroup string, mediaInfo string) []map[string]any,
) BuildResult {
	trimmedTitle := strings.TrimSpace(title)
	trimmedMedia := strings.TrimSpace(mediainfoText)

	isMediainfo, isBDInfo, reason := processingmedia.ValidateMediaInfoFormat(trimmedMedia)
	normalizedTitle := trimmedTitle
	if isMediainfo || isBDInfo {
		normalizedTitle = processingmedia.NormalizeBlurayTokenByMediaType(trimmedTitle, isMediainfo, isBDInfo)
	}

	components := []map[string]any{}
	if buildSimpleComponents != nil {
		components = buildSimpleComponents(normalizedTitle, "", trimmedMedia)
	}

	// 对齐 Python：在标题解析后，用 MediaInfo/BDInfo 的标准标签覆盖 HDR/音频。
	if isMediainfo || isBDInfo {
		hdr := processingmedia.ExtractHDRInfoFromMediaText(trimmedMedia, isBDInfo)
		audio := processingmedia.ExtractAudioInfoFromMediaText(trimmedMedia, isBDInfo)
		components = processingmedia.ApplyMediaInfoOverrides(components, hdr, audio)
	}

	// 对齐 Python：再次根据 MediaInfo/BDInfo 类型修正所有组件值中的 Blu-ray/BluRay 写法。
	if isMediainfo || isBDInfo {
		for idx := range components {
			value := strings.TrimSpace(toStringAny(components[idx]["value"], ""))
			if value == "" {
				continue
			}
			components[idx]["value"] = processingmedia.NormalizeBlurayTokenByMediaType(value, isMediainfo, isBDInfo)
		}
	}

	if isMediainfo || isBDInfo {
		for idx := range components {
			if strings.TrimSpace(toStringAny(components[idx]["key"], "")) != "媒介" {
				continue
			}
			value := strings.TrimSpace(toStringAny(components[idx]["value"], ""))
			if value == "" {
				continue
			}
			components[idx]["value"] = processingmedia.NormalizeBlurayTokenByMediaType(value, isMediainfo, isBDInfo)
		}
	} else {
		// 当无法判断媒体文本类型时，优先保持标题中出现的 BluRay/Blu-ray 写法，避免强制统一为 Blu-ray。
		preferred := PreferredBlurayTokenFromTitle(trimmedTitle)
		if preferred != "" {
			for idx := range components {
				if strings.TrimSpace(toStringAny(components[idx]["key"], "")) != "媒介" {
					continue
				}
				value := strings.TrimSpace(toStringAny(components[idx]["value"], ""))
				if value == "" {
					continue
				}
				// 仅在媒介已被识别为蓝光时覆盖，避免误改其他媒介类型。
				if strings.EqualFold(value, "Blu-ray") || strings.EqualFold(value, "BluRay") || strings.EqualFold(value, "Bluray") || strings.EqualFold(value, "BluRay") {
					components[idx]["value"] = preferred
				}
			}
		}
	}

	return BuildResult{
		NormalizedTitle: normalizedTitle,
		Components:      components,
		IsMediainfo:     isMediainfo,
		IsBDInfo:        isBDInfo,
		Reason:          reason,
	}
}

// StandardizedVideoCodecFromTitleComponents 从标题组件提取并标准化视频编码。
func StandardizedVideoCodecFromTitleComponents(components []map[string]any) string {
	if len(components) == 0 {
		return ""
	}

	for _, component := range components {
		if strings.TrimSpace(toStringAny(component["key"], "")) != "视频编码" {
			continue
		}

		raw := strings.TrimSpace(toStringAny(component["value"], ""))
		if raw == "" || strings.EqualFold(raw, "N/A") {
			return ""
		}

		switch strings.ToUpper(raw) {
		case "AV1":
			return "video.av1"
		case "X265":
			return "video.x265"
		case "HEVC", "H.265", "H265":
			return "video.h265"
		case "X264":
			return "video.x264"
		case "AVC", "H.264", "H264":
			return "video.h264"
		case "VC-1", "VC1":
			return "video.vc1"
		case "MPEG-2", "MPEG2":
			return "video.mpeg2"
		default:
			return ""
		}
	}
	return ""
}

var (
	reTokenBluRay  = regexp.MustCompile(`\bBluRay\b`)
	reTokenBluDash = regexp.MustCompile(`\bBlu-ray\b`)
	reTokenBluray  = regexp.MustCompile(`\bBluray\b`)
	reTokenBLURAY  = regexp.MustCompile(`\bBLURAY\b`)
	reTokenBLUDASH = regexp.MustCompile(`\bBLU-RAY\b`)
)

// PreferredBlurayTokenFromTitle 返回标题里原始出现的 BluRay/Blu-ray 变体。
func PreferredBlurayTokenFromTitle(title string) string {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return ""
	}
	switch {
	case reTokenBluRay.FindStringIndex(trimmed) != nil:
		return "BluRay"
	case reTokenBluDash.FindStringIndex(trimmed) != nil:
		return "Blu-ray"
	case reTokenBluray.FindStringIndex(trimmed) != nil:
		return "Bluray"
	case reTokenBLUDASH.FindStringIndex(trimmed) != nil:
		return "BLU-RAY"
	case reTokenBLURAY.FindStringIndex(trimmed) != nil:
		return "BLURAY"
	default:
		return ""
	}
}
