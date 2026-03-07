package shared

import "strings"

const (
	// ScreenshotReviewStatusNone 表示当前截图不需要额外人工确认。
	ScreenshotReviewStatusNone = "none"
	// ScreenshotReviewStatusPending 表示当前截图已自动生成，但仍需人工确认字幕时间点。
	ScreenshotReviewStatusPending = "pending"
	// ScreenshotReviewStatusConfirmed 表示当前截图已由用户人工确认。
	ScreenshotReviewStatusConfirmed = "confirmed"

	// ScreenshotReviewModeBackground 表示后台流程：允许自动生成正式截图并标记待确认。
	ScreenshotReviewModeBackground = "background"
	// ScreenshotReviewModeInteractive 表示交互流程：前端直接拉起候选截图选择。
	ScreenshotReviewModeInteractive = "interactive"
)

// NormalizeScreenshotReviewStatus 归一化截图审查状态，未知值会回退为 none。
func NormalizeScreenshotReviewStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ScreenshotReviewStatusPending:
		return ScreenshotReviewStatusPending
	case ScreenshotReviewStatusConfirmed:
		return ScreenshotReviewStatusConfirmed
	default:
		return ScreenshotReviewStatusNone
	}
}

// NeedsScreenshotManualReview 判断当前截图是否仍需人工确认。
func NeedsScreenshotManualReview(status string) bool {
	return NormalizeScreenshotReviewStatus(status) == ScreenshotReviewStatusPending
}

// NormalizeScreenshotReviewMode 归一化截图处理模式，未知值默认回退为 background。
func NormalizeScreenshotReviewMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ScreenshotReviewModeInteractive:
		return ScreenshotReviewModeInteractive
	default:
		return ScreenshotReviewModeBackground
	}
}
