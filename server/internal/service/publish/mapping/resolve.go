package mapping

import "strings"

// MappingContext 定义站点特殊映射所需的上下文依赖。
type MappingContext struct {
	SourceSiteNickname      string
	FindSiteNicknameByGroup func(releaseGroup string) (string, error)
}

// ResolvePublishMappings 根据站点 code 与上下文生成发布表单字段映射（包含站点特殊规则）。
func ResolvePublishMappings(siteCode string, uploadData map[string]any, ctx MappingContext) map[string]string {
	mapped := ResolveBasicPublishMappings(siteCode, uploadData)

	if strings.EqualFold(strings.TrimSpace(siteCode), "hdfans") {
		applyHdfansOverrides(mapped, uploadData, ctx)
	}

	return mapped
}
