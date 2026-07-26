package sites

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
	publishuploader "github.com/pt-nexus/server/internal/service/publish/uploader"
)

const zhuquePublishLogModule = "发布-朱雀"

var (
	reZhuqueCSRFTokenMetaNameFirst    = regexp.MustCompile(`(?is)<meta[^>]*name=["']x-csrf-token["'][^>]*content=["']([^"']+)["'][^>]*>`)
	reZhuqueCSRFTokenMetaContentFirst = regexp.MustCompile(`(?is)<meta[^>]*content=["']([^"']+)["'][^>]*name=["']x-csrf-token["'][^>]*>`)
	reZhuqueTMDB                      = regexp.MustCompile(`(?is)themoviedb\.org/(movie|tv)/(\d+)`)
	reZhuqueBBCode                    = regexp.MustCompile(`\[[^\]]*\]`)
	reZhuqueImgBBCode                 = regexp.MustCompile(`(?is)\[img\](.*?)\[/img\]`)
	reZhuqueURL                       = regexp.MustCompile(`https?://[^\s\[\]]+`)
)

// PublishZhuque 执行朱雀站点特殊发布流程（TNode/API：/api/torrent/upload）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回发布详情页 URL、直链下载 URL、是否疑似“种子已存在”、以及发布过程日志。
// 失败场景：配置缺失、必需映射字段缺失、读取种子失败、上传失败等返回 error。
// 副作用：读取本地种子文件并向目标站点发起上传请求；可选写入 data/tmp/torrents 参数落盘。
func PublishZhuque(input publisher.PublishInput) (publisher.PublishResult, error) {
	targetName := strings.TrimSpace(input.TargetName)
	if targetName == "" {
		targetName = "目标站点"
	}

	logLines := []string{
		"检测到朱雀站点：启用 API 发布流程",
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

	zhuqueFields, buildErr := BuildZhuqueUploadFields(
		input.UploadData,
		strings.TrimSpace(input.Title),
		strings.TrimSpace(input.Subtitle),
		strings.TrimSpace(input.MediaInfo),
		strings.TrimSpace(input.IMDbLink),
		strings.TrimSpace(input.DoubanLink),
		input.RootConfig,
	)
	if buildErr != nil {
		appendLog(fmt.Sprintf("朱雀参数构建失败: %v", buildErr))
		return publisher.PublishResult{AttemptDetailLog: buildDetail()}, buildErr
	}

	if dumpPath, dumpErr := publishuploader.DumpUploadParametersToTmp(
		targetName,
		strings.TrimSpace(input.TorrentPath),
		zhuqueFields,
		input.UploadData,
		strings.TrimSpace(input.Title),
		strings.TrimSpace(zhuqueFields["note"]),
		strings.TrimSpace(input.Subtitle),
		strings.TrimSpace(input.IMDbLink),
		strings.TrimSpace(input.DoubanLink),
		strings.TrimSpace(input.MediaInfo),
	); dumpErr != nil {
		appendLog(fmt.Sprintf("发布参数保存失败: %v", dumpErr))
	} else if strings.TrimSpace(dumpPath) != "" {
		appendLog(fmt.Sprintf("发布参数已保存到: %s", dumpPath))
	}

	if os.Getenv("UPLOAD_TEST_MODE") == "true" {
		appendLog("测试模式：跳过实际发布，模拟成功响应")
		return publisher.PublishResult{
			PublishURL:        "https://demo.site.test/torrent/info/999999999?test=true",
			DirectDownloadURL: "https://demo.site.test/api/torrent/download/999999999/TEST_KEY",
			UploadFormFields:  zhuqueFields,
			AttemptDetailLog:  buildDetail(),
		}, nil
	}

	torrentPath := strings.TrimSpace(input.TorrentPath)
	torrentFile, err := os.ReadFile(torrentPath)
	if err != nil {
		wrappedErr := fmt.Errorf("读取种子文件失败: %w", err)
		appendLog(fmt.Sprintf("读取种子失败: %v", wrappedErr))
		return publisher.PublishResult{UploadFormFields: zhuqueFields, AttemptDetailLog: buildDetail()}, wrappedErr
	}

	publishURL, directDownloadURL, existing, attemptDetail, attemptErr := TryUploadTorrentZhuque(
		baseURL,
		cookie,
		filepath.Base(torrentPath),
		torrentFile,
		zhuqueFields,
	)
	appendLog(attemptDetail)
	if attemptErr != nil {
		return publisher.PublishResult{
			IsExistingTorrent: existing,
			UploadFormFields:  zhuqueFields,
			AttemptDetailLog:  buildDetail(),
		}, attemptErr
	}

	return publisher.PublishResult{
		PublishURL:        publishURL,
		DirectDownloadURL: directDownloadURL,
		IsExistingTorrent: existing,
		UploadFormFields:  zhuqueFields,
		AttemptDetailLog:  buildDetail(),
	}, nil
}

// BuildZhuqueUploadFields 构造朱雀站点（TNode/API）的 multipart 表单字段。
// 参数/返回：uploadData 为发布 payload；title/subtitle/mediainfo/imdbLink/doubanLink 为最终展示字段；返回可直接提交给 /api/torrent/upload 的字段映射。
// 失败场景：配置缺失、必需映射字段缺失时返回 error。
// 副作用：读取运行时配置中的匿名发布开关。
func BuildZhuqueUploadFields(uploadData map[string]any, title, subtitle, mediainfo, imdbLink, doubanLink string, rootConfig map[string]any) (map[string]string, error) {
	siteCfg, err := publishmapping.LoadSitePublishConfig("zhuque")
	if err != nil {
		return nil, err
	}

	standardized := map[string]any{}
	if uploadData != nil {
		if typed, ok := uploadData["standardized_params"].(map[string]any); ok && typed != nil {
			standardized = typed
		}
	}

	category := strings.TrimSpace(publishmapping.PickMappedValueWithFallback("type", siteCfg.Mappings["type"], strings.TrimSpace(toStringAny(standardized["type"], ""))))
	medium := strings.TrimSpace(publishmapping.PickMappedValueWithFallback("medium", siteCfg.Mappings["medium"], strings.TrimSpace(toStringAny(standardized["medium"], ""))))
	videoCoding := strings.TrimSpace(publishmapping.PickMappedValueWithFallback("video_codec", siteCfg.Mappings["video_codec"], strings.TrimSpace(toStringAny(standardized["video_codec"], ""))))
	resolution := strings.TrimSpace(publishmapping.PickMappedValueWithFallback("resolution", siteCfg.Mappings["resolution"], strings.TrimSpace(toStringAny(standardized["resolution"], ""))))

	anonymousUpload := publisher.ResolveAnonymousUploadEnabled(rootConfig)

	tmdbID, tmdbType := extractZhuqueTMDBInfo(uploadData, imdbLink)
	screenshots := extractZhuqueScreenshots(uploadData)
	note := buildZhuqueNote(uploadData, imdbLink, doubanLink)
	tags := mapZhuqueTagIDs(siteCfg, uploadData, standardized)

	fields := map[string]string{
		"title":       strings.TrimSpace(title),
		"subtitle":    strings.TrimSpace(subtitle),
		"mediainfo":   strings.TrimSpace(mediainfo),
		"anonymous":   boolToLowerString(anonymousUpload),
		"confirm":     "true",
		"category":    category,
		"medium":      medium,
		"videoCoding": videoCoding,
		"resolution":  resolution,
		"tmdbid":      tmdbID,
		"tmdbtype":    tmdbType,
		"zwex":        "0",
	}
	if strings.TrimSpace(tags) != "" {
		fields["tags"] = tags
	}
	if strings.TrimSpace(screenshots) != "" {
		fields["screenshot"] = screenshots
	}
	if strings.TrimSpace(note) != "" {
		fields["note"] = note
	}

	required := []string{"title", "category", "medium", "videoCoding", "resolution"}
	missing := make([]string, 0, len(required))
	for _, key := range required {
		if strings.TrimSpace(fields[key]) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("缺少必需参数: %v", missing)
	}
	return fields, nil
}

// TryUploadTorrentZhuque 执行朱雀站点（TNode/API）上传请求，解析 JSON 响应并返回详情页与直链下载地址。
// 参数/返回：baseURL/cookie 为站点信息；fileName/torrentFile 为本地种子；formFields 为 BuildZhuqueUploadFields 输出；
// 返回发布详情页 URL、直链下载 URL、是否“种子已存在”、本次尝试日志，以及错误。
// 失败场景：请求失败、响应解析失败、站点返回错误等返回 error。
// 副作用：发起网络请求（获取 CSRF Token、上传、拉取 torrentKey）。
func TryUploadTorrentZhuque(baseURL, cookie, fileName string, torrentFile []byte, formFields map[string]string) (string, string, bool, string, error) {
	normalizedBaseURL := normalizeBaseURL(baseURL)
	detailLines := []string{
		"朱雀发布模式: API /api/torrent/upload",
		fmt.Sprintf("站点地址: %s", normalizedBaseURL),
	}
	buildDetail := func() string { return strings.Join(detailLines, "\n") }

	if normalizedBaseURL == "" {
		err := fmt.Errorf("baseURL 为空")
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", "", false, buildDetail(), err
	}
	if strings.TrimSpace(cookie) == "" {
		err := fmt.Errorf("cookie 为空")
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", "", false, buildDetail(), err
	}
	if len(torrentFile) == 0 {
		err := fmt.Errorf("torrent 内容为空")
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", "", false, buildDetail(), err
	}

	client, clientErr := newZhuqueHTTPClient(normalizedBaseURL, cookie)
	if clientErr != nil {
		detailLines = append(detailLines, fmt.Sprintf("创建 HTTP Client 失败: %v", clientErr))
		return "", "", false, buildDetail(), clientErr
	}

	csrfToken, csrfDetail := fetchZhuqueCSRFToken(client, normalizedBaseURL)
	if csrfDetail != "" {
		detailLines = append(detailLines, csrfDetail)
	}

	postURL := strings.TrimRight(normalizedBaseURL, "/") + "/api/torrent/upload"
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range formFields {
		_ = writer.WriteField(key, value)
	}
	part, err := writer.CreateFormFile("torrent", fileName)
	if err != nil {
		detailLines = append(detailLines, fmt.Sprintf("构建上传表单失败: %v", err))
		return "", "", false, buildDetail(), err
	}
	if _, err := part.Write(torrentFile); err != nil {
		detailLines = append(detailLines, fmt.Sprintf("写入种子内容失败: %v", err))
		return "", "", false, buildDetail(), err
	}
	if err := writer.Close(); err != nil {
		detailLines = append(detailLines, fmt.Sprintf("封装上传请求失败: %v", err))
		return "", "", false, buildDetail(), err
	}

	req, err := http.NewRequest(http.MethodPost, postURL, body)
	if err != nil {
		detailLines = append(detailLines, fmt.Sprintf("创建 HTTP 请求失败: %v", err))
		return "", "", false, buildDetail(), err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", strings.TrimRight(normalizedBaseURL, "/")+"/torrent/upload")
	req.Header.Set("Origin", normalizedBaseURL)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if strings.TrimSpace(csrfToken) != "" {
		req.Header.Set("x-csrf-token", strings.TrimSpace(csrfToken))
	}

	resp, err := client.Do(req)
	if err != nil {
		detailLines = append(detailLines, fmt.Sprintf("请求失败: %v", err))
		return "", "", false, buildDetail(), err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	bodyText := string(respBody)
	responseDetail := summarizeResponseBody(bodyText)
	detailLines = append(detailLines, fmt.Sprintf("请求地址: %s", postURL))
	detailLines = append(detailLines, fmt.Sprintf("响应状态: %d %s", resp.StatusCode, strings.TrimSpace(resp.Status)))
	if location := strings.TrimSpace(resp.Header.Get("Location")); location != "" {
		detailLines = append(detailLines, fmt.Sprintf("Location: %s", location))
	}
	if responseDetail != "" {
		detailLines = append(detailLines, fmt.Sprintf("站点响应: %s", responseDetail))
	}

	existing := false
	if resp.StatusCode == http.StatusBadRequest {
		parsed := map[string]any{}
		if err := json.Unmarshal(respBody, &parsed); err == nil {
			code := strings.TrimSpace(toStringAny(parsed["code"], ""))
			if code == "TORRENT_ALREADY_UPLOAD" {
				existing = true
				detailLines = append(detailLines, "已存在判定: true (TORRENT_ALREADY_UPLOAD)")
				logx.Infof(zhuquePublishLogModule, "朱雀发布提示种子已存在")
				return "", "", existing, buildDetail(), nil
			}
			if code != "" {
				err := fmt.Errorf("站点错误: %s", code)
				detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
				return "", "", existing, buildDetail(), err
			}
		}
		err := fmt.Errorf("参数错误 (400)")
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", "", existing, buildDetail(), err
	}
	if resp.StatusCode == http.StatusInternalServerError {
		err := fmt.Errorf("站点内部错误 (500)")
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", "", existing, buildDetail(), err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		if responseDetail == "" {
			responseDetail = "<empty response body>"
		}
		err := fmt.Errorf("HTTP %d 上传失败: %s", resp.StatusCode, responseDetail)
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", "", existing, buildDetail(), err
	}

	respJSON := map[string]any{}
	if err := json.Unmarshal(respBody, &respJSON); err != nil {
		// 对齐 Python：部分情况下返回非 JSON 文本，包含 success 字样则按成功处理。
		if strings.Contains(strings.ToLower(bodyText), "success") {
			detailLines = append(detailLines, "发布成功，但响应不是 JSON（按 success 文本判定）")
			return "", "", existing, buildDetail(), nil
		}
		detailLines = append(detailLines, fmt.Sprintf("响应解析失败: %v", err))
		return "", "", existing, buildDetail(), err
	}

	success := false
	if status, ok := respJSON["status"].(float64); ok && int(status) == 200 {
		if data, ok := respJSON["data"].(map[string]any); ok && data != nil {
			if code := strings.TrimSpace(toStringAny(data["code"], "")); code == "UPLOAD_SUCCESS" {
				success = true
			}
		}
	}
	if !success {
		if code, ok := respJSON["code"].(float64); ok && int(code) == 0 {
			success = true
		}
	}
	if !success {
		if flag, ok := respJSON["success"].(bool); ok && flag {
			success = true
		}
	}

	if !success {
		msg := strings.TrimSpace(toStringAny(respJSON["message"], toStringAny(respJSON["msg"], "未知错误")))
		if data, ok := respJSON["data"].(map[string]any); ok && data != nil {
			if nested := strings.TrimSpace(toStringAny(data["message"], "")); nested != "" {
				msg = nested
			}
		}
		err := fmt.Errorf("发布失败: %s", msg)
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", "", existing, buildDetail(), err
	}

	torrentID := ""
	if data, ok := respJSON["data"].(map[string]any); ok && data != nil {
		torrentID = strings.TrimSpace(toStringAny(data["id"], ""))
	}
	if torrentID == "" {
		torrentID = strings.TrimSpace(toStringAny(respJSON["id"], ""))
	}
	if torrentID == "" {
		detailLines = append(detailLines, "发布成功，但未获取到 ID")
		return "", "", existing, buildDetail(), nil
	}

	publishURL := strings.TrimRight(normalizedBaseURL, "/") + "/torrent/info/" + torrentID
	detailLines = append(detailLines, fmt.Sprintf("解析详情页: %s", publishURL))

	torrentKey, keyDetail := fetchZhuqueTorrentKey(client, normalizedBaseURL)
	if keyDetail != "" {
		detailLines = append(detailLines, keyDetail)
	}
	directDownloadURL := ""
	if strings.TrimSpace(torrentKey) != "" {
		directDownloadURL = strings.TrimRight(normalizedBaseURL, "/") + "/api/torrent/download/" + neturl.PathEscape(torrentID) + "/" + neturl.PathEscape(strings.TrimSpace(torrentKey))
		detailLines = append(detailLines, fmt.Sprintf("直链下载: %s", directDownloadURL))
	}
	logx.Infof(zhuquePublishLogModule, "朱雀发布成功 torrent_id=%s", torrentID)
	return publishURL, directDownloadURL, existing, buildDetail(), nil
}

func newZhuqueHTTPClient(baseURL, cookieHeader string) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	u, err := neturl.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil {
		return nil, err
	}
	jar.SetCookies(u, parseCookieHeader(cookieHeader))

	return &http.Client{
		Timeout: 120 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Jar: jar,
	}, nil
}

func parseCookieHeader(cookieHeader string) []*http.Cookie {
	parts := strings.Split(cookieHeader, ";")
	cookies := make([]*http.Cookie, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		kv := strings.SplitN(trimmed, "=", 2)
		if len(kv) != 2 {
			continue
		}
		name := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		if name == "" {
			continue
		}
		cookies = append(cookies, &http.Cookie{Name: name, Value: value})
	}
	return cookies
}

func fetchZhuqueCSRFToken(client *http.Client, baseURL string) (string, string) {
	pageURL := strings.TrimRight(baseURL, "/") + "/torrent/upload"
	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Sprintf("获取 CSRF Token 失败: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Sprintf("获取 CSRF Token 失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if resp.StatusCode != http.StatusOK {
		location := strings.TrimSpace(resp.Header.Get("Location"))
		if location != "" {
			return "", fmt.Sprintf("获取 CSRF Token 失败: HTTP %d Location=%s", resp.StatusCode, location)
		}
		return "", fmt.Sprintf("获取 CSRF Token 失败: HTTP %d", resp.StatusCode)
	}
	token := ""
	if match := reZhuqueCSRFTokenMetaNameFirst.FindStringSubmatch(text); len(match) >= 2 {
		token = strings.TrimSpace(match[1])
	}
	if token == "" {
		if match := reZhuqueCSRFTokenMetaContentFirst.FindStringSubmatch(text); len(match) >= 2 {
			token = strings.TrimSpace(match[1])
		}
	}
	if token == "" {
		return "", "获取 CSRF Token：页面未找到 x-csrf-token"
	}
	return token, "获取 CSRF Token：成功"
}

func fetchZhuqueTorrentKey(client *http.Client, baseURL string) (string, string) {
	apiURL := strings.TrimRight(baseURL, "/") + "/api/user/getSecurityInfo"
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Sprintf("获取 torrentKey 失败: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", strings.TrimRight(baseURL, "/")+"/user/rss")
	req.Header.Set("Origin", strings.TrimRight(baseURL, "/"))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Sprintf("获取 torrentKey 失败: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		detail := summarizeResponseBody(string(body))
		if strings.TrimSpace(detail) != "" {
			return "", fmt.Sprintf("获取 torrentKey 失败: HTTP %d %s", resp.StatusCode, detail)
		}
		return "", fmt.Sprintf("获取 torrentKey 失败: HTTP %d", resp.StatusCode)
	}

	parsed := map[string]any{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Sprintf("获取 torrentKey 失败: 解析响应异常 %v", err)
	}
	if status, ok := parsed["status"].(float64); ok && int(status) != 200 {
		return "", fmt.Sprintf("获取 torrentKey 失败: API status=%d", int(status))
	}
	data, ok := parsed["data"].(map[string]any)
	if !ok || data == nil {
		return "", "获取 torrentKey 失败: data 为空"
	}
	key := strings.TrimSpace(toStringAny(data["torrentKey"], ""))
	if key == "" {
		return "", "获取 torrentKey 失败: 响应缺少 torrentKey"
	}
	return key, "获取 torrentKey：成功"
}

func extractZhuqueTMDBInfo(uploadData map[string]any, imdbLink string) (string, string) {
	tmdbLink := ""
	rawIMDbFromUpload := ""
	rawIMDbAltFromUpload := ""
	standardized := map[string]any{}
	if uploadData != nil {
		tmdbLink = strings.TrimSpace(toStringAny(uploadData["tmdb_link"], ""))
		rawIMDbFromUpload = toStringAny(uploadData["imdb_link"], "")
		rawIMDbAltFromUpload = toStringAny(uploadData["imdbLink"], "")
		if typed, ok := uploadData["standardized_params"].(map[string]any); ok && typed != nil {
			standardized = typed
		}
		if tmdbLink == "" {
			if intro, ok := uploadData["intro"].(map[string]any); ok && intro != nil {
				tmdbLink = strings.TrimSpace(toStringAny(intro["tmdb_link"], ""))
			}
		}
	}
	resolvedIMDb := strings.TrimSpace(firstNonEmpty(
		strings.TrimSpace(imdbLink),
		rawIMDbFromUpload,
		rawIMDbAltFromUpload,
		toStringAny(standardized["imdb_link"], ""),
		toStringAny(standardized["imdbLink"], ""),
	))
	tmdbLink = processingrepair.ResolveTMDbLinkWithIMDbFallback(tmdbLink, resolvedIMDb, zhuquePublishLogModule, "朱雀发布")
	if tmdbLink == "" {
		return "", "0"
	}
	match := reZhuqueTMDB.FindStringSubmatch(tmdbLink)
	if len(match) < 3 {
		return "", "0"
	}
	kind := strings.ToLower(strings.TrimSpace(match[1]))
	id := strings.TrimSpace(match[2])
	if id == "" {
		return "", "0"
	}
	if kind == "tv" {
		return id, "1"
	}
	return id, "0"
}

func extractZhuqueScreenshots(uploadData map[string]any) string {
	intro := map[string]any{}
	if uploadData != nil {
		if typed, ok := uploadData["intro"].(map[string]any); ok && typed != nil {
			intro = typed
		}
	}
	raw := strings.TrimSpace(toStringAny(intro["screenshots"], ""))
	if raw == "" {
		return ""
	}

	matches := reZhuqueImgBBCode.FindAllStringSubmatch(raw, -1)
	if len(matches) > 0 {
		urls := make([]string, 0, len(matches))
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			u := strings.TrimSpace(match[1])
			if u != "" {
				urls = append(urls, u)
			}
		}
		return strings.Join(urls, "\n")
	}

	lines := strings.Split(raw, "\n")
	urlLines := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
			if match := reZhuqueURL.FindString(trimmed); match != "" {
				urlLines = append(urlLines, strings.TrimSpace(match))
			}
		}
	}
	return strings.Join(urlLines, "\n")
}

func buildZhuqueNote(uploadData map[string]any, imdbLink, doubanLink string) string {
	intro := map[string]any{}
	if uploadData != nil {
		if typed, ok := uploadData["intro"].(map[string]any); ok && typed != nil {
			intro = typed
		}
	}
	parts := make([]string, 0, 3)
	statement := strings.TrimSpace(toStringAny(intro["statement"], ""))
	if statement != "" {
		filtered := strings.TrimSpace(reZhuqueBBCode.ReplaceAllString(statement, ""))
		if filtered != "" {
			parts = append(parts, filtered)
		}
	}
	if strings.TrimSpace(imdbLink) != "" {
		parts = append(parts, fmt.Sprintf("资源IMDB链接: %s", strings.TrimSpace(imdbLink)))
	}
	if strings.TrimSpace(doubanLink) != "" {
		parts = append(parts, fmt.Sprintf("资源豆瓣链接: %s", strings.TrimSpace(doubanLink)))
	}
	return strings.Join(parts, "\n\n")
}

func mapZhuqueTagIDs(siteCfg *publishmapping.SitePublishConfig, uploadData map[string]any, standardized map[string]any) string {
	tagMapping := map[string]string{}
	if siteCfg != nil {
		tagMapping = siteCfg.Mappings["tag"]
	}

	fallback := map[string]string{
		"官方":    "601",
		"禁转":    "602",
		"国语":    "603",
		"中字":    "604",
		"杜比视界":  "611",
		"杜比":    "611",
		"HDR10": "613",
		"HDR":   "613",
		"特效字幕":  "614",
		"完结":    "621",
		"分集":    "622",
	}

	rawTags := collectZhuqueTags(uploadData, standardized)
	if len(rawTags) == 0 {
		return ""
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
			if id := strings.TrimSpace(publishmapping.PickMappedValueWithFallbackNoDefault("tag", tagMapping, candidate)); id != "" {
				mappedID = id
				break
			}
		}
		if mappedID == "" {
			trimmed := strings.TrimPrefix(tag, "tag.")
			mappedID = strings.TrimSpace(fallback[trimmed])
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
		return ""
	}
	sort.Strings(tagIDs)
	return strings.Join(tagIDs, ",")
}

func collectZhuqueTags(uploadData map[string]any, standardized map[string]any) []string {
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

func boolToLowerString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
