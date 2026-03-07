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

// ScreenshotPreviewCandidate 表示供前端选择的低清截图候选。
type ScreenshotPreviewCandidate struct {
	ID          string  `json:"id"`
	TimeSeconds float64 `json:"time_seconds"`
	TimeLabel   string  `json:"time_label"`
	PreviewData string  `json:"preview_data"`
	Recommended bool    `json:"recommended"`
}
