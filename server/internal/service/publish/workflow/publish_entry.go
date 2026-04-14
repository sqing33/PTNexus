package workflow

import (
	"fmt"
	neturl "net/url"
	"strings"

	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	publishchecker "github.com/pt-nexus/server/internal/service/publish/checker"
	publishdownloader "github.com/pt-nexus/server/internal/service/publish/downloader"
	publishuploader "github.com/pt-nexus/server/internal/service/publish/uploader"
)

// PublishExecutionInput 定义单站发布执行输入。
type PublishExecutionInput struct {
	TargetSite string
	TargetInfo map[string]any
	UploadData map[string]any

	SourceSiteNickname string

	TorrentPath string
	Payload     map[string]any

	FallbackSavePath     string
	FallbackDownloaderID string
	DefaultDownloaderID  string
}

// PublishExecutionDeps 定义单站发布执行依赖。
type PublishExecutionDeps struct {
	AddToDownloader         func(payload map[string]any) (map[string]any, int)
	FindSiteNicknameByGroup func(releaseGroup string) (string, error)
}

// ExecutePublish 执行单站发布主流程（上传、URL标准化、自动加下载器）。
// 参数/返回：输入需包含目标站配置、发布参数和种子路径；返回与原迁移接口一致的发布结果结构。
// 失败场景：上传失败、自动加下载器参数不足时返回对应状态与提示。
// 副作用：会向目标站点发起上传请求，并可选调用下载器添加任务。
func ExecutePublish(input PublishExecutionInput, deps PublishExecutionDeps) (map[string]any, int) {
	targetSite := strings.TrimSpace(input.TargetSite)
	targetInfo := input.TargetInfo
	uploadData := input.UploadData
	payload := input.Payload
	if uploadData == nil {
		uploadData = map[string]any{}
	}
	if payload == nil {
		payload = map[string]any{}
	}

	resolvedSavePath, resolvedDownloaderID := publishdownloader.ResolveEffectiveTarget(
		payload,
		strings.TrimSpace(input.FallbackSavePath),
		strings.TrimSpace(input.FallbackDownloaderID),
		strings.TrimSpace(input.DefaultDownloaderID),
	)
	buildPreCheckFailure := func(reason string) (map[string]any, int) {
		trimmedReason := strings.TrimSpace(reason)
		if trimmedReason == "" {
			trimmedReason = "参数校验未通过"
		}
		message := fmt.Sprintf("发布前校验失败: %s", trimmedReason)
		return map[string]any{
			"success":       false,
			"logs":          message,
			"message":       message,
			"limit_reached": true,
			"pre_check":     true,
			"url":           nil,
		}, 200
	}
	if resolvedSavePath == "" {
		return buildPreCheckFailure("缺少有效 savePath，已停止发布")
	}
	if resolvedDownloaderID == "" {
		return buildPreCheckFailure("缺少有效 downloaderId，已停止发布")
	}

	publishURL, directDownloadURL, logs, isExistingTorrent, uploadFormFields, publishErr := PublishTorrentToTarget(
		targetInfo,
		uploadData,
		strings.TrimSpace(input.TorrentPath),
		strings.TrimSpace(input.SourceSiteNickname),
		deps.FindSiteNicknameByGroup,
	)
	targetNickname := strings.TrimSpace(toStringAny(targetInfo["nickname"], targetSite))
	if publishErr != nil {
		failureLogs := strings.TrimSpace(logs)
		if failureLogs == "" {
			failureLogs = fmt.Sprintf("发布到 %s 失败: %v", targetNickname, publishErr)
		}
		return map[string]any{
			"success": false,
			"logs":    failureLogs,
			"url":     nil,
		}, 500
	}
	normalizedBaseURL := acquirefetch.NormalizeSiteBaseURL(toStringAny(targetInfo["base_url"], ""))
	publishURL = publishuploader.NormalizePublishURLWithOfferSupport(normalizedBaseURL, publishURL)
	directDownloadURL = strings.TrimSpace(directDownloadURL)
	if directDownloadURL == "" {
		directDownloadURL = publishuploader.BuildDirectDownloadURLForPublished(
			toStringAny(targetInfo["base_url"], ""),
			toStringAny(targetInfo["passkey"], ""),
			toStringAny(targetInfo["site"], ""),
			publishURL,
		)
	}
	trimmedLogs := strings.TrimSpace(logs)
	if trimmedLogs == "" {
		trimmedLogs = fmt.Sprintf("发布到 %s 成功", targetNickname)
	}
	if publishURL != "" && !strings.Contains(trimmedLogs, publishURL) {
		trimmedLogs += "\n" + fmt.Sprintf("详情页链接: %s", publishURL)
	}
	if directDownloadURL != "" && !strings.Contains(trimmedLogs, directDownloadURL) {
		trimmedLogs += "\n" + fmt.Sprintf("直链下载: %s", directDownloadURL)
	}

	autoEditExecuted := false
	autoEditResult := map[string]any(nil)
	autoUpdateExistingTorrent := false
	if _, exists := payload["auto_update_existing_torrent"]; exists {
		autoUpdateExistingTorrent = boolFromAny(payload["auto_update_existing_torrent"])
	} else if _, exists := payload["autoUpdateExistingTorrent"]; exists {
		autoUpdateExistingTorrent = boolFromAny(payload["autoUpdateExistingTorrent"])
	}
	if isExistingTorrent && autoUpdateExistingTorrent {
		torrentID := strings.TrimSpace(extractTorrentIDFromPublishURL(publishURL))
		if torrentID == "" {
			autoEditResult = map[string]any{
				"success": false,
				"skipped": true,
				"message": "跳过：仅支持 NexusPHP details.php?id=... 形式的详情页链接",
			}
		} else if strings.TrimSpace(normalizedBaseURL) == "" {
			autoEditResult = map[string]any{
				"success":    false,
				"skipped":    true,
				"torrent_id": torrentID,
				"message":    "跳过：目标站点缺少 base_url",
			}
		} else {
			cookie := strings.TrimSpace(toStringAny(targetInfo["cookie"], ""))
			if cookie == "" {
				autoEditResult = map[string]any{
					"success":    false,
					"skipped":    true,
					"torrent_id": torrentID,
					"message":    "跳过：目标站点缺少 cookie",
				}
			} else {
				trimmedLogs += "\n--- [自动更新已存在种子信息] ---"

				detailHTML, fetchDetail, fetchErr := publishuploader.TryFetchDetailHTML(publishURL, cookie)
				if strings.TrimSpace(fetchDetail) != "" {
					trimmedLogs += "\n" + fetchDetail
				}
				if fetchErr != nil {
					autoEditResult = map[string]any{
						"success":    false,
						"skipped":    true,
						"torrent_id": torrentID,
						"message":    fmt.Sprintf("跳过：获取详情页失败: %v", fetchErr),
					}
				} else {
					if permissionLog := strings.TrimSpace(publishchecker.BuildExistingTorrentEditPermissionLog(detailHTML, torrentID)); permissionLog != "" {
						trimmedLogs += "\n" + permissionLog
					}
					canEdit, denyReason := publishchecker.CheckExistingTorrentEditPermission(detailHTML, torrentID)
					if !canEdit {
						reason := strings.TrimSpace(denyReason)
						if reason == "" {
							reason = "详情页校验未通过"
						}
						autoEditResult = map[string]any{
							"success":    false,
							"skipped":    true,
							"torrent_id": torrentID,
							"message":    fmt.Sprintf("跳过：%s", reason),
						}
					} else {
						editFields := copyStringMap(uploadFormFields)
						editFields["id"] = torrentID
						takeEditURL := strings.TrimRight(normalizedBaseURL, "/") + "/takeedit.php"
						referer := strings.TrimRight(normalizedBaseURL, "/") + "/edit.php?id=" + torrentID

						editSuccess, editDetail, editErr := publishuploader.TryEditTorrent(takeEditURL, cookie, referer, editFields)
						autoEditExecuted = true
						if strings.TrimSpace(editDetail) != "" {
							trimmedLogs += "\n" + editDetail
						}

						if editErr != nil {
							autoEditResult = map[string]any{
								"success":    false,
								"skipped":    false,
								"torrent_id": torrentID,
								"message":    fmt.Sprintf("编辑失败: %v", editErr),
							}
						} else if editSuccess {
							autoEditResult = map[string]any{
								"success":    true,
								"skipped":    false,
								"torrent_id": torrentID,
								"message":    "编辑成功",
							}
						} else {
							autoEditResult = map[string]any{
								"success":    false,
								"skipped":    false,
								"torrent_id": torrentID,
								"message":    "编辑请求已提交，但未能确认是否成功（请自行打开详情页确认）",
							}
						}
					}
				}
			}
		}
	}

	autoAdd := boolFromAny(payload["autoAddToDownloader"]) || boolFromAny(payload["auto_add_to_downloader"]) || boolFromAny(payload["auto_add"])
	autoAddExistingToDownloader := true
	if _, exists := payload["auto_add_existing_to_downloader"]; exists {
		autoAddExistingToDownloader = boolFromAny(payload["auto_add_existing_to_downloader"])
	} else if _, exists := payload["autoAddExistingToDownloader"]; exists {
		autoAddExistingToDownloader = boolFromAny(payload["autoAddExistingToDownloader"])
	}
	useDefaultDownloader := boolFromAny(payload["useDefaultDownloader"]) || boolFromAny(payload["use_default_downloader"])
	autoAddResult := map[string]any{"success": false, "message": "未启用自动添加到下载器"}
	if autoAdd {
		if isExistingTorrent && !autoAddExistingToDownloader {
			autoAddResult = map[string]any{"success": false, "message": "检测到目标站点种子已存在，按设置跳过自动添加"}
		} else if resolvedSavePath == "" {
			autoAddResult = map[string]any{"success": false, "message": "自动添加已跳过：缺少 savePath"}
		} else if resolvedDownloaderID == "" && !useDefaultDownloader {
			autoAddResult = map[string]any{"success": false, "message": "自动添加已跳过：缺少 downloaderId"}
		} else if deps.AddToDownloader == nil {
			autoAddResult = map[string]any{"success": false, "message": "自动添加已跳过：服务未注册 AddToDownloader 回调"}
		} else {
			downloadURLForDownloader := publishURL
			if strings.TrimSpace(directDownloadURL) != "" {
				downloadURLForDownloader = strings.TrimSpace(directDownloadURL)
			}
			addPayload := map[string]any{
				"url":      downloadURLForDownloader,
				"savePath": resolvedSavePath,
			}
			if targetNickname != "" {
				addPayload["siteNickname"] = targetNickname
			}
			if resolvedDownloaderID != "" {
				addPayload["downloaderId"] = resolvedDownloaderID
			}
			if raw, exists := payload["useDefaultDownloader"]; exists {
				addPayload["useDefaultDownloader"] = raw
			} else if raw, exists := payload["use_default_downloader"]; exists {
				addPayload["useDefaultDownloader"] = raw
			}
			if tags, exists := uploadData["tags"]; exists {
				addPayload["tags"] = tags
			}
			if standardized, ok := uploadData["standardized_params"].(map[string]any); ok && standardized != nil {
				if tags, exists := standardized["tags"]; exists {
					addPayload["tags"] = tags
				}
			}

			addResult, _ := deps.AddToDownloader(addPayload)
			autoAddResult = addResult
		}
	}

	return map[string]any{
		"success":             true,
		"logs":                trimmedLogs,
		"url":                 publishURL,
		"direct_download_url": directDownloadURL,
		"site_identifier":     strings.TrimSpace(toStringAny(targetInfo["site"], strings.ToLower(targetSite))),
		"auto_add_result":     autoAddResult,
		"auto_add_executed":   autoAdd,
		"auto_edit_executed":  autoEditExecuted,
		"auto_edit_result":    autoEditResult,
		"is_existing_torrent": isExistingTorrent,
	}, 200
}

func extractTorrentIDFromPublishURL(publishURL string) string {
	trimmed := strings.TrimSpace(publishURL)
	if trimmed == "" {
		return ""
	}

	parsed, err := neturl.Parse(trimmed)
	if err != nil {
		return ""
	}

	query := parsed.Query()
	if id := strings.TrimSpace(query.Get("id")); id != "" {
		return id
	}
	return strings.TrimSpace(query.Get("torrent_id"))
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func boolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case string:
		lower := strings.ToLower(strings.TrimSpace(typed))
		return lower == "1" || lower == "true" || lower == "yes"
	default:
		return false
	}
}
