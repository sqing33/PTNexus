package migrationflow

import (
	"fmt"
	"strings"
	"time"

	"github.com/pt-nexus/server-go/internal/platform/logx"
	parser "github.com/pt-nexus/server-go/internal/service/acquire/extract"
	"github.com/pt-nexus/server-go/internal/service/downloaderclient"
	processingmedia "github.com/pt-nexus/server-go/internal/service/processing/media"
	processingpersist "github.com/pt-nexus/server-go/internal/service/processing/persist"
	processingrepair "github.com/pt-nexus/server-go/internal/service/processing/repair"
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
				if processingpersist.BoolFromAny(row["is_reviewed"]) {
					logx.Infof(mediainfoRefreshLogModule, "标题组件回写跳过：seed_id=%s is_reviewed=true", processingpersist.ComposeSeedID(hash, torrentID, siteName))
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

					currentAudioCodec := strings.TrimSpace(toStringAny(row["audio_codec"]))
					if currentAudioCodec == "" || strings.HasSuffix(currentAudioCodec, ".other") {
						title := strings.TrimSpace(toStringAny(row["title"]))
						body := strings.TrimSpace(toStringAny(row["body"]))
						inferred := parser.InferStandardizedValues(title, strings.TrimSpace(mediainfo), body)
						candidate := strings.TrimSpace(inferred["audio_codec"])
						if candidate != "" && candidate != "audio.other" {
							if err := s.repo.UpdateSeedParameterByKey(hash, torrentID, siteName, map[string]any{
								"audio_codec": candidate,
								"updated_at":  now.Format("2006-01-02 15:04:05"),
							}); err != nil {
								logx.Warnf(mediainfoRefreshLogModule, "音频编码回写失败：seed_id=%s audio_codec=%s err=%v", processingpersist.ComposeSeedID(hash, torrentID, siteName), candidate, err)
							} else {
								logx.Infof(mediainfoRefreshLogModule, "音频编码回写完成：seed_id=%s audio_codec=%s", processingpersist.ComposeSeedID(hash, torrentID, siteName), candidate)
							}
						} else if candidate != "" && candidate == "audio.other" {
							logx.Infof(mediainfoRefreshLogModule, "音频编码回写跳过：seed_id=%s inferred=audio.other", processingpersist.ComposeSeedID(hash, torrentID, siteName))
						}
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
