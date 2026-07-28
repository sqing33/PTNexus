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
)

const (
	mediainfoRefreshLogModule = "媒体信息刷新"
)

func (s *MigrateService) refreshMediainfoAsync(payload map[string]any) (map[string]any, int) {
	rootConfig := map[string]any{}
	if s != nil && s.cfg != nil {
		rootConfig = s.cfg.Get()
	}
	s.enrichMediaPayloadSavePath(payload, rootConfig)
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
			ShouldSkipLocalFallback: func(downloaderID string, savePath string, translatedSavePath string) bool {
				return shouldSkipLocalMediaFallback(rootConfig, downloaderID, savePath, translatedSavePath)
			},
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

// enrichMediaPayloadSavePath 从下载器当前任务列表回填可用于媒体处理的真实内容路径。
// 参数/返回：payload 为媒体刷新/截图请求参数，rootConfig 为运行配置；函数原地补齐 save_path/savePath 与 torrent_name/torrentName。
// 失败场景：下载器缺失、连接失败或未匹配任务时仅记录警告，不中断原流程。
// 副作用：会读取下载器任务列表，并可能修改 payload 中的 save_path、savePath、torrent_name、torrentName。
func (s *MigrateService) enrichMediaPayloadSavePath(payload map[string]any, rootConfig map[string]any) {
	if payload == nil {
		return
	}
	downloaderID := firstNonEmptyMediaString(
		toStringAny(payload["downloader_id"]),
		toStringAny(payload["downloaderId"]),
	)
	if downloaderID == "" {
		return
	}
	torrentName := firstNonEmptyMediaString(
		toStringAny(payload["torrent_name"]),
		toStringAny(payload["torrentName"]),
		toStringAny(payload["content_name"]),
		toStringAny(payload["name"]),
	)
	currentSavePath := firstNonEmptyMediaString(
		toStringAny(payload["save_path"]),
		toStringAny(payload["savePath"]),
	)
	seedID := strings.TrimSpace(toStringAny(payload["seed_id"]))
	seedHash := firstNonEmptyMediaString(
		toStringAny(payload["downloader_hash"]),
		toStringAny(payload["downloaderHash"]),
	)
	if seedID != "" {
		if hash, _, _, err := processingpersist.ParseSeedID(seedID); err == nil {
			seedHash = firstNonEmptyMediaString(seedHash, hash)
		}
	}
	if seedHash == "" && torrentName == "" && currentSavePath == "" {
		return
	}
	downloader, err := downloaderclient.FromConfig(rootConfig, downloaderID)
	if err != nil {
		logx.Warnf(mediainfoRefreshLogModule, "媒体路径回填跳过：读取下载器失败 downloader_id=%s err=%v", downloaderID, err)
		return
	}
	snapshots, err := downloader.FetchTorrents()
	if err != nil {
		logx.Warnf(mediainfoRefreshLogModule, "媒体路径回填跳过：拉取下载器任务失败 downloader_id=%s err=%v", downloaderID, err)
		return
	}
	for _, snapshot := range snapshots {
		bestPath := bestSnapshotMediaPath(snapshot)
		if bestPath == "" {
			continue
		}
		matched := false
		if seedHash != "" && strings.EqualFold(seedHash, strings.TrimSpace(snapshot.Hash)) {
			matched = true
		}
		if !matched && torrentName != "" && strings.TrimSpace(snapshot.Name) == torrentName {
			matched = true
		}
		if !matched && currentSavePath != "" && snapshotMatchesContentPath(snapshot, currentSavePath) {
			matched = true
		}
		if !matched {
			continue
		}
		payload["save_path"] = bestPath
		payload["savePath"] = bestPath
		payload["prefer_exact_remote_path"] = strings.TrimSpace(snapshot.ContentPath) != ""
		if strings.TrimSpace(snapshot.Name) != "" {
			payload["torrent_name"] = strings.TrimSpace(snapshot.Name)
			payload["torrentName"] = strings.TrimSpace(snapshot.Name)
		}
		logx.Infof(
			mediainfoRefreshLogModule,
			"媒体路径回填完成 downloader_id=%s hash=%s torrent_name=%s save_path=%s content_path=%s used_path=%s",
			downloaderID,
			snapshot.Hash,
			snapshot.Name,
			snapshot.SavePath,
			snapshot.ContentPath,
			bestPath,
		)
		return
	}
	logx.Warnf(mediainfoRefreshLogModule, "媒体路径回填未命中 downloader_id=%s seed_hash=%s torrent_name=%s", downloaderID, seedHash, torrentName)
}

func bestSnapshotMediaPath(snapshot downloaderclient.TorrentSnapshot) string {
	if path := strings.TrimSpace(snapshot.ContentPath); path != "" {
		return path
	}
	return strings.TrimSpace(snapshot.SavePath)
}

func snapshotMatchesContentPath(snapshot downloaderclient.TorrentSnapshot, path string) bool {
	normalized := normalizeMediaPathForCompare(path)
	if normalized == "" {
		return false
	}
	return normalizeMediaPathForCompare(snapshot.ContentPath) == normalized
}

func shouldSkipLocalMediaFallback(rootConfig map[string]any, downloaderID string, savePath string, translatedSavePath string) bool {
	downloader, err := downloaderclient.FromConfig(rootConfig, strings.TrimSpace(downloaderID))
	if err != nil || !downloader.UseProxy {
		return false
	}
	return normalizeMediaPathForCompare(savePath) == normalizeMediaPathForCompare(translatedSavePath)
}

func normalizeMediaPathForCompare(value string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	for strings.Contains(normalized, "//") {
		normalized = strings.ReplaceAll(normalized, "//", "/")
	}
	return strings.TrimRight(normalized, "/")
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

func firstNonEmptyMediaString(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}
