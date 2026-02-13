package bdflow

import (
	"strings"
	"time"

	processingpersist "github.com/pt-nexus/server-go/internal/service/processing/persist"
)

// BDInfoCallbackRepo 定义 BDInfo 回调落库所需的最小仓储接口。
type BDInfoCallbackRepo interface {
	UpdateSeedParameterByKey(hash, torrentID, siteName string, updates map[string]any) error
}

// PersistBDInfoCompleteBySeedID 将 BDInfo 回调结果按 seed_id 回写到 seed_parameters。
// 参数/返回：repo 为数据库依赖，seedID 为 hash_torrentId_siteName，now 为时间戳；无返回值。
// 失败场景：seed_id 非法或写库失败时会静默返回，不阻断上层回调。
// 副作用：更新 mediainfo_status/bdinfo_error/bdinfo_completed_at/updated_at，以及成功时的 mediainfo。
func PersistBDInfoCompleteBySeedID(repo BDInfoCallbackRepo, seedID string, success bool, mediaInfo string, errorMessage string, now time.Time) {
	if repo == nil {
		return
	}
	hash, torrentID, siteName, err := processingpersist.ParseSeedID(seedID)
	if err != nil {
		return
	}
	timestamp := now
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	timeText := timestamp.Format("2006-01-02 15:04:05")

	updates := map[string]any{
		"mediainfo_status":    "failed",
		"bdinfo_error":        strings.TrimSpace(errorMessage),
		"bdinfo_completed_at": timeText,
		"updated_at":          timeText,
	}
	if success {
		updates["mediainfo_status"] = "completed"
		updates["mediainfo"] = strings.TrimSpace(mediaInfo)
		updates["bdinfo_error"] = ""
	}
	_ = repo.UpdateSeedParameterByKey(hash, torrentID, siteName, updates)
}
