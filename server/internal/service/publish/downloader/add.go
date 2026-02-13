package downloader

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	acquirefetch "github.com/pt-nexus/server-go/internal/service/acquire/fetch"
	"github.com/pt-nexus/server-go/internal/service/downloaderclient"
	processingpersist "github.com/pt-nexus/server-go/internal/service/processing/persist"
	processingshared "github.com/pt-nexus/server-go/internal/service/processing/shared"
	publishguard "github.com/pt-nexus/server-go/internal/service/publish/guard"
	publishuploader "github.com/pt-nexus/server-go/internal/service/publish/uploader"
	"gorm.io/gorm"
)

// AddToDownloaderRepo 定义“按详情页反查站点并下载种子”所需依赖。
type AddToDownloaderRepo interface {
	DB() *gorm.DB
}

// AddToDownloader 将发布 URL 或本地种子添加到下载器。
// 参数/返回：payload 为前端传参；rootConfig 为全局配置；repo 可用于从 URL 反查站点并下载种子；返回接口响应与状态码。
// 失败场景：缺少 url/savePath/downloaderId、下载器配置错误、添加失败等会返回对应错误。
// 副作用：可能发起网络请求下载 torrent，并向下载器添加任务。
func AddToDownloader(payload map[string]any, rootConfig map[string]any, repo AddToDownloaderRepo) (map[string]any, int) {
	rawURL := strings.TrimSpace(processingshared.ToString(payload["url"], ""))
	savePath := strings.TrimSpace(processingshared.ToString(payload["savePath"], ""))
	downloaderID := strings.TrimSpace(processingshared.ToString(payload["downloaderId"], ""))

	if processingpersist.BoolFromAny(payload["useDefaultDownloader"]) && rootConfig != nil {
		if crossSeed, ok := rootConfig["cross_seed"].(map[string]any); ok {
			defaultID := strings.TrimSpace(processingshared.ToString(crossSeed["default_downloader"], ""))
			if defaultID != "" {
				downloaderID = defaultID
			}
		}
	}

	if rawURL == "" || savePath == "" || downloaderID == "" {
		return map[string]any{"success": false, "message": "错误：缺少必要参数 (url, savePath, downloaderId)。"}, 400
	}

	// 🚫 自动添加前预检查（对齐 Python）：避免触发做种/队列限制。
	canContinue, limitMessage := publishguard.CheckDownloaderGate(downloaderID)
	if !canContinue {
		if strings.TrimSpace(limitMessage) == "" {
			limitMessage = "已触发限制"
		}
		return map[string]any{
			"success":       false,
			"message":       strings.TrimSpace(limitMessage),
			"downloader_id": nil,
			"limit_reached": true,
			"pre_check":     true,
		}, 200
	}

	downloader, err := downloaderclient.FromConfig(rootConfig, downloaderID)
	if err != nil {
		return map[string]any{"success": false, "message": err.Error()}, 400
	}

	manualTags := processingpersist.ParseStringArray(payload["tags"])
	siteNicknameHint := strings.TrimSpace(processingshared.ToString(payload["siteNickname"], processingshared.ToString(payload["site_nickname"], "")))
	addOptions := downloaderclient.AddTorrentOptions{Paused: false}
	detailSite := map[string]any{}

	addMessage := ""
	queuedTorrent := ""
	startedAt := time.Now()
	isLocalTorrent := strings.HasPrefix(strings.ToLower(rawURL), "file://") || publishuploader.LooksLikeLocalTorrentPath(rawURL)

	if !isLocalTorrent {
		db := (*gorm.DB)(nil)
		if repo != nil {
			db = repo.DB()
		}
		if db != nil {
			detailSite = acquirefetch.FindSiteByDetailURL(db, rawURL)
			if speedLimit := resolveSiteSpeedLimitMBps(detailSite); speedLimit > 0 {
				addOptions.UploadLimitMBps = speedLimit
			}
		}
	}

	resolvedSiteNickname := siteNicknameHint
	if resolvedSiteNickname == "" {
		resolvedSiteNickname = strings.TrimSpace(processingshared.ToString(detailSite["nickname"], ""))
	}
	configuredTags, configuredCategory := resolveConfiguredTagsAndCategory(rootConfig, resolvedSiteNickname)
	addOptions.Tags = mergeTagLists(manualTags, configuredTags)
	if downloader.Type == "transmission" && configuredCategory != "" {
		addOptions.Tags = appendUniqueTag(addOptions.Tags, configuredCategory)
	}
	if downloader.Type == "qbittorrent" {
		addOptions.Category = configuredCategory
	}

	if strings.HasPrefix(strings.ToLower(rawURL), "file://") {
		parsed, parseErr := url.Parse(rawURL)
		if parseErr != nil || strings.TrimSpace(parsed.Path) == "" {
			return map[string]any{"success": false, "message": "file:// URL 无效"}, 400
		}
		filePath := parsed.Path
		if err := downloader.AddTorrentFileWithOptions(filePath, savePath, addOptions); err != nil {
			return map[string]any{"success": false, "message": "添加种子文件失败: " + err.Error()}, 500
		}
		queuedTorrent = filePath
		addMessage = "已通过本地种子文件添加到下载器"
	} else if publishuploader.LooksLikeLocalTorrentPath(rawURL) {
		if err := downloader.AddTorrentFileWithOptions(rawURL, savePath, addOptions); err != nil {
			return map[string]any{"success": false, "message": "添加种子文件失败: " + err.Error()}, 500
		}
		queuedTorrent = rawURL
		addMessage = "已通过本地种子文件添加到下载器"
	} else {
		addedByData := false
		if len(detailSite) > 0 {
			if _, _, torrentBytes, dlErr := acquirefetch.DownloadTorrentForSource(detailSite, rawURL); dlErr == nil && len(torrentBytes) > 0 {
				fileName := fmt.Sprintf("auto-%d.torrent", time.Now().UnixNano())
				if err := downloader.AddTorrentDataWithOptions(torrentBytes, fileName, savePath, addOptions); err == nil {
					addedByData = true
					addMessage = "已从详情页下载种子并添加到下载器"
				}
			}
		}
		if !addedByData {
			if err := downloader.AddTorrentURLWithOptions(rawURL, savePath, addOptions); err != nil {
				return map[string]any{"success": false, "message": "添加下载任务失败: " + err.Error()}, 500
			}
			addMessage = "已通过 URL 添加到下载器"
		}
	}

	return map[string]any{
		"success":         true,
		"message":         addMessage,
		"downloader_id":   downloader.ID,
		"downloader_name": downloader.Name,
		"queued_torrent":  queuedTorrent,
		"cost_ms":         time.Since(startedAt).Milliseconds(),
	}, 200
}

func resolveSiteSpeedLimitMBps(site map[string]any) int {
	if len(site) == 0 {
		return 0
	}
	value, exists := site["speed_limit"]
	if !exists || value == nil {
		return 0
	}
	limit := int(processingshared.ToFloat(value))
	if limit <= 0 {
		return 0
	}
	return limit
}

// resolveConfiguredTagsAndCategory 解析设置中心下载器标签/分类配置。
// 参数/返回：rootConfig 为全局配置，siteNickname 用于替换“站点/{站点名称}”占位符；返回标签列表和分类。
// 失败场景：配置缺失或字段类型异常时返回空结果。
// 副作用：无。
func resolveConfiguredTagsAndCategory(rootConfig map[string]any, siteNickname string) ([]string, string) {
	if len(rootConfig) == 0 {
		return []string{}, ""
	}
	tagsConfig, ok := rootConfig["tags_config"].(map[string]any)
	if !ok {
		return []string{}, ""
	}

	categoryName := ""
	if categoryConfig, ok := tagsConfig["category"].(map[string]any); ok {
		if processingpersist.BoolFromAny(categoryConfig["enabled"]) {
			categoryName = strings.TrimSpace(processingshared.ToString(categoryConfig["category"], ""))
		}
	}

	finalTags := []string{}
	if tagsConfigMap, ok := tagsConfig["tags"].(map[string]any); ok {
		if processingpersist.BoolFromAny(tagsConfigMap["enabled"]) {
			for _, rawTag := range processingpersist.ParseStringArray(tagsConfigMap["tags"]) {
				tag := strings.TrimSpace(rawTag)
				if tag == "" {
					continue
				}
				if tag == "站点/{站点名称}" {
					if siteNickname == "" {
						continue
					}
					tag = "站点/" + siteNickname
				}
				finalTags = appendUniqueTag(finalTags, tag)
			}
		}
	}

	return finalTags, categoryName
}

// mergeTagLists 合并多个标签列表并去重，保持原始顺序。
// 参数/返回：inputs 为待合并标签列表；返回去重后的标签切片。
// 失败场景：输入为空时返回空切片。
// 副作用：无。
func mergeTagLists(inputs ...[]string) []string {
	merged := make([]string, 0)
	for _, input := range inputs {
		for _, tag := range input {
			merged = appendUniqueTag(merged, tag)
		}
	}
	return merged
}

// appendUniqueTag 向标签列表追加单个标签并去重。
// 参数/返回：tags 为原列表，tag 为待追加值；返回去重后的列表。
// 失败场景：tag 为空时返回原列表。
// 副作用：无。
func appendUniqueTag(tags []string, tag string) []string {
	trimmed := strings.TrimSpace(tag)
	if trimmed == "" {
		return tags
	}
	for _, current := range tags {
		if current == trimmed {
			return tags
		}
	}
	return append(tags, trimmed)
}
