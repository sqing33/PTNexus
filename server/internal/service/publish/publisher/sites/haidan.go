package sites

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/platform/logx"
	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
	publishuploader "github.com/pt-nexus/server/internal/service/publish/uploader"
)

const haidanPublishLogModule = "发布-海胆"

var (
	reHaidanSeasonEpisodeEN = regexp.MustCompile(`(?i)S(\d+)(?:E(\d+))?`)
	reHaidanSeasonEpisodeCN = regexp.MustCompile(`第(\d+)季(?:第(\d+)集)?`)
	reHaidanImgTag          = regexp.MustCompile(`(?is)\[img\](.*?)\[/img\]`)
)

// PublishHaidan 执行海胆站点特殊发布流程（截图走 preview-pics 字段，描述不包含截图）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回发布详情页 URL、是否疑似“种子已存在”、用于自动编辑的表单字段，以及发布过程日志。
// 失败场景：配置缺失、必需映射字段缺失、读取种子失败、上传失败等返回 error。
// 副作用：读取本地种子文件并向目标站点发起上传请求；可选写入 data/tmp/torrents 参数落盘。
func PublishHaidan(input publisher.PublishInput) (publisher.PublishResult, error) {
	targetName := strings.TrimSpace(input.TargetName)
	if targetName == "" {
		targetName = "目标站点"
	}

	logLines := []string{
		"检测到海胆站点：启用特殊发布流程",
	}
	appendLog := func(text string) {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return
		}
		logLines = append(logLines, trimmed)
	}
	buildDetail := func() string { return strings.Join(logLines, "\n") }

	baseURL := strings.TrimSpace(input.BaseURL)
	cookie := strings.TrimSpace(input.Cookie)
	if baseURL == "" {
		err := fmt.Errorf("目标站点缺少 base_url")
		appendLog(fmt.Sprintf("参数校验失败: %v", err))
		return publisher.PublishResult{AttemptDetailLog: buildDetail()}, err
	}
	if cookie == "" {
		err := fmt.Errorf("目标站点缺少 cookie")
		appendLog(fmt.Sprintf("参数校验失败: %v", err))
		return publisher.PublishResult{AttemptDetailLog: buildDetail()}, err
	}

	haidanFields, buildErr := BuildHaidanUploadFields(
		input.UploadData,
		strings.TrimSpace(input.Title),
		strings.TrimSpace(input.Subtitle),
		strings.TrimSpace(input.MediaInfo),
		strings.TrimSpace(input.IMDbLink),
		strings.TrimSpace(input.DoubanLink),
	)
	if buildErr != nil {
		appendLog(fmt.Sprintf("海胆参数构建失败: %v", buildErr))
		return publisher.PublishResult{AttemptDetailLog: buildDetail()}, buildErr
	}

	if dumpPath, dumpErr := publishuploader.DumpUploadParametersToTmp(
		targetName,
		strings.TrimSpace(input.TorrentPath),
		haidanFields,
		input.UploadData,
		strings.TrimSpace(input.Title),
		strings.TrimSpace(haidanFields["descr"]),
		strings.TrimSpace(input.Subtitle),
		strings.TrimSpace(input.IMDbLink),
		strings.TrimSpace(input.DoubanLink),
		strings.TrimSpace(input.MediaInfo),
	); dumpErr != nil {
		appendLog(fmt.Sprintf("发布参数保存失败: %v", dumpErr))
	} else if strings.TrimSpace(dumpPath) != "" {
		appendLog(fmt.Sprintf("发布参数已保存到: %s", dumpPath))
	}

	// 对齐 Python：UPLOAD_TEST_MODE=true 时跳过真实发布，返回模拟的详情页链接。
	if os.Getenv("UPLOAD_TEST_MODE") == "true" {
		appendLog("测试模式：跳过实际发布，模拟成功响应")
		return publisher.PublishResult{
			PublishURL:       "https://demo.site.test/details.php?id=999999999&uploaded=1&test=true",
			UploadFormFields: haidanFields,
			AttemptDetailLog: buildDetail(),
		}, nil
	}

	torrentPath := strings.TrimSpace(input.TorrentPath)
	torrentFile, err := os.ReadFile(torrentPath)
	if err != nil {
		wrappedErr := fmt.Errorf("读取种子文件失败: %w", err)
		appendLog(fmt.Sprintf("读取种子失败: %v", wrappedErr))
		return publisher.PublishResult{UploadFormFields: haidanFields, AttemptDetailLog: buildDetail()}, wrappedErr
	}

	publishURL, existing, attemptDetail, attemptErr := TryUploadTorrentHaidan(
		baseURL,
		cookie,
		filepath.Base(torrentPath),
		torrentFile,
		haidanFields,
	)
	appendLog(attemptDetail)
	if attemptErr != nil {
		return publisher.PublishResult{
			IsExistingTorrent: existing,
			UploadFormFields:  haidanFields,
			AttemptDetailLog:  buildDetail(),
		}, attemptErr
	}

	return publisher.PublishResult{
		PublishURL:        publishURL,
		IsExistingTorrent: existing,
		UploadFormFields:  haidanFields,
		AttemptDetailLog:  buildDetail(),
	}, nil
}

// BuildHaidanUploadFields 构造海胆站点的表单字段。
// 参数/返回：uploadData 为发布 payload；title/subtitle/mediainfo/imdbLink/doubanLink 为最终展示字段；
// 返回可直接提交给 takeupload.php 的字段映射。
// 失败场景：配置缺失、必需映射字段缺失时返回 error。
// 副作用：读取运行时 config.json 以决定是否匿名发布。
func BuildHaidanUploadFields(uploadData map[string]any, title, subtitle, mediainfo, imdbLink, doubanLink string) (map[string]string, error) {
	siteCfg, err := publishmapping.LoadSitePublishConfig("haidan")
	if err != nil {
		return nil, err
	}

	standardized := map[string]any{}
	if uploadData != nil {
		if typed, ok := uploadData["standardized_params"].(map[string]any); ok && typed != nil {
			standardized = typed
		}
	}

	// 类型映射
	contentType := strings.TrimSpace(toStringAny(standardized["type"], ""))
	typeMapping := siteCfg.Mappings["type"]
	typeValue := publishmapping.PickMappedValue(typeMapping, contentType)

	// 媒介映射
	mediumStr := strings.TrimSpace(toStringAny(standardized["medium"], ""))
	mediumMapping := siteCfg.Mappings["medium"]
	mediumValue := publishmapping.PickMappedValue(mediumMapping, mediumStr)

	// 视频编码映射
	codecStr := strings.TrimSpace(toStringAny(standardized["video_codec"], ""))
	codecMapping := siteCfg.Mappings["video_codec"]
	codecValue := publishmapping.PickMappedValue(codecMapping, codecStr)

	// 音频编码映射
	audioStr := strings.TrimSpace(toStringAny(standardized["audio_codec"], ""))
	audioMapping := siteCfg.Mappings["audio_codec"]
	audioValue := publishmapping.PickMappedValue(audioMapping, audioStr)

	// 分辨率映射
	resolutionStr := strings.TrimSpace(toStringAny(standardized["resolution"], ""))
	resolutionMapping := siteCfg.Mappings["resolution"]
	resolutionValue := publishmapping.PickMappedValue(resolutionMapping, resolutionStr)

	// 匿名上传设置
	anonymousUpload := true
	paths := config.ResolveRuntimePaths()
	if manager, mgrErr := config.NewManager(paths); mgrErr == nil {
		root := manager.Get()
		if uploadSettings, ok := root["upload_settings"].(map[string]any); ok && uploadSettings != nil {
			anonymousUpload = boolFromAnyWithDefault(uploadSettings["anonymous_upload"], true)
		}
	}
	uplverValue := "yes"
	if !anonymousUpload {
		uplverValue = "no"
	}

	// 构建字段映射
	fields := map[string]string{
		"name":           strings.TrimSpace(title),
		"small_descr":    strings.TrimSpace(subtitle),
		"url":            strings.TrimSpace(imdbLink),
		"descr":          buildHaidanDescription(uploadData),
		"uplver":         uplverValue,
		"type":           strings.TrimSpace(typeValue),
		"medium_sel":     strings.TrimSpace(mediumValue),
		"codec_sel":      strings.TrimSpace(codecValue),
		"audiocodec_sel": strings.TrimSpace(audioValue),
		"standard_sel":   strings.TrimSpace(resolutionValue),
	}

	// 制作组后缀
	if teamSuffix := extractHaidanTeamSuffix(uploadData, standardized); teamSuffix != "" {
		fields["team_suffix"] = teamSuffix
	}

	// 季集信息
	season, episode := resolveHaidanSeasonEpisode(uploadData, standardized)
	if season != "" {
		fields["season"] = season
	}
	if episode != "" {
		fields["episode"] = episode
	}

	// 合集标志
	if isHaidanCollectionResource(uploadData) {
		fields["collages"] = "1"
	}

	// 豆瓣链接
	if doubanLink = resolveHaidanDoubanLink(uploadData, standardized); doubanLink != "" {
		fields["durl"] = doubanLink
	}

	// 截图（单独字段，不在描述中）
	if screenshots := extractHaidanScreenshotURLs(uploadData); screenshots != "" {
		fields["preview-pics"] = screenshots
	}

	// NFO 文本
	if mediainfo = strings.TrimSpace(mediainfo); mediainfo != "" {
		fields["nfo-string"] = mediainfo
	}

	// 标签映射
	if tags := mapHaidanTagIDs(siteCfg, uploadData, standardized); len(tags) > 0 {
		for i, tagID := range tags {
			fields[fmt.Sprintf("tag_list[%d]", i)] = tagID
		}
	}

	return fields, nil
}

// buildHaidanDescription 构建海胆站点的描述正文。
// 注意：海胆站点描述仅保留声明与简介正文；截图走 preview-pics 字段，海报不进入描述。
func buildHaidanDescription(uploadData map[string]any) string {
	intro := map[string]any{}
	if uploadData != nil {
		if item, ok := uploadData["intro"].(map[string]any); ok && item != nil {
			intro = item
		}
	}

	statement := strings.TrimSpace(pickHaidanSection(uploadData, intro, "statement"))
	body := strings.TrimSpace(pickHaidanSection(uploadData, intro, "body"))

	parts := make([]string, 0, 2)
	for _, section := range []string{statement, body} {
		if section != "" {
			parts = append(parts, section)
		}
	}

	if len(parts) == 0 {
		return strings.TrimSpace(toStringAny(uploadData["subtitle"], ""))
	}
	return strings.Join(parts, "\n\n")
}

func pickHaidanSection(uploadData map[string]any, intro map[string]any, key string) string {
	fromTop := strings.TrimSpace(toStringAny(uploadData[key], ""))
	if fromTop != "" {
		return fromTop
	}
	return strings.TrimSpace(toStringAny(intro[key], ""))
}

func extractHaidanTeamSuffix(uploadData map[string]any, standardized map[string]any) string {
	teamSuffix := strings.TrimSpace(extractTeamFromTitleComponents(uploadData["title_components"]))
	if teamSuffix == "" {
		if sourceParams, ok := uploadData["source_params"].(map[string]any); ok {
			teamSuffix = strings.TrimSpace(toStringAny(sourceParams["制作组"], ""))
		}
	}
	if teamSuffix == "" {
		teamSuffix = strings.TrimSpace(toStringAny(standardized["team"], ""))
	}
	return teamSuffix
}

func extractTeamFromTitleComponents(raw any) string {
	if raw == nil {
		return ""
	}

	lookup := func(components []any) string {
		for _, item := range components {
			component, ok := item.(map[string]any)
			if !ok {
				if typed, ok := item.(map[string]interface{}); ok {
					component = map[string]any{}
					for k, v := range typed {
						component[k] = v
					}
				} else {
					continue
				}
			}
			if strings.TrimSpace(toStringAny(component["key"], "")) != "制作组" {
				continue
			}
			return strings.TrimSpace(toStringAny(component["value"], ""))
		}
		return ""
	}

	switch typed := raw.(type) {
	case []any:
		return lookup(typed)
	case []map[string]any:
		components := make([]any, 0, len(typed))
		for _, item := range typed {
			components = append(components, item)
		}
		return lookup(components)
	default:
		return ""
	}
}

func resolveHaidanSeasonEpisode(uploadData map[string]any, standardized map[string]any) (string, string) {
	seasonEpisode := ""
	if uploadData != nil {
		seasonEpisode = strings.TrimSpace(extractHaidanTitleComponent(uploadData["title_components"], "季集"))
		if seasonEpisode == "" {
			seasonEpisode = strings.TrimSpace(toStringAny(uploadData["season_episode"], ""))
		}
	}
	if seasonEpisode == "" && standardized != nil {
		seasonEpisode = strings.TrimSpace(toStringAny(standardized["season_episode"], ""))
	}

	if seasonEpisode != "" {
		season, episode := parseHaidanSeasonEpisode(seasonEpisode)
		if season != "" || episode != "" {
			return season, episode
		}
	}

	// 如果是动漫或电视剧类型，默认设置季集为0
	contentType := strings.TrimSpace(toStringAny(standardized["type"], ""))
	if contentType == "category.animation" || contentType == "category.tv_series" {
		return "0", "0"
	}

	return "", ""
}

func extractHaidanTitleComponent(raw any, componentKey string) string {
	if raw == nil {
		return ""
	}

	lookup := func(components []any) string {
		for _, item := range components {
			component, ok := item.(map[string]any)
			if !ok {
				if typed, ok := item.(map[string]interface{}); ok {
					component = map[string]any{}
					for k, v := range typed {
						component[k] = v
					}
				} else {
					continue
				}
			}
			if strings.TrimSpace(toStringAny(component["key"], "")) != componentKey {
				continue
			}
			return strings.TrimSpace(toStringAny(component["value"], ""))
		}
		return ""
	}

	switch typed := raw.(type) {
	case []any:
		return lookup(typed)
	case []map[string]any:
		components := make([]any, 0, len(typed))
		for _, item := range typed {
			components = append(components, item)
		}
		return lookup(components)
	default:
		return ""
	}
}

func parseHaidanSeasonEpisode(seasonEpisode string) (string, string) {
	trimmed := strings.TrimSpace(seasonEpisode)
	if trimmed == "" {
		return "", ""
	}

	if match := reHaidanSeasonEpisodeEN.FindStringSubmatch(strings.ToUpper(trimmed)); len(match) >= 2 {
		season := zeroPadDigits(match[1], 2)
		episode := ""
		if len(match) >= 3 && strings.TrimSpace(match[2]) != "" {
			episode = zeroPadDigits(match[2], 2)
		} else if season != "" {
			episode = "0"
		}
		return season, episode
	}

	if match := reHaidanSeasonEpisodeCN.FindStringSubmatch(trimmed); len(match) >= 2 {
		season := zeroPadDigits(match[1], 2)
		episode := ""
		if len(match) >= 3 && strings.TrimSpace(match[2]) != "" {
			episode = zeroPadDigits(match[2], 2)
		} else if season != "" {
			episode = "0"
		}
		return season, episode
	}

	return "", ""
}

func zeroPadDigits(raw string, width int) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) >= width {
		return trimmed
	}
	return strings.Repeat("0", width-len(trimmed)) + trimmed
}

func isHaidanCollectionResource(uploadData map[string]any) bool {
	if uploadData == nil {
		return false
	}

	title := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		toStringAny(uploadData["title"], ""),
		toStringAny(uploadData["original_main_title"], ""),
		toStringAny(uploadData["name"], ""),
	)))
	if title != "" {
		for _, keyword := range []string{"合集", "collection", "全集", "complete", "多季", "series"} {
			if strings.Contains(title, keyword) {
				return true
			}
		}
	}

	if sourceParams, ok := uploadData["source_params"].(map[string]any); ok {
		sourceType := strings.ToLower(strings.TrimSpace(toStringAny(sourceParams["类型"], "")))
		if strings.Contains(sourceType, "合集") || strings.Contains(sourceType, "collection") {
			return true
		}
	}

	seasonEpisode := strings.TrimSpace(extractHaidanTitleComponent(uploadData["title_components"], "季集"))
	return strings.Contains(seasonEpisode, "多季") || strings.Contains(seasonEpisode, "全")
}

func resolveHaidanDoubanLink(uploadData map[string]any, standardized map[string]any) string {
	if uploadData == nil {
		return ""
	}

	if text := strings.TrimSpace(firstNonEmpty(
		toStringAny(uploadData["douban_link"], ""),
		toStringAny(uploadData["doubanLink"], ""),
		toStringAny(uploadData["douban"], ""),
		toStringAny(uploadData["pt_gen"], ""),
		toStringAny(uploadData["ptgen"], ""),
	)); text != "" {
		return text
	}
	if standardized == nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(
		toStringAny(standardized["douban_link"], ""),
		toStringAny(standardized["doubanLink"], ""),
		toStringAny(standardized["douban"], ""),
	))
}

func extractHaidanScreenshotURLs(uploadData map[string]any) string {
	if uploadData == nil {
		return ""
	}

	screenshots := strings.TrimSpace(toStringAny(uploadData["screenshots"], ""))
	if screenshots == "" {
		if intro, ok := uploadData["intro"].(map[string]any); ok {
			screenshots = strings.TrimSpace(toStringAny(intro["screenshots"], ""))
		}
	}
	if screenshots == "" {
		return ""
	}

	matches := reHaidanImgTag.FindAllStringSubmatch(screenshots, -1)
	if len(matches) == 0 {
		return strings.TrimSpace(screenshots)
	}

	urls := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		if url := strings.TrimSpace(match[1]); url != "" {
			urls = append(urls, url)
		}
	}
	if len(urls) == 0 {
		return ""
	}
	return strings.Join(urls, "\n")
}

func mapHaidanTagIDs(siteCfg *publishmapping.SitePublishConfig, uploadData map[string]any, standardized map[string]any) []string {
	tagMapping := map[string]string{}
	if siteCfg != nil {
		tagMapping = siteCfg.Mappings["tag"]
	}

	rawTags := collectHaidanTags(uploadData, standardized)
	if len(rawTags) == 0 {
		return nil
	}

	tagIDs := make([]string, 0, len(rawTags))
	seen := map[string]struct{}{}
	for _, tag := range rawTags {
		if strings.TrimSpace(tag) == "" {
			continue
		}
		candidates := []string{tag}
		if strings.HasPrefix(tag, "tag.") {
			candidates = append(candidates, strings.TrimPrefix(tag, "tag."))
		} else {
			candidates = append(candidates, "tag."+tag)
		}

		mappedID := ""
		for _, candidate := range candidates {
			if id, ok := tagMapping[candidate]; ok && strings.TrimSpace(id) != "" {
				mappedID = strings.TrimSpace(id)
				break
			}
		}
		if mappedID == "" {
			continue
		}
		if _, exists := seen[mappedID]; exists {
			continue
		}
		seen[mappedID] = struct{}{}
		tagIDs = append(tagIDs, mappedID)
	}

	if len(tagIDs) == 0 {
		return nil
	}
	sort.Strings(tagIDs)
	return tagIDs
}

func collectHaidanTags(uploadData map[string]any, standardized map[string]any) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 16)

	appendTags := func(value any) {
		for _, tag := range parseStringArray(value) {
			trimmed := strings.TrimSpace(tag)
			if trimmed == "" {
				continue
			}
			if _, exists := seen[trimmed]; exists {
				continue
			}
			seen[trimmed] = struct{}{}
			out = append(out, trimmed)
		}
	}

	appendTags(standardized["tags"])
	if uploadData != nil {
		appendTags(uploadData["tags"])
		if sourceParams, ok := uploadData["source_params"].(map[string]any); ok && sourceParams != nil {
			appendTags(sourceParams["标签"])
		}
	}
	return out
}

// TryUploadTorrentHaidan 执行海胆站点上传请求。
// 参数/返回：baseURL/cookie 为站点信息；fileName/torrentFile 为本地种子；formFields 为 BuildHaidanUploadFields 输出；
// 返回发布详情页 URL、是否"种子已存在"、本次尝试日志，以及错误。
func TryUploadTorrentHaidan(baseURL, cookie, fileName string, torrentFile []byte, formFields map[string]string) (string, bool, string, error) {
	normalizedBaseURL := strings.TrimRight(baseURL, "/")
	detailLines := []string{
		"海胆发布模式: 表单 takeupload.php",
		fmt.Sprintf("站点地址: %s", normalizedBaseURL),
	}
	buildDetail := func() string { return strings.Join(detailLines, "\n") }

	if normalizedBaseURL == "" {
		err := fmt.Errorf("baseURL 为空")
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", false, buildDetail(), err
	}
	if strings.TrimSpace(cookie) == "" {
		err := fmt.Errorf("cookie 为空")
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", false, buildDetail(), err
	}
	if len(torrentFile) == 0 {
		err := fmt.Errorf("torrent 内容为空")
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", false, buildDetail(), err
	}

	uploadURL := normalizedBaseURL + "/takeupload.php"
	publishURL, existing, attemptDetail, attemptErr := publishuploader.TryUploadTorrent(uploadURL, baseURL, cookie, "file", torrentFile, fileName, formFields)
	detailLines = append(detailLines, attemptDetail)

	if attemptErr != nil {
		logx.Errorf(haidanPublishLogModule, "海胆发布失败: %v", attemptErr)
		return "", existing, buildDetail(), attemptErr
	}

	logx.Infof(haidanPublishLogModule, "海胆发布成功: %s", publishURL)
	return publishURL, existing, buildDetail(), nil
}
