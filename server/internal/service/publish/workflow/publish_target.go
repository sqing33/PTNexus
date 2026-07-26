package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
	processingtitle "github.com/pt-nexus/server/internal/service/processing/title"
	publishpublisher "github.com/pt-nexus/server/internal/service/publish/publisher"
	publishengine "github.com/pt-nexus/server/internal/service/publish/publisher/engine"
	publishuploader "github.com/pt-nexus/server/internal/service/publish/uploader"
)

var publishTitleBitDepthPattern = regexp.MustCompile(`(?i)\b(?:8|10|12|16|24)bit\b`)

// PublishTorrentToTarget 将种子文件发布到目标站点，并返回发布 URL 与日志文案。
// 参数/返回：targetInfo 为目标站配置，uploadData 为发布字段，torrentPath 为本地种子路径。
// 失败场景：配置缺失、读取种子失败、上传接口全部失败时返回 error。
// 副作用：读取本地种子文件并向目标站点发起上传请求。
func PublishTorrentToTarget(
	targetInfo map[string]any,
	uploadData map[string]any,
	torrentPath string,
	sourceSiteNickname string,
	findSiteNicknameByGroup func(releaseGroup string) (string, error),
	rootConfig map[string]any,
) (string, string, string, bool, map[string]string, error) {
	targetName := strings.TrimSpace(toStringAny(targetInfo["nickname"], toStringAny(targetInfo["site"], "目标站点")))
	logLines := []string{
		fmt.Sprintf("--- [步骤2] 开始发布种子到 %s ---", targetName),
	}
	appendLog := func(text string) {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return
		}
		logLines = append(logLines, trimmed)
	}

	siteCode := strings.TrimSpace(toStringAny(targetInfo["site"], ""))
	baseURL := acquirefetch.NormalizeSiteBaseURL(toStringAny(targetInfo["base_url"], ""))
	cookie := strings.TrimSpace(toStringAny(targetInfo["cookie"], ""))

	title := resolvePublishMainTitle(siteCode, uploadData, torrentPath)
	subtitle := strings.TrimSpace(toStringAny(uploadData["subtitle"], ""))
	description := publishuploader.BuildUploadDescription(siteCode, uploadData)
	imdbLink, doubanLink := resolvePublishExternalLinks(uploadData)
	mediainfo := strings.TrimSpace(toStringAny(uploadData["mediainfo"], ""))

	pubInput := publishpublisher.PublishInput{
		TargetName: targetName,
		SiteCode:   siteCode,
		BaseURL:    baseURL,
		Cookie:     cookie,
		TargetInfo: targetInfo,

		UploadData:  uploadData,
		TorrentPath: strings.TrimSpace(torrentPath),

		Title:       title,
		Subtitle:    subtitle,
		Description: description,
		IMDbLink:    imdbLink,
		DoubanLink:  doubanLink,
		MediaInfo:   mediainfo,

		SourceSiteNickname:      strings.TrimSpace(sourceSiteNickname),
		FindSiteNicknameByGroup: findSiteNicknameByGroup,
		RootConfig:              rootConfig,
	}

	result, publishErr := publishengine.Publish(pubInput)

	if strings.TrimSpace(result.AttemptDetailLog) != "" {
		appendLog(result.AttemptDetailLog)
	}

	if publishErr != nil {
		appendLog(fmt.Sprintf("发布结果：发布到 %s 失败: %v", targetName, publishErr))
		appendLog("--- [步骤2] 任务执行完毕 ---")
		return "", "", strings.Join(logLines, "\n"), result.IsExistingTorrent, result.UploadFormFields, publishErr
	}

	if os.Getenv("UPLOAD_TEST_MODE") == "true" {
		appendLog(fmt.Sprintf("发布结果：发布到 %s 成功 (测试模式)", targetName))
	} else if result.IsExistingTorrent {
		appendLog(fmt.Sprintf("发布结果：种子已存在于 %s，已自动更新信息", targetName))
	} else {
		appendLog(fmt.Sprintf("发布结果：成功发布到 %s", targetName))
	}
	appendLog("--- [步骤2] 任务执行完毕 ---")
	return result.PublishURL, result.DirectDownloadURL, strings.Join(logLines, "\n"), result.IsExistingTorrent, result.UploadFormFields, nil
}

// resolvePublishMainTitle 生成目标站点实际发布时使用的主标题。
// 参数/返回：siteCode 用于应用站点级标题修正；uploadData 为前端传入的发布参数；torrentPath 用于缺失标题时兜底文件名。
// 失败场景：标题组件缺失或重建失败时回退到当前通用标题取值，不返回错误。
// 副作用：无。
func resolvePublishMainTitle(siteCode string, uploadData map[string]any, torrentPath string) string {
	baseTitle := strings.TrimSpace(toStringAny(uploadData["original_main_title"], toStringAny(uploadData["title"], "")))
	if baseTitle == "" {
		baseTitle = strings.TrimSpace(toStringAny(uploadData["name"], filepath.Base(torrentPath)))
	}
	if !strings.EqualFold(strings.TrimSpace(siteCode), "qingwapt") {
		return baseTitle
	}

	titleComponents := parsePublishTitleComponents(uploadData["title_components"])
	if len(titleComponents) == 0 {
		return stripPublishTitleBitDepth(baseTitle)
	}

	completed := processingtitle.CompleteTitleComponents(titleComponents, baseTitle)
	filtered := filterPublishTitleComponents(completed, "色深")
	rebuilt := strings.TrimSpace(processingtitle.BuildPreviewTitleFromTitleComponents(filtered, baseTitle))
	if rebuilt == "" || rebuilt == "-NOGROUP" {
		return stripPublishTitleBitDepth(baseTitle)
	}
	return rebuilt
}

// parsePublishTitleComponents 将发布 payload 中的 title_components 统一转换为 []any。
func parsePublishTitleComponents(raw any) []any {
	switch typed := raw.(type) {
	case []any:
		return typed
	case []map[string]any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		return items
	case []map[string]string:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			converted := map[string]any{}
			for key, value := range item {
				converted[key] = value
			}
			items = append(items, converted)
		}
		return items
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return []any{}
		}
		decoded := []any{}
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			return decoded
		}
		return []any{}
	default:
		return []any{}
	}
}

// filterPublishTitleComponents 过滤标题组件中的指定键，避免站点不需要的参数进入最终标题。
func filterPublishTitleComponents(items []any, excludedKeys ...string) []any {
	if len(items) == 0 {
		return []any{}
	}

	excluded := make(map[string]struct{}, len(excludedKeys))
	for _, key := range excludedKeys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		excluded[trimmed] = struct{}{}
	}
	if len(excluded) == 0 {
		return items
	}

	filtered := make([]any, 0, len(items))
	for _, item := range items {
		component, ok := item.(map[string]any)
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		key := strings.TrimSpace(toStringAny(component["key"], ""))
		if _, skip := excluded[key]; skip {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// stripPublishTitleBitDepth 从标题文本中移除独立的色深标记，供缺少标题组件时兜底使用。
func stripPublishTitleBitDepth(title string) string {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return ""
	}
	stripped := publishTitleBitDepthPattern.ReplaceAllString(trimmed, " ")
	return strings.Join(strings.Fields(stripped), " ")
}

func toStringAny(value any, fallback string) string {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed != "" {
			return trimmed
		}
	case []byte:
		trimmed := strings.TrimSpace(string(typed))
		if trimmed != "" {
			return trimmed
		}
	}
	return fallback
}

func resolvePublishExternalLinks(uploadData map[string]any) (string, string) {
	if uploadData == nil {
		return "", ""
	}

	standardized, _ := uploadData["standardized_params"].(map[string]any)

	imdbLink := firstNonEmpty(
		toStringAny(uploadData["imdb_link"], ""),
		toStringAny(uploadData["imdbLink"], ""),
		toStringAny(uploadData["imdb"], ""),
	)
	doubanLink := firstNonEmpty(
		toStringAny(uploadData["douban_link"], ""),
		toStringAny(uploadData["doubanLink"], ""),
		toStringAny(uploadData["douban"], ""),
		toStringAny(uploadData["pt_gen"], ""),
		toStringAny(uploadData["ptgen"], ""),
	)

	if standardized != nil {
		imdbLink = firstNonEmpty(
			imdbLink,
			toStringAny(standardized["imdb_link"], ""),
			toStringAny(standardized["imdbLink"], ""),
			toStringAny(standardized["imdb"], ""),
		)
		doubanLink = firstNonEmpty(
			doubanLink,
			toStringAny(standardized["douban_link"], ""),
			toStringAny(standardized["doubanLink"], ""),
			toStringAny(standardized["douban"], ""),
		)
	}

	return strings.TrimSpace(imdbLink), strings.TrimSpace(doubanLink)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
