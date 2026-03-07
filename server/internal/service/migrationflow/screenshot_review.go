package migrationflow

import (
	"errors"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	"gorm.io/gorm"
)

const screenshotReviewLogModule = "迁移-截图审查"

// UpdateScreenshotReviewStatus 更新数据库中截图人工确认状态。
// 参数/返回：payload 需要包含 torrent_id、site_name，可选 screenshot_review_status；返回更新后的状态。
// 失败场景：缺少必要参数、种子记录不存在或数据库写入失败时返回对应错误。
// 副作用：会更新 seed_parameters 的 screenshot_review_status 与 updated_at。
func (s *MigrateService) UpdateScreenshotReviewStatus(payload map[string]any) (map[string]any, int) {
	torrentID := strings.TrimSpace(processingshared.ToString(payload["torrent_id"], ""))
	siteName := strings.TrimSpace(processingshared.ToString(payload["site_name"], ""))
	status := processingshared.NormalizeScreenshotReviewStatus(
		processingshared.ToString(payload["screenshot_review_status"], processingshared.ScreenshotReviewStatusConfirmed),
	)
	if torrentID == "" || siteName == "" {
		return map[string]any{"success": false, "message": "错误：torrent_id 和 site_name 不能为空"}, 400
	}

	row, err := s.repo.GetSeedParameter(torrentID, siteName)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logx.Warnf(screenshotReviewLogModule, "更新失败：记录不存在 torrent_id=%s site_name=%s", torrentID, siteName)
			return map[string]any{"success": false, "message": "未找到对应种子记录"}, 404
		}
		logx.Errorf(screenshotReviewLogModule, "读取记录失败 torrent_id=%s site_name=%s err=%v", torrentID, siteName, err)
		return map[string]any{"success": false, "message": "读取种子记录失败: " + err.Error()}, 500
	}

	hash := strings.TrimSpace(processingshared.ToString(row["hash"], ""))
	resolvedSiteName := strings.TrimSpace(processingshared.ToString(row["site_name"], siteName))
	if hash == "" {
		logx.Errorf(screenshotReviewLogModule, "更新失败：缺少 hash torrent_id=%s site_name=%s", torrentID, siteName)
		return map[string]any{"success": false, "message": "种子记录缺少 hash，无法更新截图确认状态"}, 500
	}

	updates := map[string]any{
		"screenshot_review_status": status,
		"updated_at":               time.Now().Format("2006-01-02 15:04:05"),
	}
	if err := s.repo.UpdateSeedParameterByKey(hash, torrentID, resolvedSiteName, updates); err != nil {
		logx.Errorf(screenshotReviewLogModule, "写入失败 hash=%s torrent_id=%s site_name=%s status=%s err=%v", hash, torrentID, resolvedSiteName, status, err)
		return map[string]any{"success": false, "message": "更新截图确认状态失败: " + err.Error()}, 500
	}

	logx.Infof(screenshotReviewLogModule, "更新成功 hash=%s torrent_id=%s site_name=%s status=%s", hash, torrentID, resolvedSiteName, status)
	return map[string]any{
		"success":                  true,
		"message":                  "截图确认状态已更新",
		"screenshot_review_status": status,
	}, 200
}
