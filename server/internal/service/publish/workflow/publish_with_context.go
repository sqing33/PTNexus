package workflow

import "strings"

// PublishWithContextInput 定义基于迁移上下文的发布执行输入。
type PublishWithContextInput struct {
	TargetSite          string
	TaskID              string
	Payload             map[string]any
	UploadData          map[string]any
	Context             Context
	TorrentPath         string
	DefaultDownloaderID string
	RootConfig          map[string]any
}

// PublishWithContextDeps 定义基于迁移上下文发布所需依赖。
type PublishWithContextDeps struct {
	GetSiteByName           func(name string) (map[string]any, error)
	ResolveTorrentPath      func(ctx Context) string
	AddToDownloader         func(payload map[string]any) (map[string]any, int)
	FindSiteNicknameByGroup func(releaseGroup string) (string, error)
	UpdateTorrentDetails    func(input PublishTorrentDetailsUpdateInput) (int64, error)
}

// ExecutePublishWithContext 按上下文执行单站点发布（含目标站校验与种子路径回退）。
// 参数/返回：input 包含目标站点、上下文与上传参数；deps 注入站点查询和路径解析；返回发布结果与状态码。
// 失败场景：目标站未配置、未开启迁移、种子路径不可用时返回错误。
// 副作用：会调用发布执行流程并可能向下载器添加任务。
func ExecutePublishWithContext(input PublishWithContextInput, deps PublishWithContextDeps) (map[string]any, int) {
	targetSite := strings.TrimSpace(input.TargetSite)
	if targetSite == "" {
		return map[string]any{"success": false, "logs": "错误：必须提供目标站点名称。", "url": nil}, 400
	}
	if strings.TrimSpace(input.TaskID) == "" {
		return map[string]any{"success": false, "logs": "错误：无效或已过期的任务ID。", "url": nil}, 400
	}
	if deps.GetSiteByName == nil {
		return map[string]any{"success": false, "logs": "错误：目标站点服务不可用。", "url": nil}, 500
	}

	targetInfo, err := deps.GetSiteByName(targetSite)
	if err != nil {
		return map[string]any{"success": false, "logs": "错误: 目标站点 '" + targetSite + "' 配置不完整。", "url": nil}, 404
	}
	migration := parseIntAny(targetInfo["migration"])
	if migration != 2 && migration != 3 {
		return map[string]any{"success": false, "logs": "错误: 站点 '" + targetSite + "' 未启用目标站迁移。", "url": nil}, 403
	}

	torrentPath := strings.TrimSpace(input.TorrentPath)
	if torrentPath == "" && deps.ResolveTorrentPath != nil {
		torrentPath = strings.TrimSpace(deps.ResolveTorrentPath(input.Context))
	}
	if torrentPath == "" {
		return map[string]any{"success": false, "logs": "错误：未找到可发布的种子文件，请先重新抓取源站信息。", "url": nil}, 400
	}

	uploadData := input.UploadData
	if uploadData == nil {
		uploadData = map[string]any{}
	}

	return ExecutePublish(
		PublishExecutionInput{
			TargetSite:           targetSite,
			TargetInfo:           targetInfo,
			UploadData:           uploadData,
			SourceSiteNickname:   input.Context.SourceNickname,
			SourceTorrentHash:    input.Context.Hash,
			SourceTorrentName:    input.Context.Name,
			TorrentPath:          torrentPath,
			Payload:              input.Payload,
			FallbackSavePath:     input.Context.SavePath,
			FallbackDownloaderID: input.Context.DownloaderID,
			DefaultDownloaderID:  input.DefaultDownloaderID,
			RootConfig:           input.RootConfig,
		},
		PublishExecutionDeps{
			AddToDownloader:         deps.AddToDownloader,
			FindSiteNicknameByGroup: deps.FindSiteNicknameByGroup,
			UpdateTorrentDetails:    deps.UpdateTorrentDetails,
		},
	)
}

func parseIntAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}
