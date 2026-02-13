package persist

import (
	"strings"

	processingtitle "github.com/pt-nexus/server-go/internal/service/processing/title"
)

// BuildUpdatedPreviewData 规范化前端更新后的预览数据结构。
// 参数/返回：updatedData 为前端变更字段，seedID 用于补齐标识；返回可直接给前端展示的预览数据。
// 失败场景：无，字段缺失时按默认值补齐。
// 副作用：无。
func BuildUpdatedPreviewData(updatedData map[string]any, seedID string) map[string]any {
	previewData := map[string]any{}
	for key, value := range updatedData {
		previewData[key] = value
	}
	if _, ok := previewData["seed_id"]; !ok {
		previewData["seed_id"] = strings.TrimSpace(seedID)
	}
	if _, ok := previewData["standardized_params"]; !ok {
		previewData["standardized_params"] = map[string]any{}
	}
	if title := strings.TrimSpace(toStringAny(previewData["original_main_title"], toStringAny(previewData["title"], ""))); title != "" {
		if _, ok := previewData["title_components"]; !ok {
			previewData["title_components"] = processingtitle.BuildSimpleTitleComponents(title, "")
		}
	}
	return previewData
}
