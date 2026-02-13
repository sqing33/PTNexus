package workflow

import (
	"fmt"
	"strings"

	processingshared "github.com/pt-nexus/server-go/internal/service/processing/shared"
)

// OneStepMigrateDeps 定义一步式迁移执行依赖。
type OneStepMigrateDeps struct {
	Publish func(payload map[string]any) (map[string]any, int)
}

// ExecuteOneStepMigrate 执行“一步式迁移”流程：参数校验后复用发布流程。
// 参数/返回：payload 为前端迁移参数；deps 注入发布函数；返回迁移结果与状态码。
// 失败场景：缺少 source/target/search 参数、源目标站相同、发布失败。
// 副作用：会调用发布流程并触发上传。
func ExecuteOneStepMigrate(payload map[string]any, deps OneStepMigrateDeps) (map[string]any, int) {
	sourceSite := strings.TrimSpace(processingshared.ToString(payload["sourceSite"], ""))
	targetSite := strings.TrimSpace(processingshared.ToString(payload["targetSite"], ""))
	searchTerm := strings.TrimSpace(processingshared.ToString(payload["searchTerm"], ""))

	if sourceSite == "" || targetSite == "" || searchTerm == "" {
		return map[string]any{"success": false, "logs": "错误：源站点、目标站点和搜索词不能为空。"}, 400
	}
	if strings.EqualFold(sourceSite, targetSite) {
		return map[string]any{"success": false, "logs": "错误：源站点和目标站点不能相同。"}, 400
	}
	if deps.Publish == nil {
		return map[string]any{"success": false, "logs": "错误：发布服务不可用。"}, 500
	}

	publishPayload := map[string]any{}
	for key, value := range payload {
		publishPayload[key] = value
	}
	publishPayload["targetSite"] = targetSite
	result, status := deps.Publish(publishPayload)
	if status != 200 {
		if _, ok := result["logs"]; !ok {
			result["logs"] = processingshared.ToString(result["message"], "一步式迁移失败")
		}
		result["success"] = false
		return result, status
	}

	result["logs"] = fmt.Sprintf("一步式迁移执行完成：%s -> %s（关键词：%s）", sourceSite, targetSite, searchTerm)
	result["source_site"] = sourceSite
	result["target_site"] = targetSite
	result["search_term"] = searchTerm
	return result, 200
}
