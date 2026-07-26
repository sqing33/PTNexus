package publisher

// PublishInput 定义发布器执行输入。
// 说明：workflow 会先完成通用字段抽取（标题/副标题/描述/外链等），再交由 Public 或 sites 发布器落盘与上传。
type PublishInput struct {
	TargetName string
	SiteCode   string
	BaseURL    string
	Cookie     string

	// TargetInfo 为站点完整配置（仅少数特殊发布器需要，例如 rousi 的 passkey/user_agent）。
	TargetInfo map[string]any
	RootConfig map[string]any

	UploadData  map[string]any
	TorrentPath string

	Title       string
	Subtitle    string
	Description string
	IMDbLink    string
	DoubanLink  string
	MediaInfo   string

	SourceSiteNickname      string
	FindSiteNicknameByGroup func(releaseGroup string) (string, error)

	// ExtraFormFields 用于在 Public 表单发布中追加字段（例如 PTLGS 的封面与截图分离字段）。
	// key 为最终表单字段名（非 mapping key）。
	ExtraFormFields map[string]string

	// AdjustFormFields 用于在 Public 表单发布中对最终表单字段做站点级修正（例如 HDFans 的标签/媒介细分覆盖）。
	// 注意：该回调会在基础字段 + 映射字段 + ExtraFormFields 合并完成后执行。
	AdjustFormFields func(formFields map[string]string)
}

// PublishResult 定义发布结果。
type PublishResult struct {
	PublishURL        string
	DirectDownloadURL string

	IsExistingTorrent bool

	UploadFormFields map[string]string

	// AttemptDetailLog 为发布过程细节日志，供 workflow 汇总返回给前端。
	AttemptDetailLog string
}
