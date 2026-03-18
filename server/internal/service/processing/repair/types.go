package repair

// MovieInfoResult 表示影片外部信息抓取结果。
type MovieInfoResult struct {
	Poster string
	Intro  string
	IMDb   string
	Douban string
	TMDb   string
}

// ScreenshotGenerateInput 描述截图自动生成与上传所需的输入参数。
type ScreenshotGenerateInput struct {
	Payload     map[string]any
	SourceInfo  map[string]any
	ContentName string
	RootConfig  map[string]any
}

// ScreenshotSubtitleState 表示截图流程中的字幕判定状态。
type ScreenshotSubtitleState string

const (
	ScreenshotSubtitleStateConfirmedChinese     ScreenshotSubtitleState = "confirmed_chinese"
	ScreenshotSubtitleStateUsableButUnconfirmed ScreenshotSubtitleState = "usable_but_unconfirmed"
	ScreenshotSubtitleStateNoUsableSubtitle     ScreenshotSubtitleState = "no_usable_subtitle"
)

// ScreenshotSubtitleStream 表示一个可供截图流程选择的字幕流。
type ScreenshotSubtitleStream struct {
	SubtitleSID        int    `json:"subtitle_sid"`
	StreamIndex        int    `json:"stream_index"`
	CodecName          string `json:"codec_name"`
	Language           string `json:"language"`
	Title              string `json:"title"`
	DisplayName        string `json:"display_name"`
	IsConfidentChinese bool   `json:"is_confident_chinese"`
	IsDefault          bool   `json:"is_default"`
}

// ScreenshotSubtitleInspection 描述字幕流探测结果。
type ScreenshotSubtitleInspection struct {
	SubtitleState      ScreenshotSubtitleState    `json:"subtitle_state"`
	SubtitleStreams    []ScreenshotSubtitleStream `json:"subtitle_streams,omitempty"`
	CurrentSubtitleSID int                        `json:"current_subtitle_sid,omitempty"`
}

// ScreenshotPreviewCandidate 表示供前端选择的低清截图候选。
type ScreenshotPreviewCandidate struct {
	ID          string  `json:"id"`
	TimeSeconds float64 `json:"time_seconds"`
	TimeLabel   string  `json:"time_label"`
	PreviewData string  `json:"preview_data"`
	Recommended bool    `json:"recommended"`
}

// ScreenshotPreviewResult 表示截图预览接口返回的数据。
type ScreenshotPreviewResult struct {
	Candidates         []ScreenshotPreviewCandidate `json:"candidates"`
	SelectionLimit     int                          `json:"selection_limit"`
	SubtitleState      ScreenshotSubtitleState      `json:"subtitle_state"`
	SubtitleStreams    []ScreenshotSubtitleStream   `json:"subtitle_streams,omitempty"`
	CurrentSubtitleSID int                          `json:"current_subtitle_sid,omitempty"`
}

type ScreenshotPreviewBundle = ScreenshotPreviewResult
