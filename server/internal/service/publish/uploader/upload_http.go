package uploader

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/netproxy"
)

const (
	uploadRequestTimeout       = 180 * time.Second
	uploadTLSHandshakeTimeout  = 45 * time.Second
	uploadNetworkRetryAttempts = 3
	uploadResponseSummaryLimit = 600
)

var reHTMLTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
var reUploadFormTag = regexp.MustCompile(`(?is)<form\b[^>]*>.*?</form>`)
var reHiddenInputTag = regexp.MustCompile(`(?is)<input\b[^>]*>`)
var reSelectTag = regexp.MustCompile(`(?is)<select\b[^>]*>.*?</select>`)
var reOptionTag = regexp.MustCompile(`(?is)<option\b[^>]*>.*?</option>`)
var reStripHTMLTags = regexp.MustCompile(`(?is)<[^>]+>`)
var reHTMLAttr = regexp.MustCompile(`(?is)\s([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>` + "`" + `]+))`)

type uploadFormFields struct {
	Hidden  map[string]string
	Selects map[string][]string
}

// TryUploadTorrent 执行单次上传尝试，支持重定向 Location 与响应正文解析详情页链接。
// 参数/返回：uploadURL/baseURL/cookie/fileField 为站点上传所需信息；torrentFile 为种子字节；formFields 为表单字段；
// 返回发布详情页 URL、是否疑似“种子已存在”、本次尝试日志，以及错误。
// 失败场景：HTTP 请求失败、服务端返回错误等；若站点仅提示“种子已存在”但未返回详情页，将按已存在成功处理。
// 副作用：发起 HTTP 请求并读取响应正文。
func TryUploadTorrent(uploadURL, baseURL, cookie, fileField string, torrentFile []byte, fileName string, formFields map[string]string) (string, bool, string, error) {
	detailLines := []string{
		fmt.Sprintf("正在上传种子文件..."),
		fmt.Sprintf("上传端点: %s (文件字段: %s)", strings.TrimSpace(uploadURL), strings.TrimSpace(fileField)),
	}
	buildDetail := func() string {
		return strings.Join(detailLines, "\n")
	}

	client := newUploadHTTPClient()
	mergedFields := mergeUploadFormHiddenFields(client, baseURL, cookie, formFields, &detailLines)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range mergedFields {
		if strings.TrimSpace(value) == "" {
			continue
		}
		_ = writer.WriteField(key, value)
	}
	part, createFileErr := writer.CreateFormFile(fileField, fileName)
	if createFileErr != nil {
		detailLines = append(detailLines, fmt.Sprintf("构建上传表单失败: %v", createFileErr))
		return "", false, buildDetail(), createFileErr
	}
	if _, err := part.Write(torrentFile); err != nil {
		detailLines = append(detailLines, fmt.Sprintf("写入种子内容失败: %v", err))
		return "", false, buildDetail(), err
	}
	if err := writer.Close(); err != nil {
		detailLines = append(detailLines, fmt.Sprintf("封装上传请求失败: %v", err))
		return "", false, buildDetail(), err
	}

	contentType := writer.FormDataContentType()
	payloadBytes := append([]byte(nil), body.Bytes()...)

	resp := (*http.Response)(nil)
	var err error
	for attempt := 1; attempt <= uploadNetworkRetryAttempts; attempt++ {
		requestBody := bytes.NewReader(payloadBytes)
		req, buildErr := http.NewRequest(http.MethodPost, uploadURL, requestBody)
		if buildErr != nil {
			detailLines = append(detailLines, fmt.Sprintf("创建 HTTP 请求失败: %v", buildErr))
			return "", false, buildDetail(), buildErr
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		req.Header.Set("Cookie", cookie)
		req.Header.Set("Referer", strings.TrimRight(baseURL, "/")+"/upload.php")

		resp, err = client.Do(req)
		if err == nil {
			break
		}

		detailLines = append(detailLines, fmt.Sprintf("请求失败 (第 %d/%d 次): %v", attempt, uploadNetworkRetryAttempts, err))
		if !ShouldRetryUploadNetworkError(err) || attempt >= uploadNetworkRetryAttempts {
			return "", false, buildDetail(), err
		}

		backoff := time.Duration(attempt) * 2 * time.Second
		detailLines = append(detailLines, fmt.Sprintf("检测到网络波动，%.0f 秒后重试...", backoff.Seconds()))
		time.Sleep(backoff)
	}
	if err != nil {
		return "", false, buildDetail(), err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	bodyText := string(respBody)

	isExisting := looksLikeExistingTorrent(bodyText)
	responseDetail := summarizeResponseBody(bodyText)
	detailLines = append(detailLines, fmt.Sprintf("响应状态: %d %s", resp.StatusCode, strings.TrimSpace(resp.Status)))

	// 检查 Location URL 中是否包含 existed=1 / exist=1 参数
	location := strings.TrimSpace(resp.Header.Get("Location"))
	if location != "" {
		detailLines = append(detailLines, fmt.Sprintf("Location: %s", redactURLQuery(location)))
	}
	if responseDetail != "" {
		detailLines = append(detailLines, fmt.Sprintf("站点响应摘要: %s", responseDetail))
	}
	if hasExistingFlagInLocation(location) {
		isExisting = true
	}
	if isExisting {
		detailLines = append(detailLines, "✓ 种子已存在（站点已有相同种子）")
	}

	if location != "" {
		if publishURL := NormalizePublishURLWithOfferSupport(baseURL, location); publishURL != "" && isValidPublishedDetailURL(publishURL) {
			detailLines = append(detailLines, fmt.Sprintf("解析详情页: %s", publishURL))
			return publishURL, isExisting, buildDetail(), nil
		}
	}
	if publishURL := ExtractPublishURLFromText(baseURL, bodyText); publishURL != "" && isValidPublishedDetailURL(publishURL) {
		detailLines = append(detailLines, fmt.Sprintf("解析详情页: %s", publishURL))
		return publishURL, isExisting, buildDetail(), nil
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		if looksLikeUploadLoginResult(location, respBody) {
			err = errLikelyLoginPage
			appendUploadRawResponse(&detailLines, bodyText)
			detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
			return "", isExisting, buildDetail(), err
		}
		if strings.Contains(bodyText, "已存在") || strings.Contains(bodyText, "已经上传") || strings.Contains(strings.ToLower(bodyText), "already exists") {
			if publishURL := ExtractPublishURLFromText(baseURL, bodyText); publishURL != "" && isValidPublishedDetailURL(publishURL) {
				detailLines = append(detailLines, fmt.Sprintf("解析详情页: %s", publishURL))
				return publishURL, true, buildDetail(), nil
			}
			detailLines = append(detailLines, "尝试结论: 目标站点提示种子已存在，但未返回详情链接，按已存在处理")
			return "", true, buildDetail(), nil
		}
		if looksLikeUploadFormPage(bodyText) {
			err = fmt.Errorf("站点返回上传表单页面，可能是字段缺失、校验未通过或会话状态异常")
			appendUploadRawResponse(&detailLines, bodyText)
			detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
			return "", isExisting, buildDetail(), err
		}
	}

	if responseDetail == "" {
		responseDetail = "<empty response body>"
	}
	err = fmt.Errorf("HTTP %d 上传失败: %s", resp.StatusCode, responseDetail)
	appendUploadRawResponse(&detailLines, bodyText)
	detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
	return "", isExisting, buildDetail(), err
}

// ShouldRetryUploadNetworkError 判断上传请求错误是否属于可重试的网络异常。
// 参数/返回：err 为请求返回错误；当错误可能由瞬时网络抖动导致时返回 true。
// 失败场景：无失败场景，未知错误按不可重试处理。
// 副作用：无。
func ShouldRetryUploadNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	for _, keyword := range []string{
		"tls handshake timeout",
		"i/o timeout",
		"context deadline exceeded",
		"timeout awaiting response headers",
		"connection reset by peer",
		"connection refused",
		"unexpected eof",
		"temporary failure",
		"no route to host",
	} {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

// mergeUploadFormHiddenFields 读取上传页 takeupload 表单中的隐藏字段并与发布字段合并。
func mergeUploadFormHiddenFields(client *http.Client, baseURL string, cookie string, formFields map[string]string, detailLines *[]string) map[string]string {
	merged := map[string]string{}
	appendField := func(key string, value string) {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			return
		}
		merged[trimmedKey] = strings.TrimSpace(value)
	}

	formMeta, err := fetchUploadFormFields(client, baseURL, cookie)
	if err != nil {
		if detailLines != nil {
			*detailLines = append(*detailLines, fmt.Sprintf("读取上传页隐藏字段失败，继续直接提交: %v", err))
		}
	} else if len(formMeta.Hidden) > 0 {
		for key, value := range formMeta.Hidden {
			appendField(key, value)
		}
		if detailLines != nil {
			*detailLines = append(*detailLines, fmt.Sprintf("已合并上传页隐藏字段: %d 个", len(formMeta.Hidden)))
		}
	}

	for key, value := range formFields {
		appendField(key, value)
	}
	resolveUploadSelectIndexMarkers(merged, formMeta.Selects, detailLines)
	return merged
}

func fetchUploadFormFields(client *http.Client, baseURL string, cookie string) (uploadFormFields, error) {
	if client == nil {
		client = newUploadHTTPClient()
	}
	uploadPageURL := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/upload.php"
	req, err := http.NewRequest(http.MethodGet, uploadPageURL, nil)
	if err != nil {
		return uploadFormFields{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Cookie", cookie)
	resp, err := client.Do(req)
	if err != nil {
		return uploadFormFields{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return uploadFormFields{}, fmt.Errorf("上传页响应状态异常: %d", resp.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return uploadFormFields{}, err
	}
	return extractUploadFormFields(string(content)), nil
}

// extractUploadFormFields 从上传页 HTML 中提取 takeupload 表单隐藏字段与下拉选项。
func extractUploadFormFields(pageHTML string) uploadFormFields {
	result := uploadFormFields{
		Hidden:  map[string]string{},
		Selects: map[string][]string{},
	}
	for _, form := range reUploadFormTag.FindAllString(pageHTML, -1) {
		formLower := strings.ToLower(form)
		if !strings.Contains(formLower, "takeupload.php") {
			continue
		}
		for _, input := range reHiddenInputTag.FindAllString(form, -1) {
			attrs := parseHTMLAttributes(input)
			if !strings.EqualFold(strings.TrimSpace(attrs["type"]), "hidden") {
				continue
			}
			name := strings.TrimSpace(attrs["name"])
			if name == "" {
				continue
			}
			result.Hidden[name] = strings.TrimSpace(attrs["value"])
		}
		for _, selectTag := range reSelectTag.FindAllString(form, -1) {
			attrs := parseHTMLAttributes(selectTag)
			name := strings.TrimSpace(attrs["name"])
			if name == "" {
				continue
			}
			values := make([]string, 0, 16)
			for _, optionTag := range reOptionTag.FindAllString(selectTag, -1) {
				optionAttrs := parseHTMLAttributes(optionTag)
				value, exists := optionAttrs["value"]
				if !exists {
					value = optionLabelText(optionTag)
				}
				values = append(values, strings.TrimSpace(value))
			}
			if len(values) > 0 {
				result.Selects[name] = values
			}
		}
	}
	return result
}

func resolveUploadSelectIndexMarkers(formFields map[string]string, selects map[string][]string, detailLines *[]string) {
	for key, value := range formFields {
		idx, ok := parseUploadIndexMarker(value)
		if !ok {
			continue
		}
		options := selects[strings.TrimSpace(key)]
		if idx < 0 || idx >= len(options) {
			formFields[key] = fmt.Sprintf("%d", idx)
			continue
		}
		resolved := strings.TrimSpace(options[idx])
		if resolved == "" {
			formFields[key] = fmt.Sprintf("%d", idx)
			continue
		}
		formFields[key] = resolved
		if detailLines != nil {
			*detailLines = append(*detailLines, fmt.Sprintf("已按上传页选项索引解析字段: %s index=%d", key, idx))
		}
	}
}

func parseUploadIndexMarker(value string) (int, bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "@index:") {
		return 0, false
	}
	idxText := strings.TrimSpace(strings.TrimPrefix(trimmed, "@index:"))
	idx, err := strconv.Atoi(idxText)
	if err != nil {
		return 0, false
	}
	return idx, true
}

func optionLabelText(optionTag string) string {
	text := reStripHTMLTags.ReplaceAllString(optionTag, "")
	return html.UnescapeString(strings.Join(strings.Fields(strings.TrimSpace(text)), " "))
}

// parseHTMLAttributes 解析单个 HTML 标签上的属性键值。
func parseHTMLAttributes(tag string) map[string]string {
	result := map[string]string{}
	for _, match := range reHTMLAttr.FindAllStringSubmatch(tag, -1) {
		if len(match) < 5 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(match[1]))
		value := firstNonEmptyUploadAttr(match[2], match[3], match[4])
		if key != "" {
			result[key] = html.UnescapeString(value)
		}
	}
	return result
}

func firstNonEmptyUploadAttr(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func looksLikeExistingTorrent(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	return strings.Contains(trimmed, "种子已存在") ||
		strings.Contains(trimmed, "该种子已存在") ||
		strings.Contains(trimmed, "已存在") ||
		strings.Contains(trimmed, "已经上传") ||
		strings.Contains(lower, "already exists")
}

func summarizeResponseBody(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	normalized := strings.Join(strings.Fields(trimmed), " ")
	title := extractUploadHTMLTitle(trimmed)
	if looksLikeUploadFormPage(trimmed) {
		return truncateLogText(fmt.Sprintf("返回上传表单页面 (title=%s): %s", title, normalized), uploadResponseSummaryLimit)
	}
	if looksLikeHTMLResponse(trimmed) {
		if title != "" {
			return truncateLogText(fmt.Sprintf("返回 HTML 页面 (title=%s): %s", title, normalized), uploadResponseSummaryLimit)
		}
		return truncateLogText("返回 HTML 页面: "+normalized, uploadResponseSummaryLimit)
	}
	return truncateLogText(normalized, uploadResponseSummaryLimit)
}

func appendUploadRawResponse(detailLines *[]string, bodyText string) {
	if detailLines == nil {
		return
	}
	if strings.TrimSpace(bodyText) == "" {
		return
	}
	*detailLines = append(*detailLines, "--- [站点原始响应] ---")
	*detailLines = append(*detailLines, bodyText)
}

func newUploadHTTPClient() *http.Client {
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   uploadTLSHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	netproxy.ConfigureTransport(transport)
	return &http.Client{
		Timeout:   uploadRequestTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func looksLikeUploadFormPage(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	if !strings.Contains(lower, "<form") {
		return false
	}
	if strings.Contains(lower, "action=\"takeupload.php\"") || strings.Contains(lower, "action='takeupload.php'") {
		return true
	}
	return strings.Contains(lower, "name=\"upload\"") || strings.Contains(lower, "发布 - powered by nexusphp")
}

func looksLikeHTMLResponse(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return strings.HasPrefix(lower, "<!doctype html") || strings.Contains(lower, "<html")
}

func looksLikeUploadLoginResult(location string, body []byte) bool {
	if isUploadLoginURL(location) {
		return true
	}
	return looksLikeUploadLoginHTML(body)
}

func isUploadLoginURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	parsed, err := neturl.Parse(trimmed)
	if err != nil {
		lower := strings.ToLower(trimmed)
		return strings.Contains(lower, "login.php") && strings.Contains(lower, "returnto=")
	}
	path := strings.ToLower(strings.TrimSpace(parsed.Path))
	if strings.HasSuffix(path, "/login.php") || path == "login.php" {
		return true
	}
	query := strings.ToLower(parsed.RawQuery)
	return strings.Contains(path, "login.php") && strings.Contains(query, "returnto=")
}

func looksLikeUploadLoginHTML(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	limit := len(content)
	if limit > 4096 {
		limit = 4096
	}
	sample := strings.ToLower(string(content[:limit]))
	trimmed := strings.TrimSpace(sample)
	if !strings.HasPrefix(trimmed, "<!doctype") && !strings.HasPrefix(trimmed, "<html") {
		return false
	}
	if strings.Contains(sample, "login.php") && strings.Contains(sample, "returnto=") {
		return true
	}
	if strings.Contains(sample, "name=\"username\"") && strings.Contains(sample, "name=\"password\"") {
		return true
	}
	return strings.Contains(sample, "action=\"login.php\"")
}

func isValidPublishedDetailURL(raw string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return false
	}
	if isUploadLoginURL(trimmed) {
		return false
	}
	return strings.Contains(trimmed, "details.php?") ||
		strings.Contains(trimmed, "offers.php?") ||
		strings.Contains(trimmed, "/torrent/") ||
		strings.Contains(trimmed, "/torrent/info/") ||
		isTTGDownloadURL(trimmed)
}

func extractUploadHTMLTitle(text string) string {
	match := reHTMLTitle.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return truncateLogText(strings.Join(strings.Fields(strings.TrimSpace(match[1])), " "), 120)
}

func truncateLogText(text string, maxLen int) string {
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}

func hasExistingFlagInLocation(location string) bool {
	trimmed := strings.TrimSpace(location)
	if trimmed == "" {
		return false
	}

	// 兼容部分站点返回非标准 URL 的场景。
	if strings.Contains(trimmed, "existed=1") || strings.Contains(trimmed, "exist=1") {
		return true
	}

	parsed, err := neturl.Parse(trimmed)
	if err != nil {
		return false
	}
	return queryContainsOne(parsed.Query(), "existed") || queryContainsOne(parsed.Query(), "exist")
}

func queryContainsOne(values neturl.Values, key string) bool {
	for _, value := range values[key] {
		if strings.TrimSpace(value) == "1" {
			return true
		}
	}
	return false
}
