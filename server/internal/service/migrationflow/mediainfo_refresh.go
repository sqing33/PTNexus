package migrationflow

import (
	"fmt"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
)

const (
	mediainfoRefreshLogModule = "媒体信息刷新"
)

func (s *MigrateService) refreshMediainfoAsync(payload map[string]any) (map[string]any, int) {
	rootConfig := map[string]any{}
	if s != nil && s.cfg != nil {
		rootConfig = s.cfg.Get()
	}
	return processingmedia.RefreshMediainfoAsync(
		payload,
		s.repo,
		processingmedia.RefreshMediainfoDeps{
			LogModule:       mediainfoRefreshLogModule,
			ParseSeedID:     processingpersist.ParseSeedID,
			StartBDInfoTask: s.startBDInfoTask,
			FetchProxyMediaInfo: func(downloaderID, remotePath, contentName string) processingmedia.ProxyMediaInfoProbe {
				trimmedID := strings.TrimSpace(downloaderID)
				trimmedPath := strings.TrimSpace(remotePath)
				if trimmedID == "" || trimmedPath == "" {
					return processingmedia.ProxyMediaInfoProbe{StatusCode: 0}
				}

				downloader, decision, err := downloaderclient.DecideProxy(rootConfig, trimmedID)
				if !decision.Enabled {
					if err != nil {
						logx.Warnf(mediainfoRefreshLogModule, "盒子代理提取跳过：downloader_id=%s reason=%s err=%v", trimmedID, strings.TrimSpace(decision.Reason), err)
					}
					return processingmedia.ProxyMediaInfoProbe{StatusCode: 0}
				}

				mediaInfo, isBDMV, err := downloader.FetchMediaInfoByProxy(trimmedPath, strings.TrimSpace(contentName))
				if err != nil {
					if apiErr, ok := err.(*downloaderclient.ProxyAPIError); ok && apiErr != nil {
						return processingmedia.ProxyMediaInfoProbe{
							StatusCode: apiErr.StatusCode,
							Success:    false,
							Message:    apiErr.Message,
						}
					}
					return processingmedia.ProxyMediaInfoProbe{
						StatusCode: 500,
						Success:    false,
						Message:    err.Error(),
					}
				}
				if isBDMV {
					return processingmedia.ProxyMediaInfoProbe{
						StatusCode: 200,
						Success:    true,
						IsBDMV:     true,
						Message:    "检测到蓝光原盘目录",
					}
				}
				return processingmedia.ProxyMediaInfoProbe{
					StatusCode: 200,
					Success:    true,
					MediaInfo:  strings.TrimSpace(mediaInfo),
					IsBDMV:     false,
					Message:    "MediaInfo 获取成功",
				}
			},
			TranslateDownloaderPath: func(downloaderID string, savePath string) string {
				return strings.TrimSpace(downloaderclient.TranslateDownloaderPath(rootConfig, downloaderID, savePath))
			},
			ResolveMediaTargetFile: processingrepair.ResolveMediaTargetFile,
			AfterPersist: func(hash, torrentID, siteName string, row map[string]any, savePath string, torrentName string, mediainfo string) {
				now := time.Now()
				seedID := processingpersist.ComposeSeedID(hash, torrentID, siteName)
				if processingpersist.BoolFromAny(row["is_reviewed"]) {
					logx.Infof(mediainfoRefreshLogModule, "标题组件回写跳过：seed_id=%s is_reviewed=true", seedID)
				} else {
					processingpersist.RewriteSeedTitleComponentsByMediaInfo(
						mediainfoRefreshLogModule,
						s.repo,
						hash,
						torrentID,
						siteName,
						now,
						row,
						mediainfo,
					)
				}

				title := strings.TrimSpace(toStringAny(row["title"]))
				if title == "" {
					title = strings.TrimSpace(toStringAny(row["name"]))
				}
				body := strings.TrimSpace(toStringAny(row["body"]))
				inferred := parser.InferStandardizedValues(title, strings.TrimSpace(mediainfo), body)
				standardUpdates := map[string]any{}
				for _, key := range []string{"video_codec", "audio_codec", "resolution"} {
					candidate := strings.TrimSpace(inferred[key])
					if candidate == "" || strings.HasSuffix(candidate, ".other") {
						continue
					}
					current := strings.TrimSpace(toStringAny(row[key]))
					if current == candidate {
						continue
					}
					standardUpdates[key] = candidate
				}
				if len(standardUpdates) > 0 {
					standardUpdates["updated_at"] = now.Format("2006-01-02 15:04:05")
					if err := s.repo.UpdateSeedParameterByKey(hash, torrentID, siteName, standardUpdates); err != nil {
						logx.Warnf(
							mediainfoRefreshLogModule,
							"标准参数回写失败：seed_id=%s video_codec=%s audio_codec=%s resolution=%s err=%v",
							seedID,
							strings.TrimSpace(toStringAny(standardUpdates["video_codec"])),
							strings.TrimSpace(toStringAny(standardUpdates["audio_codec"])),
							strings.TrimSpace(toStringAny(standardUpdates["resolution"])),
							err,
						)
					} else {
						logx.Infof(
							mediainfoRefreshLogModule,
							"标准参数回写完成：seed_id=%s video_codec=%s audio_codec=%s resolution=%s",
							seedID,
							strings.TrimSpace(toStringAny(standardUpdates["video_codec"])),
							strings.TrimSpace(toStringAny(standardUpdates["audio_codec"])),
							strings.TrimSpace(toStringAny(standardUpdates["resolution"])),
						)
					}
				}
				// 对齐 Python：媒体文本刷新后，重新补全 tags 并写回（仅未审核时生效）。
				s.recomputeAndPersistTags(hash, torrentID, siteName, savePath, torrentName, "MediaInfo刷新")
			},
		},
	)
}

func toStringAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprintf("%v", value)
	}
}
