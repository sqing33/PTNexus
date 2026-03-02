package media

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
)

// RefreshMediainfoRepo 定义媒体刷新流程的最小仓储接口。
type RefreshMediainfoRepo interface {
	GetSeedParameterByKey(hash, torrentID, siteName string) (map[string]any, error)
	UpdateSeedParameterByKey(hash, torrentID, siteName string, updates map[string]any) error
}

// RefreshMediainfoDeps 定义媒体刷新流程依赖注入。
type RefreshMediainfoDeps struct {
	LogModule string

	ParseSeedID     func(seedID string) (string, string, string, error)
	StartBDInfoTask func(seedID string, force bool) (string, error)

	// FetchProxyMediaInfo 允许在本机无法访问媒体文件时，通过盒子代理远程提取 MediaInfo。
	// 参数/返回：downloaderID 为下载器 ID，remotePath 为盒子侧真实路径；contentName 用于多文件时辅助选取；返回探测结果。
	// 失败场景：代理不可达/返回异常时应返回 Success=false，并设置 StatusCode/Message 供上层判断是否继续回退。
	// 副作用：可能发起 HTTP 请求到盒子代理。
	FetchProxyMediaInfo func(downloaderID, remotePath, contentName string) ProxyMediaInfoProbe

	TranslateDownloaderPath func(downloaderID string, savePath string) string
	ResolveMediaTargetFile  func(translatedSavePath string, torrentName string, contentName string) (string, error)

	AfterPersist func(hash, torrentID, siteName string, row map[string]any, savePath string, torrentName string, mediainfo string)
}

// ProxyMediaInfoProbe 描述盒子代理媒体信息提取的探测结果。
// 参数/返回：StatusCode 为 HTTP/逻辑状态码（0 表示“未执行/跳过”）；Success 表示是否获得有效结果；MediaInfo 为提取到的文本；IsBDMV 表示蓝光原盘目录提示。
// 失败场景：无。
// 副作用：无。
type ProxyMediaInfoProbe struct {
	StatusCode int
	Success    bool
	Message    string
	MediaInfo  string
	IsBDMV     bool
}

// RefreshMediainfoAsync 执行媒体信息刷新：优先蓝光判定转 BDInfo，否则回退 MediaInfo 提取并写库。
// 参数/返回：payload 为刷新请求参数；repo 与 deps 注入外部能力；返回接口响应与状态码。
// 失败场景：缺少 seed_id/save_path、目标文件定位失败、MediaInfo 提取失败、BDInfo 启动失败。
// 副作用：可能更新数据库 mediainfo 字段，或启动异步 BDInfo 任务。
func RefreshMediainfoAsync(payload map[string]any, repo RefreshMediainfoRepo, deps RefreshMediainfoDeps) (map[string]any, int) {
	logModule := strings.TrimSpace(deps.LogModule)
	if logModule == "" {
		logModule = "媒体信息刷新"
	}

	seedID := strings.TrimSpace(toStringWithDefault(payload["seed_id"], ""))
	if seedID == "" {
		logx.Errorf(logModule, "参数校验失败：缺少 seed_id")
		return map[string]any{"success": false, "message": "缺少 seed_id 参数"}, 400
	}

	savePath := strings.TrimSpace(toStringWithDefault(payload["save_path"], toStringWithDefault(payload["savePath"], "")))
	contentName := strings.TrimSpace(toStringWithDefault(payload["content_name"], ""))
	downloaderID := strings.TrimSpace(toStringWithDefault(payload["downloader_id"], toStringWithDefault(payload["downloaderId"], "")))
	torrentName := strings.TrimSpace(toStringWithDefault(payload["torrent_name"], toStringWithDefault(payload["torrentName"], "")))
	currentMediainfo := strings.TrimSpace(toStringWithDefault(payload["current_mediainfo"], toStringWithDefault(payload["mediainfo"], "")))
	logx.Infof(
		logModule,
		"开始刷新：seed_id=%s save_path=%s torrent_name=%s content_name=%s downloader_id=%s",
		seedID, savePath, torrentName, contentName, downloaderID,
	)

	hash, torrentID, siteName, parseErr := "", "", "", error(nil)
	if deps.ParseSeedID != nil {
		hash, torrentID, siteName, parseErr = deps.ParseSeedID(seedID)
	}

	if (savePath == "" || torrentName == "") && parseErr == nil && repo != nil {
		logx.Infof(logModule, "尝试参数回填：seed_id=%s save_path_empty=%t torrent_name_empty=%t", seedID, savePath == "", torrentName == "")
		if row, rowErr := repo.GetSeedParameterByKey(hash, torrentID, siteName); rowErr == nil {
			if savePath == "" {
				savePath = strings.TrimSpace(toStringWithDefault(row["save_path"], ""))
			}
			if torrentName == "" {
				torrentName = strings.TrimSpace(toStringWithDefault(row["name"], ""))
			}
			logx.Infof(logModule, "参数回填完成：seed_id=%s save_path=%s torrent_name=%s", seedID, savePath, torrentName)
		} else {
			logx.Warnf(logModule, "参数回填失败：seed_id=%s 数据库读取失败 err=%v", seedID, rowErr)
		}
	}

	if savePath == "" {
		logx.Errorf(logModule, "参数校验失败：seed_id=%s 缺少 save_path", seedID)
		return map[string]any{"success": false, "message": "缺少 save_path 参数"}, 400
	}

	// 优先尝试盒子代理远程提取 MediaInfo：适用于 downloader.use_proxy=true 且本机不挂载媒体目录的场景。
	// 注意：此处使用原始 save_path 作为 remote_path 候选，避免被本地路径映射污染。
	if deps.FetchProxyMediaInfo != nil && strings.TrimSpace(downloaderID) != "" {
		remoteCandidates := buildRemotePathCandidates(savePath, torrentName, contentName)
		for _, remoteCandidate := range remoteCandidates {
			probe := deps.FetchProxyMediaInfo(downloaderID, remoteCandidate, contentName)
			if probe.StatusCode == 0 {
				// 回调选择跳过，继续走本地逻辑。
				break
			}
			if probe.Success && probe.IsBDMV {
				logx.Warnf(logModule, "盒子代理提示蓝光原盘：seed_id=%s remote_path=%s message=%s", seedID, remoteCandidate, strings.TrimSpace(probe.Message))
				if deps.StartBDInfoTask == nil {
					logx.Errorf(logModule, "启动BDInfo失败：seed_id=%s reason=StartBDInfoTask回调为空", seedID)
					return map[string]any{"success": false, "message": "BDInfo 服务未注册"}, 500
				}
				taskID, err := deps.StartBDInfoTask(seedID, true)
				if err != nil {
					logx.Errorf(logModule, "启动BDInfo失败：seed_id=%s remote_path=%s err=%v", seedID, remoteCandidate, err)
					return map[string]any{"success": false, "message": err.Error()}, 500
				}
				logx.Infof(logModule, "刷新结束：seed_id=%s route=ProxyDetectBDMV task_id=%s remote_path=%s", seedID, taskID, remoteCandidate)
				return map[string]any{
					"success":             true,
					"mediainfo":           currentMediainfo,
					"is_mediainfo":        false,
					"is_bdinfo":           true,
					"message":             "检测到蓝光原盘目录，BDInfo 正在后台处理中...",
					"detected_by":         "proxy_media",
					"detected_media_type": "bdmv",
					"resolved_path":       remoteCandidate,
					"bdinfo_async": map[string]any{
						"bdinfo_status":  "processing",
						"bdinfo_task_id": taskID,
						"task_id":        taskID,
						"is_bluray":      true,
					},
				}, 200
			}
			if probe.Success && strings.TrimSpace(probe.MediaInfo) != "" {
				mediainfoText := strings.TrimSpace(probe.MediaInfo)
				seedUpdates, _ := persistMediainfoAndCollectUpdates(seedID, parseErr, repo, deps, hash, torrentID, siteName, savePath, torrentName, mediainfoText, logModule)
				seedUpdatesValue := any(nil)
				if len(seedUpdates) > 0 {
					seedUpdatesValue = seedUpdates
				}
				logx.Infof(logModule, "刷新结束：seed_id=%s route=ProxyMediaInfo remote_path=%s output_bytes=%d", seedID, remoteCandidate, len(mediainfoText))
				return map[string]any{
					"success":             true,
					"mediainfo":           mediainfoText,
					"seed_updates":        seedUpdatesValue,
					"is_mediainfo":        true,
					"is_bdinfo":           false,
					"message":             "MediaInfo 更新完成（盒子代理）",
					"detected_by":         "proxy_media",
					"detected_media_type": "mediainfo",
					"resolved_path":       remoteCandidate,
					"bdinfo_async": map[string]any{
						"bdinfo_status":  "skipped",
						"bdinfo_task_id": nil,
						"is_bluray":      false,
					},
				}, 200
			}
			if probe.StatusCode >= 400 {
				logx.Warnf(logModule, "盒子代理提取失败：seed_id=%s remote_path=%s status=%d message=%s", seedID, remoteCandidate, probe.StatusCode, strings.TrimSpace(probe.Message))
				// 对于 400（路径不存在/未找到视频文件）继续尝试下一个候选；其他错误直接回退本地逻辑。
				if probe.StatusCode == 400 {
					continue
				}
				break
			}
			// 未明确成功也未明确失败，直接回退本地逻辑。
			break
		}
	}

	translatedSavePath := savePath
	if deps.TranslateDownloaderPath != nil {
		translatedSavePath = strings.TrimSpace(deps.TranslateDownloaderPath(downloaderID, savePath))
	}
	if translatedSavePath == "" {
		translatedSavePath = savePath
	}
	logx.Infof(
		logModule,
		"路径映射完成：seed_id=%s original_save_path=%s translated_save_path=%s",
		seedID, savePath, translatedSavePath,
	)

	isBluray, detectedPath := DetectBlurayDiscByCandidates(translatedSavePath, torrentName, contentName)
	if isBluray {
		logx.Warnf(logModule, "蓝光判定命中：seed_id=%s resolved_path=%s action=启动BDInfo", seedID, detectedPath)
		if deps.StartBDInfoTask == nil {
			logx.Errorf(logModule, "启动BDInfo失败：seed_id=%s reason=StartBDInfoTask回调为空", seedID)
			return map[string]any{"success": false, "message": "BDInfo 服务未注册"}, 500
		}
		taskID, err := deps.StartBDInfoTask(seedID, true)
		if err != nil {
			logx.Errorf(logModule, "启动BDInfo失败：seed_id=%s resolved_path=%s err=%v", seedID, detectedPath, err)
			return map[string]any{"success": false, "message": err.Error()}, 500
		}
		logx.Infof(logModule, "刷新结束：seed_id=%s route=BDInfo task_id=%s resolved_path=%s", seedID, taskID, detectedPath)
		return map[string]any{
			"success":             true,
			"mediainfo":           currentMediainfo,
			"is_mediainfo":        false,
			"is_bdinfo":           true,
			"message":             "检测到蓝光原盘目录，BDInfo 正在后台处理中...",
			"detected_by":         "path_structure",
			"detected_media_type": "bdmv",
			"resolved_path":       detectedPath,
			"bdinfo_async": map[string]any{
				"bdinfo_status":  "processing",
				"bdinfo_task_id": taskID,
				"task_id":        taskID,
				"is_bluray":      true,
			},
		}, 200
	}
	logx.Infof(logModule, "蓝光判定未命中：seed_id=%s resolved_path=%s action=提取MediaInfo", seedID, detectedPath)

	if deps.ResolveMediaTargetFile == nil {
		return map[string]any{"success": false, "message": "媒体目标解析器未注册"}, 500
	}
	targetFile, err := deps.ResolveMediaTargetFile(translatedSavePath, torrentName, contentName)
	if err != nil {
		logx.Errorf(logModule, "定位媒体文件失败：seed_id=%s translated_save_path=%s err=%v", seedID, translatedSavePath, err)
		return map[string]any{"success": false, "message": err.Error()}, 400
	}

	mediainfoText, err := ExtractMediaInfo(targetFile)
	if err != nil {
		logx.Errorf(logModule, "提取MediaInfo失败：seed_id=%s target_file=%s err=%v", seedID, targetFile, err)
		return map[string]any{"success": false, "message": err.Error()}, 500
	}

	_, seedUpdatesValue := persistMediainfoAndCollectUpdates(seedID, parseErr, repo, deps, hash, torrentID, siteName, savePath, torrentName, mediainfoText, logModule)

	logx.Infof(logModule, "刷新结束：seed_id=%s route=MediaInfo target_file=%s output_bytes=%d", seedID, targetFile, len(mediainfoText))
	return map[string]any{
		"success":             true,
		"mediainfo":           mediainfoText,
		"seed_updates":        seedUpdatesValue,
		"is_mediainfo":        true,
		"is_bdinfo":           false,
		"message":             "MediaInfo 更新完成",
		"detected_by":         "path_structure",
		"detected_media_type": "mediainfo",
		"resolved_path":       detectedPath,
		"bdinfo_async": map[string]any{
			"bdinfo_status":  "skipped",
			"bdinfo_task_id": nil,
			"is_bluray":      false,
		},
	}, 200
}

func buildRemotePathCandidates(savePath, torrentName, contentName string) []string {
	trimmedSavePath := strings.TrimSpace(savePath)
	trimmedTorrentName := strings.TrimSpace(torrentName)
	trimmedContentName := strings.TrimSpace(contentName)

	candidates := make([]string, 0, 3)
	if trimmedSavePath != "" && trimmedTorrentName != "" {
		candidates = append(candidates, filepath.Join(trimmedSavePath, trimmedTorrentName))
	}
	if trimmedSavePath != "" && trimmedContentName != "" && !strings.EqualFold(trimmedContentName, trimmedTorrentName) {
		candidates = append(candidates, filepath.Join(trimmedSavePath, trimmedContentName))
	}
	if trimmedSavePath != "" {
		candidates = append(candidates, trimmedSavePath)
	}
	return candidates
}

func persistMediainfoAndCollectUpdates(
	seedID string,
	parseErr error,
	repo RefreshMediainfoRepo,
	deps RefreshMediainfoDeps,
	hash string,
	torrentID string,
	siteName string,
	savePath string,
	torrentName string,
	mediainfoText string,
	logModule string,
) (map[string]any, any) {
	now := time.Now().Format("2006-01-02 15:04:05")
	seedUpdates := map[string]any{}
	if parseErr == nil && repo != nil {
		updateErr := repo.UpdateSeedParameterByKey(hash, torrentID, siteName, map[string]any{
			"mediainfo":        strings.TrimSpace(mediainfoText),
			"mediainfo_status": "completed",
			"bdinfo_error":     "",
			"updated_at":       now,
		})
		if updateErr != nil {
			logx.Warnf(logModule, "数据库更新失败：seed_id=%s hash=%s torrent_id=%s site=%s err=%v", seedID, hash, torrentID, siteName, updateErr)
		} else if deps.AfterPersist != nil {
			if row, rowErr := repo.GetSeedParameterByKey(hash, torrentID, siteName); rowErr == nil {
				deps.AfterPersist(hash, torrentID, siteName, row, savePath, torrentName, strings.TrimSpace(mediainfoText))
				// 后处理可能回写 title_components/medium/tags/type，完成后再读取最新行回传给前端。
				if updatedRow, updatedRowErr := repo.GetSeedParameterByKey(hash, torrentID, siteName); updatedRowErr == nil {
					seedUpdates = buildSeedUpdatesFromRow(updatedRow)
				}
			} else {
				logx.Warnf(logModule, "后处理回调跳过：seed_id=%s 读取行失败 err=%v", seedID, rowErr)
			}
		} else {
			// 即使没有后处理，也回传一次当前落库后的结果，便于前端同步显示。
			if updatedRow, updatedRowErr := repo.GetSeedParameterByKey(hash, torrentID, siteName); updatedRowErr == nil {
				seedUpdates = buildSeedUpdatesFromRow(updatedRow)
			}
		}

		if len(seedUpdates) > 0 {
			logx.Infof(logModule, "返回前端更新字段：seed_id=%s keys=%d", seedID, len(seedUpdates))
		}
	} else if parseErr != nil {
		logx.Warnf(logModule, "数据库更新跳过：seed_id=%s 解析失败 err=%v", seedID, parseErr)
	}

	seedUpdatesValue := any(nil)
	if len(seedUpdates) > 0 {
		seedUpdatesValue = seedUpdates
	}
	return seedUpdates, seedUpdatesValue
}

// buildSeedUpdatesFromRow 组装回传前端的字段更新集（不引入额外落库点，仅反映当前数据库最新值）。
// 参数/返回：row 为 seed_parameters 当前行；返回 seed_updates 对象（可能为空）。
// 失败场景：row 为空时返回空 map。
// 副作用：无。
func buildSeedUpdatesFromRow(row map[string]any) map[string]any {
	if len(row) == 0 {
		return map[string]any{}
	}

	title := strings.TrimSpace(toStringWithDefault(row["title"], ""))
	mediainfoText := strings.TrimSpace(toStringWithDefault(row["mediainfo"], ""))
	body := strings.TrimSpace(toStringWithDefault(row["body"], ""))
	inferred := parser.InferStandardizedValues(title, mediainfoText, body)

	standardized := map[string]any{
		"type":   strings.TrimSpace(toStringWithDefault(row["type"], "")),
		"medium": strings.TrimSpace(toStringWithDefault(row["medium"], "")),
		"tags":   parseJSONStringArray(row["tags"]),
	}
	updates := map[string]any{
		"title_components": parseJSONArray(row["title_components"]),
		"standardized_params": map[string]any{
			"type":   standardized["type"],
			"medium": standardized["medium"],
			"tags":   standardized["tags"],
		},
		"inferred_standardized_params": map[string]any{
			"video_codec": strings.TrimSpace(inferred["video_codec"]),
			"audio_codec": strings.TrimSpace(inferred["audio_codec"]),
			"resolution":  strings.TrimSpace(inferred["resolution"]),
		},
	}
	return updates
}

func parseJSONArray(value any) []any {
	if value == nil {
		return []any{}
	}
	if typed, ok := value.([]any); ok {
		return typed
	}
	if typed, ok := value.(string); ok {
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return []any{}
		}
		if strings.HasPrefix(trimmed, "[") {
			parsed := []any{}
			if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
				return parsed
			}
		}
	}
	return []any{}
}

func parseJSONStringArray(value any) []string {
	if value == nil {
		return []string{}
	}
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			trimmed := strings.TrimSpace(toStringWithDefault(item, ""))
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return []string{}
		}
		if strings.HasPrefix(trimmed, "[") {
			parsed := []string{}
			if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
				return parsed
			}
			parsedAny := []any{}
			if err := json.Unmarshal([]byte(trimmed), &parsedAny); err == nil {
				out := make([]string, 0, len(parsedAny))
				for _, item := range parsedAny {
					entry := strings.TrimSpace(toStringWithDefault(item, ""))
					if entry != "" {
						out = append(out, entry)
					}
				}
				return out
			}
		}
		parts := strings.Split(trimmed, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			entry := strings.TrimSpace(part)
			if entry != "" {
				out = append(out, entry)
			}
		}
		return out
	default:
		return []string{}
	}
}

func toStringWithDefault(value any, fallback string) string {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return fallback
		}
		return trimmed
	case []byte:
		trimmed := strings.TrimSpace(string(typed))
		if trimmed == "" {
			return fallback
		}
		return trimmed
	default:
		return fallback
	}
}
