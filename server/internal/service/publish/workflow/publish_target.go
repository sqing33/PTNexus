package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	acquirefetch "github.com/pt-nexus/server-go/internal/service/acquire/fetch"
	publishpublisher "github.com/pt-nexus/server-go/internal/service/publish/publisher"
	publishengine "github.com/pt-nexus/server-go/internal/service/publish/publisher/engine"
	publishuploader "github.com/pt-nexus/server-go/internal/service/publish/uploader"
)

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

	title := strings.TrimSpace(toStringAny(uploadData["original_main_title"], toStringAny(uploadData["title"], "")))
	if title == "" {
		title = strings.TrimSpace(toStringAny(uploadData["name"], filepath.Base(torrentPath)))
	}
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
