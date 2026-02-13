package repair

import (
	"strings"
)

// MediaValidateEntryDeps 定义媒体校验入口流程依赖。
type MediaValidateEntryDeps struct {
	GetRootConfig func() map[string]any
	GetCSPTToken  func() string
	GetSeedMedia  func(seedID string) string
}

// MediaValidateEntry 执行媒体校验入口流程，统一拼装运行配置与外部依赖。
// 参数/返回：payload 为校验参数；deps 注入配置与媒体回填能力；返回接口响应与状态码。
// 失败场景：缺少必要依赖时按空配置降级执行，具体错误由 ValidateMediaPayload 返回。
// 副作用：可能触发截图生成上传和 PTGen 请求。
func MediaValidateEntry(payload map[string]any, deps MediaValidateEntryDeps) (map[string]any, int) {
	rootConfig := map[string]any{}
	if deps.GetRootConfig != nil {
		if cfg := deps.GetRootConfig(); cfg != nil {
			rootConfig = cfg
		}
	}
	csptToken := ""
	if deps.GetCSPTToken != nil {
		csptToken = strings.TrimSpace(deps.GetCSPTToken())
	}
	return ValidateMediaPayload(payload, rootConfig, csptToken, MediaValidateDeps{
		GetSeedMediaInfo: deps.GetSeedMedia,
	})
}
