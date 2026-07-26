package workflow

import "strings"

// PublishFromPayloadInput 定义从前端 payload 发起单站发布的输入。
type PublishFromPayloadInput struct {
	Payload             map[string]any
	TorrentPath         string
	DefaultDownloaderID string
	RootConfig          map[string]any
}

// PublishFromPayloadDeps 定义从 payload 发起发布所需依赖。
type PublishFromPayloadDeps struct {
	ContextState *ContextState

	GetSiteByName           func(name string) (map[string]any, error)
	ResolveTorrentPath      func(ctx Context) string
	AddToDownloader         func(payload map[string]any) (map[string]any, int)
	FindSiteNicknameByGroup func(releaseGroup string) (string, error)
}

// ExecutePublishFromPayload 按前端 payload 执行单站点发布（自动读取 task_id 对应上下文）。
// 参数/返回：input 为发布请求 payload；deps 注入上下文与发布依赖；返回发布结果与状态码。
// 失败场景：task_id 无效或上下文过期返回 400。
// 副作用：会触发目标站上传流程，并可选自动添加到下载器。
func ExecutePublishFromPayload(input PublishFromPayloadInput, deps PublishFromPayloadDeps) (map[string]any, int) {
	payload := input.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	taskID := strings.TrimSpace(toStringAny(payload["task_id"], ""))
	if taskID == "" || deps.ContextState == nil {
		return map[string]any{"success": false, "logs": "错误：无效或已过期的任务ID。", "url": nil}, 400
	}
	ctx, ok := deps.ContextState.Get(taskID)
	if !ok {
		return map[string]any{"success": false, "logs": "错误：无效或已过期的任务ID。", "url": nil}, 400
	}

	uploadData, _ := payload["upload_data"].(map[string]any)
	return ExecutePublishWithContext(
		PublishWithContextInput{
			TargetSite:          strings.TrimSpace(toStringAny(payload["targetSite"], "")),
			TaskID:              taskID,
			Payload:             payload,
			UploadData:          uploadData,
			Context:             ctx,
			TorrentPath:         strings.TrimSpace(input.TorrentPath),
			DefaultDownloaderID: strings.TrimSpace(input.DefaultDownloaderID),
			RootConfig:          input.RootConfig,
		},
		PublishWithContextDeps{
			GetSiteByName:           deps.GetSiteByName,
			ResolveTorrentPath:      deps.ResolveTorrentPath,
			AddToDownloader:         deps.AddToDownloader,
			FindSiteNicknameByGroup: deps.FindSiteNicknameByGroup,
		},
	)
}
