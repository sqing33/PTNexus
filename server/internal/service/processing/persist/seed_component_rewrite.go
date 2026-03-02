package persist

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
	processingtitle "github.com/pt-nexus/server/internal/service/processing/title"
)

const seedComponentRewriteLogModule = "媒体信息刷新"

// SeedParameterUpdater 定义媒体信息刷新时需要的最小写库接口。
type SeedParameterUpdater interface {
	UpdateSeedParameterByKey(hash, torrentID, siteName string, updates map[string]any) error
}

// RewriteSeedTitleComponentsByMediaInfo 使用媒体文本重建标题组件并回写数据库（仅调用方允许时执行）。
// 参数/返回：row 为 seed_parameters 当前行；repo 为写库接口；返回是否执行了更新与是否命中媒体格式。
// 失败场景：标题为空、媒体格式未命中、序列化失败或写库失败时返回 false。
// 副作用：可能写入 seed_parameters.title_components 与 seed_parameters.medium。
func RewriteSeedTitleComponentsByMediaInfo(
	logModule string,
	repo SeedParameterUpdater,
	hash string,
	torrentID string,
	siteName string,
	now time.Time,
	row map[string]any,
	mediaInfoText string,
) (bool, bool, bool) {
	if repo == nil || row == nil {
		return false, false, false
	}
	if strings.TrimSpace(logModule) == "" {
		logModule = seedComponentRewriteLogModule
	}

	title := strings.TrimSpace(toStringSimple(row["title"]))
	if title == "" {
		title = strings.TrimSpace(toStringSimple(row["name"]))
	}
	result := processingtitle.BuildTitleComponentsForStorage(title, mediaInfoText, processingtitle.BuildSimpleTitleComponentsWithMediaInfo)
	if !(result.IsMediainfo || result.IsBDInfo) {
		logx.Warnf(logModule, "标题组件回写跳过：seed_id=%s_%s_%s 媒体格式未命中 reason=%s", hash, torrentID, siteName, result.Reason)
		return false, false, false
	}
	if len(result.Components) == 0 {
		logx.Warnf(logModule, "标题组件回写跳过：seed_id=%s_%s_%s 解析结果为空", hash, torrentID, siteName)
		return false, true, false
	}
	encoded, err := json.Marshal(result.Components)
	if err != nil {
		logx.Warnf(logModule, "标题组件回写失败：seed_id=%s_%s_%s 序列化失败 err=%v", hash, torrentID, siteName, err)
		return false, true, false
	}

	logx.Infof(logModule, "标题组件回写开始：seed_id=%s_%s_%s is_mediainfo=%t is_bdinfo=%t reason=%s", hash, torrentID, siteName, result.IsMediainfo, result.IsBDInfo, result.Reason)
	writeErr := repo.UpdateSeedParameterByKey(hash, torrentID, siteName, map[string]any{
		"title_components": string(encoded),
		"updated_at":       now.Format("2006-01-02 15:04:05"),
	})
	if writeErr != nil {
		logx.Warnf(logModule, "标题组件回写失败：seed_id=%s_%s_%s err=%v", hash, torrentID, siteName, writeErr)
		return false, true, false
	}
	logx.Infof(logModule, "标题组件回写完成：seed_id=%s_%s_%s components=%d", hash, torrentID, siteName, len(result.Components))
	mediaType := "BDInfo"
	if result.IsMediainfo {
		mediaType = "MediaInfo"
	}
	for _, item := range result.Components {
		if strings.TrimSpace(toStringSimple(item["key"])) != "媒介" {
			continue
		}
		value := strings.TrimSpace(toStringSimple(item["value"]))
		if value != "" {
			logx.Infof(logModule, "标题组件回写媒介结果：seed_id=%s_%s_%s value=%s media_type=%s", hash, torrentID, siteName, value, mediaType)
		}
		break
	}

	mediumBefore := strings.TrimSpace(toStringSimple(row["medium"]))
	mediumAfter := processingmedia.NormalizeMediumByMediaType(mediumBefore, result.IsMediainfo, result.IsBDInfo)
	if strings.TrimSpace(mediumAfter) != "" && strings.TrimSpace(mediumAfter) != mediumBefore {
		logx.Infof(
			logModule,
			"媒介标准键纠偏开始：seed_id=%s_%s_%s medium_before=%s medium_after=%s media_type=%s",
			hash,
			torrentID,
			siteName,
			mediumBefore,
			mediumAfter,
			func() string {
				if result.IsMediainfo {
					return "MediaInfo"
				}
				return "BDInfo"
			}(),
		)
		mediumErr := repo.UpdateSeedParameterByKey(hash, torrentID, siteName, map[string]any{
			"medium":     mediumAfter,
			"updated_at": now.Format("2006-01-02 15:04:05"),
		})
		if mediumErr != nil {
			logx.Warnf(logModule, "媒介标准键纠偏失败：seed_id=%s_%s_%s err=%v", hash, torrentID, siteName, mediumErr)
		} else {
			logx.Infof(logModule, "媒介标准键纠偏完成：seed_id=%s_%s_%s", hash, torrentID, siteName)
		}
	}
	return true, true, result.IsBDInfo
}

func toStringSimple(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}
