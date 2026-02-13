package migrationflow

import (
	"strings"

	"github.com/pt-nexus/server-go/internal/platform/logx"
	processingpersist "github.com/pt-nexus/server-go/internal/service/processing/persist"
	processingshared "github.com/pt-nexus/server-go/internal/service/processing/shared"
)

const updateDBSeedLogModule = "迁移-种子更新"

// UpdateDBSeedInfo 写入用户在转种面板中手工调整后的参数，并生成标准化参数供发布映射使用。
// 参数/返回：payload 包含 torrent_id/site_name/updated_parameters 等；返回标准化参数与映射预览数据。
// 失败场景：缺少必要参数、读取/写入数据库失败。
// 副作用：覆盖写入 seed_parameters（对齐历史行为：Delete+Create），并将 is_reviewed 置为 true。
func (s *MigrateService) UpdateDBSeedInfo(payload map[string]any) (map[string]any, int) {
	result, status := processingpersist.ApplyManualUpdateFromPayload(
		s.repo,
		payload,
		s.newID,
		s.reverseMappings(),
	)
	if status >= 500 {
		torrentID := strings.TrimSpace(processingshared.ToString(payload["torrent_id"], ""))
		siteName := strings.TrimSpace(processingshared.ToString(payload["site_name"], ""))
		logx.Errorf(updateDBSeedLogModule, "写入 seed_parameters 失败 torrent_id=%s site_name=%s message=%s", torrentID, siteName, processingshared.ToString(result["message"], ""))
	}
	return result, status
}
