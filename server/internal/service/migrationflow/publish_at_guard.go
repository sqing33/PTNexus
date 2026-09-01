package migrationflow

import (
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
)

// seedPublishAtLayouts 可发种时间（seed_parameters.publish_at）支持的解析格式。
// 前端写入格式为 ISO 无时区（2006-01-02T15:04:05），同时兼容带空格与 RFC3339 的历史数据。
var seedPublishAtLayouts = []string{
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	time.RFC3339,
}

// parseSeedPublishAt 解析可发种时间文本，为空或格式非法时返回 false。
func parseSeedPublishAt(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range seedPublishAtLayouts {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// resolveSeedPublishAtNotReached 判断种子是否未到可发种时间。
// 参数/返回：torrentID 为源种子 ID；now 为当前时间。返回 (可发种时间, 是否未到)。
// 失败场景：查询失败时放行（仅记录日志），避免数据库异常阻塞全部发种。
// 副作用：仅读取 seed_parameters.publish_at，不修改数据。
func (s *MigrateService) resolveSeedPublishAtNotReached(torrentID string, now time.Time) (time.Time, bool) {
	if s == nil || s.repo == nil {
		return time.Time{}, false
	}
	torrentID = strings.TrimSpace(torrentID)
	if torrentID == "" {
		return time.Time{}, false
	}
	raw, err := s.repo.FindSeedPublishAtByTorrentID(torrentID)
	if err != nil {
		logx.Warnf(publishQueueLogModule, "查询可发种时间失败，放行发种 torrent_id=%s err=%v", torrentID, err)
		return time.Time{}, false
	}
	publishAt, ok := parseSeedPublishAt(raw)
	if !ok {
		return time.Time{}, false
	}
	return publishAt, now.Before(publishAt)
}

// formatSeedPublishAt 格式化可发种时间用于日志/提示。
func formatSeedPublishAt(t time.Time) string {
	return t.Format(repository.PublishQueueTimeLayout)
}

// publishAtBlockMessage 构造"未到可发种时间"的提示文案。
func publishAtBlockMessage(publishAt time.Time) string {
	return "未到可发种时间（" + formatSeedPublishAt(publishAt) + "），不能发种"
}
