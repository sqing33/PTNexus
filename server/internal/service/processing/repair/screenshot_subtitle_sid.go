package repair

import (
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
)

// getBestChineseSubtitleSID 返回可直接用于自动正式截图的中文字幕 sid。
func getBestChineseSubtitleSID(ffprobePath string, videoPath string) int {
	if strings.TrimSpace(ffprobePath) == "" {
		logx.PlainInfof("   ⚠️ 未找到 ffprobe，无法分析字幕流。")
		return 0
	}

	inspection, _, err := resolveScreenshotSubtitleSelection(ffprobePath, videoPath, 0, false)
	if err != nil {
		logx.PlainInfof("   ⚠️ 字幕分析失败: %v", err)
		return 0
	}
	if inspection.SubtitleState != ScreenshotSubtitleStateConfirmedChinese || inspection.CurrentSubtitleSID <= 0 {
		return 0
	}

	for _, stream := range inspection.SubtitleStreams {
		if stream.SubtitleSID != inspection.CurrentSubtitleSID {
			continue
		}
		logx.PlainInfof("   🎯 自动选中字幕: %s", strings.TrimSpace(stream.DisplayName))
		return stream.SubtitleSID
	}
	return inspection.CurrentSubtitleSID
}
