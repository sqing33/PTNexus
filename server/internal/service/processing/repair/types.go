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
