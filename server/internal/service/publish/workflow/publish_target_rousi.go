package workflow

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pt-nexus/server-go/internal/config"
	publishmapping "github.com/pt-nexus/server-go/internal/service/publish/mapping"
)

var (
	reRousiSourceUHDBluRay  = regexp.MustCompile(`(?i)UHD\s*(?:BLU-?RAY|BLURAY|BLURAY\s+DIY|BD\b|BD-?RIP|BDRIP)`)
	reRousiSourceUHD        = regexp.MustCompile(`(?i)\bUHD\b`)
	reRousiSourceWeb        = regexp.MustCompile(`(?i)\bWEB[\s._-]*(?:DL|RIP)\b`)
	reRousiSourceHDTV       = regexp.MustCompile(`(?i)\b(?:UHDTV|HDTV|TV[\s._-]*RIP)\b`)
	reRousiSourceDVD        = regexp.MustCompile(`(?i)\b(?:DVD[\s._-]*RIP|DVDRIP|DVD(?:5|9)?)\b`)
	reRousiSourceBluRay     = regexp.MustCompile(`(?i)\b(?:BLU-?RAY|BLURAY|BDRIP|BD-?RIP|BD)\b`)
	reRousiUUID             = regexp.MustCompile(`(?i)([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)
	reRousiBBCodeImage      = regexp.MustCompile(`(?is)\[img[^\]]*\].*?\[/img\]`)
	reRousiHTMLImage        = regexp.MustCompile(`(?is)<img[^>]*>`)
	reRousiMarkdownImage    = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)
	reRousiOnlyImageURLLine = regexp.MustCompile(`(?i)^https?://\S+\.(?:png|jpe?g|gif|webp)\S*$`)
	reRousiBBCodeQuote      = regexp.MustCompile(`(?is)\[quote\](.*?)\[/quote\]`)
	reRousiBBCodeCode       = regexp.MustCompile(`(?is)\[code\](.*?)\[/code\]`)
	reRousiBBCodeURLWithArg = regexp.MustCompile(`(?is)\[url=(.*?)\](.*?)\[/url\]`)
	reRousiBBCodeURL        = regexp.MustCompile(`(?is)\[url\](.*?)\[/url\]`)
	reRousiBBCodeColor      = regexp.MustCompile(`(?is)\[color=[^\]]+\](.*?)\[/color\]`)
	reRousiBBCodeSize       = regexp.MustCompile(`(?is)\[size=[^\]]+\](.*?)\[/size\]`)
	reRousiBBCodeBold       = regexp.MustCompile(`(?is)\[b\](.*?)\[/b\]`)
	reRousiBBCodeItalic     = regexp.MustCompile(`(?is)\[i\](.*?)\[/i\]`)
	reRousiBBCodeUnderline  = regexp.MustCompile(`(?is)\[u\](.*?)\[/u\]`)
	reRousiManyNewlines     = regexp.MustCompile(`\n{3,}`)
	reRousiBBCodeURLImg     = regexp.MustCompile(`(?is)\[url=([^\]]+)\]\s*\[img[^\]]*\](.*?)\[/img\]\s*\[/url\]`)
	reRousiBBCodeImgSource  = regexp.MustCompile(`(?is)\[img[^\]]*\](.*?)\[/img\]`)
	reRousiMDImgSource      = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)
	reRousiHTMLImgSource    = regexp.MustCompile(`(?is)<img[^>]+src=["']([^"']+)["']`)
	reRousiImageExt         = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp)(\?.*)?$`)
	reRousiPixhostThumb     = regexp.MustCompile(`(?i)^https?://t(\d+)\.pixhost\.to/thumbs/(.+)$`)
	reRousiTorrentFilename  = regexp.MustCompile(`^([^-]+)-(\d+)-`)
)

type rousiImageSource struct {
	URL     string
	Referer string
}

func isRousiSite(siteCode string) bool {
	return strings.EqualFold(strings.TrimSpace(siteCode), "rousi")
}

func tryUploadTorrentRousiAPI(
	baseURL string,
	targetName string,
	torrentPath string,
	targetInfo map[string]any,
	uploadData map[string]any,
	torrentFile []byte,
	title string,
	description string,
) (string, bool, string, error) {
	trimmedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	detailLines := []string{
		fmt.Sprintf("请求地址: %s/api/v1/torrents", trimmedBaseURL),
		"上传方式: API v1 JSON(Bearer)",
	}
	buildDetail := func() string {
		return strings.Join(detailLines, "\n")
	}

	passkey := strings.TrimSpace(toStringAny(targetInfo["passkey"], ""))
	if passkey == "" {
		err := fmt.Errorf("目标站点缺少 passkey")
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", false, buildDetail(), err
	}

	userAgent := strings.TrimSpace(toStringAny(targetInfo["user_agent"], ""))
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
	}

	siteCode := strings.TrimSpace(toStringAny(targetInfo["site"], "rousi"))
	payload, err := buildRousiAPIPayload(siteCode, uploadData, torrentFile, title, description)
	if err != nil {
		detailLines = append(detailLines, fmt.Sprintf("构建 API 参数失败: %v", err))
		return "", false, buildDetail(), err
	}

	uploadTargetName := strings.TrimSpace(targetName)
	if uploadTargetName == "" {
		uploadTargetName = strings.TrimSpace(toStringAny(targetInfo["nickname"], siteCode))
	}
	images, cleanup, summary, imgErr := buildRousiImagesDataURLs(trimmedBaseURL, passkey, userAgent, uploadTargetName, torrentPath, uploadData)
	if summary != "" {
		detailLines = append(detailLines, summary)
	}
	if imgErr != nil {
		detailLines = append(detailLines, fmt.Sprintf("截图处理失败: %v", imgErr))
	}
	if cleanup != nil {
		defer cleanup()
	}
	if len(images) > 0 {
		payload["images"] = images
	}

	body, err := json.Marshal(payload)
	if err != nil {
		detailLines = append(detailLines, fmt.Sprintf("序列化 API 参数失败: %v", err))
		return "", false, buildDetail(), err
	}

	req, err := http.NewRequest(http.MethodPost, trimmedBaseURL+"/api/v1/torrents", bytes.NewReader(body))
	if err != nil {
		detailLines = append(detailLines, fmt.Sprintf("创建 HTTP 请求失败: %v", err))
		return "", false, buildDetail(), err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+passkey)
	req.Header.Set("Origin", trimmedBaseURL)
	req.Header.Set("Referer", trimmedBaseURL+"/")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		detailLines = append(detailLines, fmt.Sprintf("请求失败: %v", err))
		return "", false, buildDetail(), err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	bodyText := string(respBody)
	responseDetail := summarizeRousiResponseBody(bodyText)
	detailLines = append(detailLines, fmt.Sprintf("响应状态: %d %s", resp.StatusCode, strings.TrimSpace(resp.Status)))
	if responseDetail != "" {
		detailLines = append(detailLines, fmt.Sprintf("站点响应: %s", responseDetail))
	}

	isExisting := looksLikeExistingTorrentForRousi(bodyText)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if responseDetail == "" {
			responseDetail = "<empty response body>"
		}
		err = fmt.Errorf("HTTP %d 上传失败: %s", resp.StatusCode, responseDetail)
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", isExisting, buildDetail(), err
	}

	resultEnvelope := map[string]any{}
	if err := json.Unmarshal(respBody, &resultEnvelope); err != nil {
		err = fmt.Errorf("API 返回非 JSON 响应: %s", responseDetail)
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", isExisting, buildDetail(), err
	}

	if code := int(toFloatLoose(resultEnvelope["code"])); code != 0 {
		message := strings.TrimSpace(toStringLoose(resultEnvelope["message"]))
		if message == "" {
			message = responseDetail
		}
		if message == "" {
			message = "未知错误"
		}
		isExisting = isExisting || looksLikeExistingTorrentForRousi(message)
		err = fmt.Errorf("API 错误: %s", message)
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", isExisting, buildDetail(), err
	}

	resultData := mapStringAny(resultEnvelope["data"])
	uuid := strings.TrimSpace(toStringLoose(resultData["uuid"]))
	if uuid == "" {
		uuid = extractUUID(bodyText)
	}
	status := strings.TrimSpace(toStringLoose(resultData["status"]))
	if status != "" {
		detailLines = append(detailLines, fmt.Sprintf("发布状态: %s", status))
	}
	if uuid == "" {
		err = fmt.Errorf("API 返回成功但缺少 uuid")
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", isExisting, buildDetail(), err
	}

	publishURL := trimmedBaseURL + "/torrent/" + uuid
	detailLines = append(detailLines, fmt.Sprintf("解析详情页: %s", publishURL))
	return publishURL, isExisting, buildDetail(), nil
}

func buildRousiAPIPayload(siteCode string, uploadData map[string]any, torrentFile []byte, title string, description string) (map[string]any, error) {
	trimmedTitle := strings.TrimSpace(title)
	if trimmedTitle == "" {
		return nil, fmt.Errorf("标题为空")
	}
	if len(torrentFile) == 0 {
		return nil, fmt.Errorf("种子文件为空")
	}

	if uploadData == nil {
		uploadData = map[string]any{}
	}
	standardized := mapStringAny(uploadData["standardized_params"])
	siteCfg, _ := publishmapping.LoadSitePublishConfig(siteCode)

	trimmedDescription := strings.TrimSpace(buildRousiDescriptionMarkdown(uploadData, description))
	if trimmedDescription == "" {
		trimmedDescription = trimmedTitle
	}

	payload := map[string]any{
		"torrent":     base64.StdEncoding.EncodeToString(torrentFile),
		"title":       trimmedTitle,
		"description": trimmedDescription,
		"anonymous":   true,
	}

	if subtitle := strings.TrimSpace(toStringLoose(uploadData["subtitle"])); subtitle != "" {
		payload["subtitle"] = subtitle
	}

	rawCategory := firstNonEmpty(
		strings.TrimSpace(toStringLoose(uploadData["category"])),
		strings.TrimSpace(toStringLoose(uploadData["type"])),
		strings.TrimSpace(toStringLoose(standardized["category"])),
		strings.TrimSpace(toStringLoose(standardized["type"])),
	)
	payload["category"] = resolveRousiCategory(rawCategory, siteCfg)

	attributes := map[string]any{}
	resolutionRaw := firstNonEmpty(
		strings.TrimSpace(toStringLoose(uploadData["resolution"])),
		strings.TrimSpace(toStringLoose(standardized["resolution"])),
	)
	if resolution := resolveRousiMappedValue(siteCfg, "resolution", resolutionRaw); resolution != "" {
		attributes["resolution"] = resolution
	}

	regionRaw := firstNonEmpty(
		strings.TrimSpace(toStringLoose(uploadData["region"])),
		strings.TrimSpace(toStringLoose(standardized["region"])),
		strings.TrimSpace(toStringLoose(standardized["source"])),
	)
	if region := resolveRousiMappedValue(siteCfg, "region", regionRaw); region != "" {
		attributes["region"] = region
	}

	if source := inferRousiSourceFromTitle(trimmedTitle); source != "" {
		attributes["source"] = source
	}

	linkSources := map[string][]string{
		"tmdb":   {"tmdb", "tmdb_link"},
		"imdb":   {"imdb", "imdb_link"},
		"douban": {"douban", "douban_link"},
	}
	for key, candidates := range linkSources {
		for _, candidate := range candidates {
			value := strings.TrimSpace(toStringLoose(uploadData[candidate]))
			if value == "" {
				value = strings.TrimSpace(toStringLoose(standardized[candidate]))
			}
			if value == "" {
				continue
			}
			attributes[key] = value
			break
		}
	}

	if len(attributes) > 0 {
		payload["attributes"] = attributes
	}

	mediaInfo := firstNonEmpty(
		strings.TrimSpace(toStringLoose(uploadData["mediaInfo"])),
		strings.TrimSpace(toStringLoose(uploadData["media_info"])),
		strings.TrimSpace(toStringLoose(uploadData["mediainfo"])),
		strings.TrimSpace(toStringLoose(uploadData["mediainfo_text"])),
		strings.TrimSpace(toStringLoose(uploadData["mediainfo_str"])),
	)
	if mediaInfo != "" {
		payload["media_info"] = mediaInfo
	}

	if anonymous, exists := uploadData["anonymous"]; exists {
		payload["anonymous"] = boolFromAny(anonymous)
	}

	if price, exists := uploadData["price"]; exists {
		payload["price"] = price
	}

	for key, value := range payload {
		if text, ok := value.(string); ok {
			if strings.TrimSpace(text) == "" {
				delete(payload, key)
			}
		}
	}

	return payload, nil
}

func extractRousiImageSourcesFromText(text string) []rousiImageSource {
	source := strings.TrimSpace(text)
	if source == "" {
		return nil
	}

	items := make([]rousiImageSource, 0, 16)
	appendURL := func(url string, referer string) {
		trimmed := strings.TrimSpace(url)
		if trimmed == "" {
			return
		}
		items = append(items, rousiImageSource{
			URL:     trimmed,
			Referer: strings.TrimSpace(referer),
		})
	}

	for _, match := range reRousiBBCodeURLImg.FindAllStringSubmatch(source, -1) {
		if len(match) < 3 {
			continue
		}
		pageURL := strings.TrimSpace(match[1])
		imgURL := strings.TrimSpace(match[2])
		if strings.HasPrefix(imgURL, "http://") || strings.HasPrefix(imgURL, "https://") {
			appendURL(imgURL, pageURL)
		}
	}
	for _, match := range reRousiBBCodeImgSource.FindAllStringSubmatch(source, -1) {
		if len(match) < 2 {
			continue
		}
		imgURL := strings.TrimSpace(match[1])
		if strings.HasPrefix(imgURL, "http://") || strings.HasPrefix(imgURL, "https://") {
			appendURL(imgURL, "")
		}
	}
	for _, match := range reRousiMDImgSource.FindAllStringSubmatch(source, -1) {
		if len(match) < 2 {
			continue
		}
		imgURL := strings.TrimSpace(match[1])
		if strings.HasPrefix(imgURL, "http://") || strings.HasPrefix(imgURL, "https://") {
			appendURL(imgURL, "")
		}
	}
	for _, match := range reRousiHTMLImgSource.FindAllStringSubmatch(source, -1) {
		if len(match) < 2 {
			continue
		}
		imgURL := strings.TrimSpace(match[1])
		if strings.HasPrefix(imgURL, "http://") || strings.HasPrefix(imgURL, "https://") {
			appendURL(imgURL, "")
		}
	}

	seen := map[string]struct{}{}
	result := make([]rousiImageSource, 0, len(items))
	for _, item := range items {
		url := strings.TrimSpace(item.URL)
		if url == "" {
			continue
		}

		if strings.Contains(strings.ToLower(url), "pixhost.to") && strings.Contains(url, "/thumbs/") {
			if sub := reRousiPixhostThumb.FindStringSubmatch(url); len(sub) >= 3 {
				url = "https://img" + strings.TrimSpace(sub[1]) + ".pixhost.to/images/" + strings.TrimSpace(sub[2])
			}
		}

		if !isProbableRousiImageURL(url) {
			continue
		}

		if _, exists := seen[url]; exists {
			continue
		}
		seen[url] = struct{}{}
		result = append(result, rousiImageSource{URL: url, Referer: strings.TrimSpace(item.Referer)})
	}

	return result
}

func isProbableRousiImageURL(url string) bool {
	lower := strings.ToLower(strings.TrimSpace(url))
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}
	if reRousiImageExt.MatchString(lower) {
		return true
	}
	if strings.Contains(lower, "pixhost.to") && (strings.Contains(lower, "/images/") || strings.Contains(lower, "/thumbs/")) {
		return true
	}
	return false
}

func collectRousiImageSources(uploadData map[string]any) []rousiImageSource {
	if uploadData == nil {
		return nil
	}

	sources := make([]rousiImageSource, 0, 16)
	hasDataURLs := false
	for _, item := range extractStringList(uploadData["images"]) {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "data:image/") {
			sources = append(sources, rousiImageSource{URL: trimmed})
			hasDataURLs = true
			continue
		}
		if isProbableRousiImageURL(trimmed) {
			sources = append(sources, rousiImageSource{URL: trimmed})
		}
	}

	if !hasDataURLs {
		intro := mapStringAny(uploadData["intro"])
		if len(intro) > 0 {
			sources = append(sources, extractRousiImageSourcesFromText(toStringLoose(intro["poster"]))...)
			sources = append(sources, extractRousiImageSourcesFromText(toStringLoose(intro["screenshots"]))...)
		}
	}

	seen := map[string]struct{}{}
	result := make([]rousiImageSource, 0, len(sources))
	for _, item := range sources {
		url := strings.TrimSpace(item.URL)
		if url == "" {
			continue
		}
		if _, exists := seen[url]; exists {
			continue
		}
		seen[url] = struct{}{}
		result = append(result, rousiImageSource{URL: url, Referer: strings.TrimSpace(item.Referer)})
	}
	return result
}

func extractStringList(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(toStringLoose(item)); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return []string{trimmed}
		}
		return nil
	default:
		return nil
	}
}

func parseRousiDataURL(dataURL string) (string, string, []byte, bool) {
	if !strings.HasPrefix(dataURL, "data:image/") {
		return "", "", nil, false
	}
	idx := strings.Index(dataURL, ";base64,")
	if idx < 0 {
		return "", "", nil, false
	}
	header := dataURL[:idx]
	b64 := dataURL[idx+len(";base64,"):]
	mime := strings.TrimSpace(strings.TrimPrefix(header, "data:"))
	ext := "bin"
	if parts := strings.SplitN(mime, "/", 2); len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		ext = strings.TrimSpace(parts[1])
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) == 0 {
		return mime, ext, nil, false
	}
	return mime, ext, raw, true
}

func buildRousiImagesDataURLs(baseURL string, passkey string, userAgent string, targetName string, torrentPath string, uploadData map[string]any) ([]string, func(), string, error) {
	sources := collectRousiImageSources(uploadData)
	if len(sources) == 0 {
		return nil, nil, "", nil
	}

	paths := config.ResolveRuntimePaths()
	imagesRoot := filepath.Join(paths.DataDir, "tmp", "torrents", "images")
	if err := os.MkdirAll(imagesRoot, 0o755); err != nil {
		return nil, nil, "", err
	}

	sourceSiteCode := "unknown"
	torrentID := "unknown"
	if base := strings.TrimSpace(filepath.Base(torrentPath)); base != "" {
		if match := reRousiTorrentFilename.FindStringSubmatch(base); len(match) >= 3 {
			sourceSiteCode = strings.TrimSpace(match[1])
			torrentID = strings.TrimSpace(match[2])
		}
	}

	timestamp := time.Now().Format("2006-01-02-15:04:05")
	workDirName := fmt.Sprintf("%s-%s-%s-%s", sanitizeRousiTempDirPart(sourceSiteCode), sanitizeRousiTempDirPart(torrentID), sanitizeRousiTempDirPart(targetName), sanitizeRousiTempDirPart(timestamp))
	workDir := filepath.Join(imagesRoot, workDirName)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, nil, "", err
	}

	cleanup := func() {
		safeRemoveWorkDir(imagesRoot, workDir)
	}

	perImageLimit := int64(5 * 1024 * 1024)
	totalLimit := int64(20 * 1024 * 1024)
	totalUsed := int64(0)

	images := make([]string, 0, 6)
	debugManifest := make([]map[string]any, 0, 8)

	for idx, item := range sources {
		if len(images) >= 6 {
			break
		}
		sourceURL := strings.TrimSpace(item.URL)
		referer := strings.TrimSpace(item.Referer)
		if sourceURL == "" {
			continue
		}

		originalPath := ""
		if strings.HasPrefix(sourceURL, "data:image/") {
			_, ext, raw, ok := parseRousiDataURL(sourceURL)
			if !ok {
				continue
			}
			originalPath = filepath.Join(workDir, fmt.Sprintf("%02d.original.%s", idx, ext))
			if err := os.WriteFile(originalPath, raw, 0o644); err != nil {
				continue
			}
		} else if strings.HasPrefix(sourceURL, "http://") || strings.HasPrefix(sourceURL, "https://") {
			raw, err := downloadRousiImageToBytes(baseURL, passkey, userAgent, sourceURL, referer)
			if err != nil || len(raw) == 0 {
				continue
			}
			originalPath = filepath.Join(workDir, fmt.Sprintf("%02d.downloaded", idx))
			if err := os.WriteFile(originalPath, raw, 0o644); err != nil {
				continue
			}
		} else {
			continue
		}

		originalSize := int64(0)
		if info, err := os.Stat(originalPath); err == nil && !info.IsDir() {
			originalSize = info.Size()
		}

		jpegPath := filepath.Join(workDir, fmt.Sprintf("%02d.final.jpg", idx))
		if !ffmpegConvertToJpegUnderLimit(originalPath, jpegPath, perImageLimit) {
			continue
		}
		jpegInfo, err := os.Stat(jpegPath)
		if err != nil || jpegInfo.IsDir() {
			continue
		}
		if totalUsed+jpegInfo.Size() > totalLimit {
			break
		}
		jpegBytes, err := os.ReadFile(jpegPath)
		if err != nil || len(jpegBytes) == 0 {
			continue
		}
		totalUsed += int64(len(jpegBytes))
		images = append(images, "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(jpegBytes))

		if os.Getenv("DEV_ENV") == "true" {
			debugManifest = append(debugManifest, map[string]any{
				"index":         idx,
				"source_url":    sourceURL,
				"referer":       referer,
				"original_path": originalPath,
				"original_size": originalSize,
				"jpeg_path":     jpegPath,
				"jpeg_size":     jpegInfo.Size(),
			})
		}
	}

	if os.Getenv("DEV_ENV") == "true" && len(debugManifest) > 0 {
		if content, err := json.MarshalIndent(debugManifest, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(workDir, "images.manifest.json"), content, 0o644)
		}
	}

	summary := fmt.Sprintf("截图处理: sources=%d images=%d total_bytes=%d", len(sources), len(images), totalUsed)
	return images, cleanup, summary, nil
}

func sanitizeRousiTempDirPart(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	trimmed = strings.ReplaceAll(trimmed, "/", "_")
	trimmed = strings.ReplaceAll(trimmed, "\\", "_")
	trimmed = strings.ReplaceAll(trimmed, ":", "_")
	trimmed = strings.ReplaceAll(trimmed, "..", "_")
	trimmed = strings.ReplaceAll(trimmed, " ", "_")
	return trimmed
}

func safeRemoveWorkDir(rootDir string, workDir string) {
	if strings.TrimSpace(rootDir) == "" || strings.TrimSpace(workDir) == "" {
		return
	}

	rootAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return
	}
	workAbs, err := filepath.Abs(workDir)
	if err != nil {
		return
	}
	rootAbs = filepath.Clean(rootAbs)
	workAbs = filepath.Clean(workAbs)
	if !strings.HasPrefix(workAbs, rootAbs+string(os.PathSeparator)) {
		return
	}
	_ = os.RemoveAll(workAbs)
}

func ffmpegConvertToJpegUnderLimit(inputPath string, outputPath string, maxBytes int64) bool {
	scalesPrimary := []int{0, 1920, 1600, 1280, 1024, 800}
	qValuesPrimary := []int{3, 5, 7, 9, 11, 13, 15, 17, 20, 24, 28, 32}

	scalesSecondary := []int{720, 640, 576, 512, 480}
	qValuesSecondary := []int{28, 32, 36, 40, 45}

	try := func(scales []int, qValues []int) bool {
		for _, scale := range scales {
			for _, qv := range qValues {
				vfArg := "null"
				if scale > 0 {
					vfArg = fmt.Sprintf("scale='min(%d,iw)':-2:flags=lanczos", scale)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				cmd := exec.CommandContext(
					ctx,
					"ffmpeg",
					"-y",
					"-hide_banner",
					"-loglevel",
					"error",
					"-i",
					inputPath,
					"-an",
					"-sn",
					"-dn",
					"-frames:v",
					"1",
					"-vf",
					vfArg,
					"-pix_fmt",
					"yuvj420p",
					"-q:v",
					strconv.Itoa(qv),
					outputPath,
				)
				cmd.Stdout = io.Discard
				cmd.Stderr = io.Discard
				_ = cmd.Run()
				cancel()

				if info, err := os.Stat(outputPath); err == nil && !info.IsDir() && info.Size() <= maxBytes {
					return true
				}
			}
		}
		return false
	}

	if try(scalesPrimary, qValuesPrimary) {
		return true
	}
	if try(scalesSecondary, qValuesSecondary) {
		return true
	}
	if info, err := os.Stat(outputPath); err == nil && !info.IsDir() && info.Size() <= maxBytes {
		return true
	}
	return false
}

func downloadRousiImageToBytes(baseURL string, passkey string, userAgent string, targetURL string, referer string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}

	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")

	chosenReferer := strings.TrimSpace(referer)
	if !(strings.HasPrefix(chosenReferer, "http://") || strings.HasPrefix(chosenReferer, "https://")) {
		chosenReferer = ""
	}
	if chosenReferer == "" {
		chosenReferer = defaultRousiRefererForURL(targetURL)
	}
	if chosenReferer != "" {
		req.Header.Set("Referer", chosenReferer)
	}

	if shouldAttachRousiBearer(baseURL, targetURL) && strings.TrimSpace(passkey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(passkey))
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(preview)))
	}
	return io.ReadAll(resp.Body)
}

func defaultRousiRefererForURL(targetURL string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(targetURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host + "/"
}

func shouldAttachRousiBearer(baseURL string, targetURL string) bool {
	baseParsed, err := neturl.Parse(strings.TrimSpace(baseURL))
	if err != nil || baseParsed.Host == "" {
		return false
	}
	targetParsed, err := neturl.Parse(strings.TrimSpace(targetURL))
	if err != nil || targetParsed.Host == "" {
		return false
	}
	return strings.EqualFold(baseParsed.Host, targetParsed.Host)
}

func resolveRousiCategory(rawCategory string, siteCfg *publishmapping.SitePublishConfig) string {
	candidate := strings.TrimSpace(rawCategory)
	if siteCfg != nil && candidate != "" {
		candidate = strings.TrimSpace(publishmapping.PickMappedValueWithFallback("type", siteCfg.Mappings["type"], candidate))
	}
	candidate = normalizeRousiToken(candidate)
	lower := strings.ToLower(candidate)

	switch {
	case strings.Contains(lower, "movie") || strings.Contains(candidate, "电影"):
		return "movie"
	case strings.Contains(lower, "tv_shows") || strings.Contains(lower, "variety") || strings.Contains(candidate, "综艺"):
		return "variety"
	case strings.Contains(lower, "tv_series") || strings.Contains(lower, "tv") || strings.Contains(candidate, "电视剧") || strings.Contains(candidate, "剧集"):
		return "tv"
	case strings.Contains(lower, "animation") || strings.Contains(lower, "anime") || strings.Contains(candidate, "动漫"):
		return "animation"
	case strings.Contains(lower, "documentary") || strings.Contains(candidate, "纪录"):
		return "documentary"
	case strings.Contains(lower, "sports") || strings.Contains(candidate, "体育"):
		return "sports"
	case strings.Contains(lower, "music") || strings.Contains(candidate, "音乐"):
		return "music"
	case strings.Contains(lower, "software") || strings.Contains(candidate, "软件"):
		return "software"
	case strings.Contains(lower, "ebook") || strings.Contains(lower, "book") || strings.Contains(candidate, "电子书"):
		return "ebook"
	case strings.Contains(lower, "other") || strings.Contains(candidate, "其他") || strings.Contains(candidate, "其它"):
		return "other"
	case strings.TrimSpace(candidate) == "":
		return "other"
	default:
		return candidate
	}
}

func resolveRousiMappedValue(siteCfg *publishmapping.SitePublishConfig, mappingKey string, rawValue string) string {
	candidate := strings.TrimSpace(rawValue)
	if candidate == "" {
		return ""
	}
	if siteCfg != nil {
		candidate = strings.TrimSpace(publishmapping.PickMappedValueWithFallback(mappingKey, siteCfg.Mappings[mappingKey], candidate))
	}
	return normalizeRousiToken(candidate)
}

func inferRousiSourceFromTitle(title string) string {
	text := strings.TrimSpace(title)
	if text == "" {
		return "其它"
	}

	switch {
	case reRousiSourceUHDBluRay.MatchString(text):
		return "UHD Blu-ray"
	case reRousiSourceUHD.MatchString(text):
		return "UHD Blu-ray"
	case reRousiSourceWeb.MatchString(text):
		return "WEB-DL"
	case reRousiSourceHDTV.MatchString(text):
		return "HDTV"
	case reRousiSourceDVD.MatchString(text):
		return "DVDRip"
	case reRousiSourceBluRay.MatchString(text):
		return "Blu-ray"
	default:
		return "其它"
	}
}

func normalizeRousiToken(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, ".") {
		parts := strings.SplitN(trimmed, ".", 2)
		prefix := strings.ToLower(strings.TrimSpace(parts[0]))
		if prefix == "source" || prefix == "resolution" || prefix == "medium" || prefix == "category" || prefix == "type" {
			trimmed = strings.TrimSpace(parts[1])
		}
	}
	return trimmed
}

func buildRousiDescriptionMarkdown(uploadData map[string]any, fallback string) string {
	intro := mapStringAny(uploadData["intro"])

	posterScreenshotURLs := []string{}
	if len(intro) > 0 {
		for _, item := range extractRousiImageSourcesFromText(toStringLoose(intro["poster"])) {
			if trimmed := strings.TrimSpace(item.URL); trimmed != "" {
				posterScreenshotURLs = append(posterScreenshotURLs, trimmed)
			}
		}
		for _, item := range extractRousiImageSourcesFromText(toStringLoose(intro["screenshots"])) {
			if trimmed := strings.TrimSpace(item.URL); trimmed != "" {
				posterScreenshotURLs = append(posterScreenshotURLs, trimmed)
			}
		}
	}

	rawText := strings.TrimSpace(toStringLoose(uploadData["description"]))
	if rawText == "" && len(intro) > 0 {
		statement := strings.TrimSpace(toStringLoose(intro["statement"]))
		body := strings.TrimSpace(toStringLoose(intro["body"]))
		parts := make([]string, 0, 2)
		if statement != "" {
			parts = append(parts, statement)
		}
		if body != "" {
			parts = append(parts, body)
		}
		rawText = strings.Join(parts, "\n\n")
	}
	if rawText == "" {
		rawText = strings.TrimSpace(fallback)
	}
	if rawText == "" {
		return ""
	}

	converted := bbcodeToMarkdown(rawText)
	sanitized := sanitizeMarkdownNoImages(converted)
	return removeExplicitImageURLs(sanitized, posterScreenshotURLs)
}

func sanitizeMarkdownNoImages(text string) string {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return ""
	}
	cleaned = reRousiBBCodeImage.ReplaceAllString(cleaned, "")
	cleaned = reRousiHTMLImage.ReplaceAllString(cleaned, "")
	cleaned = reRousiMarkdownImage.ReplaceAllString(cleaned, "")

	lines := strings.Split(cleaned, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && reRousiOnlyImageURLLine.MatchString(trimmed) {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}

func bbcodeToMarkdown(text string) string {
	source := strings.TrimSpace(text)
	if source == "" {
		return ""
	}
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.ReplaceAll(source, "\r", "\n")

	convertInline := func(s string) string {
		out := s

		out = reRousiBBCodeCode.ReplaceAllStringFunc(out, func(match string) string {
			sub := reRousiBBCodeCode.FindStringSubmatch(match)
			body := ""
			if len(sub) >= 2 {
				body = strings.Trim(sub[1], "\n")
			}
			if body == "" {
				return ""
			}
			return "\n```\n" + body + "\n```\n"
		})

		out = reRousiBBCodeURLWithArg.ReplaceAllStringFunc(out, func(match string) string {
			sub := reRousiBBCodeURLWithArg.FindStringSubmatch(match)
			if len(sub) < 3 {
				return match
			}
			url := strings.TrimSpace(sub[1])
			label := strings.TrimSpace(sub[2])
			if url == "" || label == "" {
				return label
			}
			return "[" + label + "](" + url + ")"
		})
		out = reRousiBBCodeURL.ReplaceAllStringFunc(out, func(match string) string {
			sub := reRousiBBCodeURL.FindStringSubmatch(match)
			if len(sub) < 2 {
				return match
			}
			url := strings.TrimSpace(sub[1])
			if url == "" {
				return ""
			}
			return "[" + url + "](" + url + ")"
		})

		out = reRousiBBCodeColor.ReplaceAllString(out, "$1")
		out = reRousiBBCodeSize.ReplaceAllString(out, "$1")
		out = reRousiBBCodeBold.ReplaceAllString(out, "**$1**")
		out = reRousiBBCodeItalic.ReplaceAllString(out, "*$1*")
		out = reRousiBBCodeUnderline.ReplaceAllString(out, "$1")

		out = strings.ReplaceAll(out, "[list]", "")
		out = strings.ReplaceAll(out, "[/list]", "")
		out = strings.ReplaceAll(out, "[*]", "- ")

		out = stripRemainingBBCodeTags(out)
		return out
	}

	parts := []string{}
	lastEnd := 0
	matches := reRousiBBCodeQuote.FindAllStringSubmatchIndex(source, -1)
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		before := source[lastEnd:match[0]]
		if strings.TrimSpace(before) != "" {
			parts = append(parts, strings.TrimSpace(convertInline(before)))
		}

		quoteBody := ""
		if match[2] >= 0 && match[3] >= 0 {
			quoteBody = strings.Trim(source[match[2]:match[3]], "\n")
		}
		quoteConverted := strings.TrimSpace(convertInline(quoteBody))
		if quoteConverted != "" {
			lines := strings.Split(quoteConverted, "\n")
			for i, line := range lines {
				lines[i] = strings.TrimRight("> "+line, " ")
			}
			parts = append(parts, strings.TrimRight(strings.Join(lines, "\n"), "\n"))
		}
		lastEnd = match[1]
	}

	tail := source[lastEnd:]
	if strings.TrimSpace(tail) != "" {
		parts = append(parts, strings.TrimSpace(convertInline(tail)))
	}

	out := strings.Join(parts, "\n\n")
	out = reRousiManyNewlines.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out)
}

func stripRemainingBBCodeTags(text string) string {
	if text == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(text))

	for i := 0; i < len(text); {
		if text[i] != '[' {
			b.WriteByte(text[i])
			i++
			continue
		}
		end := strings.IndexByte(text[i:], ']')
		if end < 0 {
			b.WriteByte(text[i])
			i++
			continue
		}
		end = i + end
		tagContent := text[i+1 : end]
		nextPos := end + 1
		if nextPos < len(text) && text[nextPos] == '(' {
			b.WriteString(text[i : end+1])
			i = end + 1
			continue
		}
		if isProbableBBCodeTag(tagContent) {
			i = end + 1
			continue
		}
		b.WriteString(text[i : end+1])
		i = end + 1
	}

	return b.String()
}

func isProbableBBCodeTag(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return false
	}

	nameEnd := 0
	for nameEnd < len(trimmed) {
		ch := trimmed[nameEnd]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			nameEnd++
			continue
		}
		break
	}
	if nameEnd == 0 {
		return false
	}
	name := trimmed[:nameEnd]
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
			return true
		}
	}
	return false
}

func removeExplicitImageURLs(text string, imageURLs []string) string {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" || len(imageURLs) == 0 {
		return cleaned
	}

	for _, url := range imageURLs {
		trimmed := strings.TrimSpace(url)
		if trimmed == "" {
			continue
		}
		cleaned = strings.ReplaceAll(cleaned, trimmed, "")
	}

	lines := strings.Split(cleaned, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}

	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if start >= end {
		return ""
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}

func looksLikeExistingTorrentForRousi(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	return strings.Contains(trimmed, "种子已存在") ||
		strings.Contains(trimmed, "该种子已存在") ||
		strings.Contains(trimmed, "已存在") ||
		strings.Contains(lower, "already exists")
}

func summarizeRousiResponseBody(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	summary := strings.Join(strings.Fields(trimmed), " ")
	if len(summary) > 300 {
		return summary[:300] + "..."
	}
	return summary
}

func extractUUID(text string) string {
	match := reRousiUUID.FindStringSubmatch(text)
	if len(match) >= 2 {
		return strings.ToLower(strings.TrimSpace(match[1]))
	}
	return ""
}

func mapStringAny(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if typed, ok := value.(map[string]any); ok && typed != nil {
		return typed
	}
	if typed, ok := value.(map[string]interface{}); ok && typed != nil {
		copied := make(map[string]any, len(typed))
		for key, item := range typed {
			copied[key] = item
		}
		return copied
	}
	return map[string]any{}
}

func toStringLoose(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []byte:
		return string(typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int8:
		return fmt.Sprintf("%d", typed)
	case int16:
		return fmt.Sprintf("%d", typed)
	case int32:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case uint:
		return fmt.Sprintf("%d", typed)
	case uint8:
		return fmt.Sprintf("%d", typed)
	case uint16:
		return fmt.Sprintf("%d", typed)
	case uint32:
		return fmt.Sprintf("%d", typed)
	case uint64:
		return fmt.Sprintf("%d", typed)
	case float32:
		return fmt.Sprintf("%v", typed)
	case float64:
		return fmt.Sprintf("%v", typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", value)
	}
}

func toFloatLoose(value any) float64 {
	switch typed := value.(type) {
	case int:
		return float64(typed)
	case int8:
		return float64(typed)
	case int16:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	case uint:
		return float64(typed)
	case uint8:
		return float64(typed)
	case uint16:
		return float64(typed)
	case uint32:
		return float64(typed)
	case uint64:
		return float64(typed)
	case float32:
		return float64(typed)
	case float64:
		return typed
	case json.Number:
		if number, err := typed.Float64(); err == nil {
			return number
		}
		return 0
	case string:
		if number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return number
		}
		return 0
	default:
		return 0
	}
}
