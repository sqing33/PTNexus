package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	acquirefetch "github.com/pt-nexus/server-go/internal/service/acquire/fetch"
	publishmapping "github.com/pt-nexus/server-go/internal/service/publish/mapping"
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
	if isPTLGSSite(siteCode) {
		description = buildPTLGSDescription(uploadData)
		appendLog("检测到 PTLGS 站点：启用特殊字段分离流程")
	}
	imdbLink, doubanLink := resolvePublishExternalLinks(uploadData)
	mediainfo := strings.TrimSpace(toStringAny(uploadData["mediainfo"], ""))

	// 朱雀站点（TNode/API）走特殊发布逻辑：不使用 takeupload.php/upload.php 表单。
	if strings.EqualFold(siteCode, "zhuque") {
		appendLog("检测到朱雀站点：启用 API 发布流程")

		if baseURL == "" {
			err := fmt.Errorf("目标站点缺少 base_url")
			appendLog(fmt.Sprintf("发布结果：发布到 %s 失败: %v", targetName, err))
			appendLog("--- [步骤2] 任务执行完毕 ---")
			return "", "", strings.Join(logLines, "\n"), false, nil, err
		}
		if cookie == "" {
			err := fmt.Errorf("目标站点缺少 cookie")
			appendLog(fmt.Sprintf("发布结果：发布到 %s 失败: %v", targetName, err))
			appendLog("--- [步骤2] 任务执行完毕 ---")
			return "", "", strings.Join(logLines, "\n"), false, nil, err
		}

		zhuqueFields, buildErr := publishuploader.BuildZhuqueUploadFields(uploadData, title, subtitle, mediainfo, imdbLink, doubanLink)
		if buildErr != nil {
			appendLog(fmt.Sprintf("朱雀参数构建失败: %v", buildErr))
			appendLog("--- [步骤2] 任务执行完毕 ---")
			return "", "", strings.Join(logLines, "\n"), false, nil, buildErr
		}

		if dumpPath, dumpErr := publishuploader.DumpUploadParametersToTmp(
			targetName,
			torrentPath,
			zhuqueFields,
			uploadData,
			title,
			zhuqueFields["note"],
			subtitle,
			imdbLink,
			doubanLink,
			mediainfo,
		); dumpErr != nil {
			appendLog(fmt.Sprintf("发布参数保存失败: %v", dumpErr))
		} else if strings.TrimSpace(dumpPath) != "" {
			appendLog(fmt.Sprintf("发布参数已保存到: %s", dumpPath))
		}

		// 对齐 Python：UPLOAD_TEST_MODE=true 时跳过真实发布，返回模拟的详情页链接。
		if os.Getenv("UPLOAD_TEST_MODE") == "true" {
			appendLog("测试模式：跳过实际发布，模拟成功响应")
			appendLog(fmt.Sprintf("发布结果：发布到 %s 成功 (测试模式)", targetName))
			appendLog("--- [步骤2] 任务执行完毕 ---")
			return "https://demo.site.test/torrent/info/999999999?test=true", "https://demo.site.test/api/torrent/download/999999999/TEST_KEY", strings.Join(logLines, "\n"), false, zhuqueFields, nil
		}

		torrentFile, err := os.ReadFile(torrentPath)
		if err != nil {
			wrappedErr := fmt.Errorf("读取种子文件失败: %w", err)
			appendLog(fmt.Sprintf("发布结果：发布到 %s 失败: %v", targetName, wrappedErr))
			appendLog("--- [步骤2] 任务执行完毕 ---")
			return "", "", strings.Join(logLines, "\n"), false, zhuqueFields, wrappedErr
		}

		publishURL, directDownloadURL, existing, attemptDetail, attemptErr := publishuploader.TryUploadTorrentZhuque(
			baseURL,
			cookie,
			filepath.Base(torrentPath),
			torrentFile,
			zhuqueFields,
		)
		appendLog(attemptDetail)
		if attemptErr != nil {
			appendLog(fmt.Sprintf("发布结果：发布到 %s 失败: %v", targetName, attemptErr))
			appendLog("--- [步骤2] 任务执行完毕 ---")
			return "", "", strings.Join(logLines, "\n"), existing, zhuqueFields, attemptErr
		}

		if existing {
			appendLog(fmt.Sprintf("发布结果：种子已存在于 %s，已自动更新信息", targetName))
		} else {
			appendLog(fmt.Sprintf("发布结果：成功发布到 %s", targetName))
		}
		appendLog("--- [步骤2] 任务执行完毕 ---")
		return publishURL, directDownloadURL, strings.Join(logLines, "\n"), existing, zhuqueFields, nil
	}

	siteCfg, _ := publishmapping.LoadSitePublishConfig(siteCode)
	resolveFieldName := func(mappingKey string, fallback string) string {
		if siteCfg == nil {
			return fallback
		}
		for _, key := range []string{mappingKey, fallback} {
			if resolved := strings.TrimSpace(siteCfg.FormFields[key]); resolved != "" {
				return resolved
			}
		}
		return fallback
	}
	setField := func(formFields map[string]string, mappingKey string, fallback string, value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		formFields[resolveFieldName(mappingKey, fallback)] = trimmed
	}

	formFields := map[string]string{}
	setField(formFields, "name", "name", title)
	setField(formFields, "title", "title", title)
	setField(formFields, "small_descr", "small_descr", subtitle)
	setField(formFields, "description", "descr", description)
	setField(formFields, "imdb_url", "url", imdbLink)
	setField(formFields, "douban_url", "dburl", doubanLink)
	setField(formFields, "pt_gen", "pt_gen", doubanLink)
	setField(formFields, "technical_info", "technical_info", mediainfo)
	if isPTLGSSite(siteCode) {
		cover, screenshots := buildPTLGSImageFields(uploadData)
		setField(formFields, "cover", "cover", cover)
		setField(formFields, "screenshots", "screenshots", screenshots)
	}

	if mapped := publishmapping.ResolvePublishMappings(siteCode, uploadData, publishmapping.MappingContext{
		SourceSiteNickname:      sourceSiteNickname,
		FindSiteNicknameByGroup: findSiteNicknameByGroup,
	}); len(mapped) > 0 {
		for key, value := range mapped {
			formFields[key] = value
		}
	}

	if dumpPath, dumpErr := publishuploader.DumpUploadParametersToTmp(
		targetName,
		torrentPath,
		formFields,
		uploadData,
		title,
		description,
		subtitle,
		imdbLink,
		doubanLink,
		mediainfo,
	); dumpErr != nil {
		appendLog(fmt.Sprintf("发布参数保存失败: %v", dumpErr))
	} else if strings.TrimSpace(dumpPath) != "" {
		appendLog(fmt.Sprintf("发布参数已保存到: %s", dumpPath))
	}

	// 对齐 Python：UPLOAD_TEST_MODE=true 时跳过真实发布，返回模拟的详情页链接。
	if os.Getenv("UPLOAD_TEST_MODE") == "true" {
		appendLog("测试模式：跳过实际发布，模拟成功响应")
		appendLog(fmt.Sprintf("发布结果：发布到 %s 成功 (测试模式)", targetName))
		appendLog("--- [步骤2] 任务执行完毕 ---")
		return "https://demo.site.test/details.php?id=999999999&uploaded=1&test=true", "", strings.Join(logLines, "\n"), false, formFields, nil
	}

	if baseURL == "" {
		err := fmt.Errorf("目标站点缺少 base_url")
		appendLog(fmt.Sprintf("发布结果：发布到 %s 失败: %v", targetName, err))
		appendLog("--- [步骤2] 任务执行完毕 ---")
		return "", "", strings.Join(logLines, "\n"), false, formFields, err
	}

	torrentFile, err := os.ReadFile(torrentPath)
	if err != nil {
		wrappedErr := fmt.Errorf("读取种子文件失败: %w", err)
		appendLog(fmt.Sprintf("发布结果：发布到 %s 失败: %v", targetName, wrappedErr))
		appendLog("--- [步骤2] 任务执行完毕 ---")
		return "", "", strings.Join(logLines, "\n"), false, formFields, wrappedErr
	}

	if isRousiSite(siteCode) {
		appendLog("上传方式: API v1 JSON")
		publishURL, existing, attemptDetail, attemptErr := tryUploadTorrentRousiAPI(baseURL, targetName, torrentPath, targetInfo, uploadData, torrentFile, title, description)
		appendLog(attemptDetail)
		if attemptErr == nil {
			if existing {
				appendLog(fmt.Sprintf("发布结果：种子已存在于 %s，已自动更新信息", targetName))
			} else {
				appendLog(fmt.Sprintf("发布结果：成功发布到 %s", targetName))
			}
			appendLog("--- [步骤2] 任务执行完毕 ---")
			return publishURL, "", strings.Join(logLines, "\n"), existing, formFields, nil
		}
		appendLog(fmt.Sprintf("发布结果：发布到 %s 失败: %v", targetName, attemptErr))
		appendLog("--- [步骤2] 任务执行完毕 ---")
		return "", "", strings.Join(logLines, "\n"), existing, formFields, attemptErr
	}

	if cookie == "" {
		err := fmt.Errorf("目标站点缺少 cookie")
		appendLog(fmt.Sprintf("发布结果：发布到 %s 失败: %v", targetName, err))
		appendLog("--- [步骤2] 任务执行完毕 ---")
		return "", "", strings.Join(logLines, "\n"), false, formFields, err
	}

	uploadURLs := []string{
		strings.TrimRight(baseURL, "/") + "/takeupload.php",
		strings.TrimRight(baseURL, "/") + "/upload.php",
	}
	fileFields := []string{"file", "torrent", "torrentfile", "uplfile"}
	type uploadAttemptTarget struct {
		uploadURL string
		fileField string
	}
	attemptTargets := make([]uploadAttemptTarget, 0, len(uploadURLs)*len(fileFields))
	for _, fileField := range fileFields {
		for _, uploadURL := range uploadURLs {
			attemptTargets = append(attemptTargets, uploadAttemptTarget{uploadURL: uploadURL, fileField: fileField})
		}
	}
	// 简化日志输出，不再显示技术细节
	if len(attemptTargets) == 0 {
		err := fmt.Errorf("上传配置缺失")
		appendLog(fmt.Sprintf("发布结果：发布到 %s 失败: %v", targetName, err))
		appendLog("--- [步骤2] 任务执行完毕 ---")
		return "", "", strings.Join(logLines, "\n"), false, formFields, err
	}

	lastErr := error(nil)
	existing := false
	maxRetryCount := 1
	totalAttempts := maxRetryCount + 1
	if totalAttempts > len(attemptTargets) {
		totalAttempts = len(attemptTargets)
	}
	for attemptIndex := 0; attemptIndex < totalAttempts; attemptIndex++ {
		target := attemptTargets[attemptIndex]
		publishURL, attemptExisting, attemptDetail, attemptErr := publishuploader.TryUploadTorrent(target.uploadURL, baseURL, cookie, target.fileField, torrentFile, filepath.Base(torrentPath), formFields)
		existing = existing || attemptExisting
		appendLog(attemptDetail)
		if attemptErr == nil {
			if existing {
				appendLog(fmt.Sprintf("发布结果：种子已存在于 %s，已自动更新信息", targetName))
			} else {
				appendLog(fmt.Sprintf("发布结果：成功发布到 %s", targetName))
			}
			appendLog("--- [步骤2] 任务执行完毕 ---")
			return publishURL, "", strings.Join(logLines, "\n"), existing, formFields, nil
		}
		lastErr = attemptErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("发布失败")
	}
	appendLog(fmt.Sprintf("发布结果：发布到 %s 失败: %v", targetName, lastErr))
	appendLog("--- [步骤2] 任务执行完毕 ---")
	return "", "", strings.Join(logLines, "\n"), existing, formFields, lastErr
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
