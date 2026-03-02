package publisher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
	publishuploader "github.com/pt-nexus/server/internal/service/publish/uploader"
)

// PublishPublic 执行公共发布器（NexusPHP 等表单 takeupload.php/upload.php）发布流程。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回发布详情页 URL、是否疑似“种子已存在”、用于自动编辑的表单字段，以及发布过程日志。
// 失败场景：站点配置缺失、必需鉴权信息不足、读取种子失败、上传接口全部失败时返回 error。
// 副作用：读取本地种子文件并向目标站点发起上传请求；可选写入 data/tmp/torrents 参数落盘。
func PublishPublic(input PublishInput) (PublishResult, error) {
	targetName := strings.TrimSpace(input.TargetName)
	if targetName == "" {
		targetName = "目标站点"
	}
	siteCode := strings.TrimSpace(input.SiteCode)
	baseURL := strings.TrimSpace(input.BaseURL)
	cookie := strings.TrimSpace(input.Cookie)

	title := strings.TrimSpace(input.Title)
	subtitle := strings.TrimSpace(input.Subtitle)
	description := strings.TrimSpace(input.Description)
	imdbLink := strings.TrimSpace(input.IMDbLink)
	doubanLink := strings.TrimSpace(input.DoubanLink)
	mediainfo := strings.TrimSpace(input.MediaInfo)

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

	if mapped := publishmapping.ResolveBasicPublishMappings(siteCode, input.UploadData); len(mapped) > 0 {
		for key, value := range mapped {
			formFields[key] = value
		}
	}

	for key, value := range input.ExtraFormFields {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		formFields[trimmedKey] = trimmedValue
	}

	if input.AdjustFormFields != nil {
		input.AdjustFormFields(formFields)
	}

	logLines := make([]string, 0, 8)
	appendLog := func(text string) {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return
		}
		logLines = append(logLines, trimmed)
	}
	buildDetail := func() string { return strings.Join(logLines, "\n") }

	if dumpPath, dumpErr := publishuploader.DumpUploadParametersToTmp(
		targetName,
		strings.TrimSpace(input.TorrentPath),
		formFields,
		input.UploadData,
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
		return PublishResult{
			PublishURL:       "https://demo.site.test/details.php?id=999999999&uploaded=1&test=true",
			UploadFormFields: formFields,
			AttemptDetailLog: buildDetail(),
		}, nil
	}

	if baseURL == "" {
		return PublishResult{UploadFormFields: formFields, AttemptDetailLog: buildDetail()}, fmt.Errorf("目标站点缺少 base_url")
	}
	if cookie == "" {
		return PublishResult{UploadFormFields: formFields, AttemptDetailLog: buildDetail()}, fmt.Errorf("目标站点缺少 cookie")
	}

	torrentPath := strings.TrimSpace(input.TorrentPath)
	torrentFile, err := os.ReadFile(torrentPath)
	if err != nil {
		return PublishResult{UploadFormFields: formFields, AttemptDetailLog: buildDetail()}, fmt.Errorf("读取种子文件失败: %w", err)
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
	if len(attemptTargets) == 0 {
		return PublishResult{UploadFormFields: formFields, AttemptDetailLog: buildDetail()}, fmt.Errorf("上传配置缺失")
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
		publishURL, attemptExisting, attemptDetail, attemptErr := publishuploader.TryUploadTorrent(
			target.uploadURL,
			baseURL,
			cookie,
			target.fileField,
			torrentFile,
			filepath.Base(torrentPath),
			formFields,
		)
		existing = existing || attemptExisting
		appendLog(attemptDetail)
		if attemptErr == nil {
			return PublishResult{
				PublishURL:        publishURL,
				IsExistingTorrent: existing,
				UploadFormFields:  formFields,
				AttemptDetailLog:  buildDetail(),
			}, nil
		}
		lastErr = attemptErr
		// 站点已明确提示“种子已存在”时不再重试，避免后续尝试把错误覆盖为冗长 HTML 页面。
		if attemptExisting {
			break
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("发布失败")
	}
	return PublishResult{
		IsExistingTorrent: existing,
		UploadFormFields:  formFields,
		AttemptDetailLog:  buildDetail(),
	}, lastErr
}
