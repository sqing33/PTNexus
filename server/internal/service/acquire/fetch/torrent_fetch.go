package fetch

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/platform/logx"
)

var (
	reDownloadLink = regexp.MustCompile(`(?is)href\s*=\s*["']([^"']*(?:download\.php[^"']*|/api/torrent/[^"']*/download/[^"']+|[^"']*\.torrent[^"']*))["']`)
	reIDInURL      = regexp.MustCompile(`id=(\d+)`)
	reTorrentIDURL = regexp.MustCompile(`torrent_id=(\d+)`)
	reUUIDInURL    = regexp.MustCompile(`([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`)
	rePathUnsafe   = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
)

const sourceDownloadLogModule = "迁移-种子下载"

// SiteInfoReader 定义源站信息读取接口。
// 参数/返回：按站点名称读取站点配置行。
// 失败场景：站点不存在或数据库读取异常。
// 副作用：无。
type SiteInfoReader interface {
	GetSiteByName(name string) (map[string]any, error)
}

// TorrentMeta 表示 torrent 元信息。
// 参数/返回：包含种子名称、总大小与 infohash。
// 失败场景：不适用。
// 副作用：无。
type TorrentMeta struct {
	Name     string
	Size     int64
	InfoHash string
}

// TorrentContentMeta 表示 .torrent 内容解析后的元信息与文件名采样。
type TorrentContentMeta struct {
	Meta      TorrentMeta
	FileNames []string
}

type bdecodeParser struct {
	data []byte
	idx  int
}

// PrepareSourceSite 校验并返回源站配置。
// 参数/返回：reader 用于读取站点；siteName 为站点名称；返回站点配置。
// 失败场景：站点不存在、未开启迁移、缺少 cookie/passkey。
// 副作用：无。
func PrepareSourceSite(reader SiteInfoReader, siteName string) (map[string]any, error) {
	siteInfo, err := reader.GetSiteByName(siteName)
	if err != nil {
		return nil, fmt.Errorf("源站点 '%s' 不存在", siteName)
	}
	migration := int(toFloatAny(siteInfo["migration"]))
	if migration != 1 && migration != 3 {
		return nil, fmt.Errorf("站点 '%s' 不允许作为源站点", siteName)
	}
	cookie := strings.TrimSpace(toStringAny(siteInfo["cookie"], ""))
	passkey := strings.TrimSpace(toStringAny(siteInfo["passkey"], ""))
	if cookie == "" && passkey == "" {
		return nil, fmt.Errorf("源站点 '%s' 缺少 cookie/passkey", siteName)
	}
	return siteInfo, nil
}

// DownloadTorrentForSource 根据源站配置下载种子文件并写入临时目录。
// 参数/返回：sourceInfo 为站点配置，torrentID 为详情页 ID/URL；返回 torrent 文件路径、详情页 URL 与字节内容。
// 失败场景：详情页不可访问、下载链接无效、下载内容不是 torrent、写文件失败。
// 副作用：会发起网络请求并在 data/tmp/torrents 写入临时文件。
func DownloadTorrentForSource(sourceInfo map[string]any, torrentID string) (string, string, []byte, error) {
	trimmedID := strings.TrimSpace(torrentID)
	if trimmedID == "" {
		logx.Warnf(sourceDownloadLogModule, "参数校验失败 torrent_id 为空")
		return "", "", nil, errors.New("torrent_id 不能为空")
	}
	baseURL := normalizeSiteBaseURL(toStringAny(sourceInfo["base_url"], ""))
	siteCode := strings.ToLower(strings.TrimSpace(toStringAny(sourceInfo["site"], "")))
	if siteCode == "" {
		siteCode = strings.ToLower(strings.TrimSpace(toStringAny(sourceInfo["nickname"], "site")))
	}
	cookie := strings.TrimSpace(toStringAny(sourceInfo["cookie"], ""))
	passkey := strings.TrimSpace(toStringAny(sourceInfo["passkey"], ""))
	siteName := strings.TrimSpace(toStringAny(sourceInfo["nickname"], siteCode))
	logx.Infof(sourceDownloadLogModule, "开始下载 source_site=%s site_code=%s torrent_id=%s base_url=%s", siteName, siteCode, trimmedID, baseURL)

	detailURL := trimmedID
	if !strings.HasPrefix(strings.ToLower(detailURL), "http://") && !strings.HasPrefix(strings.ToLower(detailURL), "https://") {
		if baseURL == "" {
			logx.Warnf(sourceDownloadLogModule, "构造详情页失败 source_site=%s torrent_id=%s 缺少base_url", siteName, trimmedID)
			return "", "", nil, fmt.Errorf("站点 '%s' 缺少 base_url，无法构造详情页", toStringAny(sourceInfo["nickname"], siteCode))
		}
		detailURL = buildDetailURL(baseURL, siteCode, trimmedID)
	}
	logx.Infof(sourceDownloadLogModule, "详情页地址已确定 source_site=%s torrent_id=%s detail_url=%s", siteName, trimmedID, detailURL)

	downloadCandidates := make([]string, 0)
	if direct := buildDirectDownloadURL(baseURL, siteCode, detailURL, trimmedID, passkey); direct != "" {
		downloadCandidates = append(downloadCandidates, direct)
	}

	html, htmlErr := fetchPageWithCookie(detailURL, cookie, 45*time.Second)
	if htmlErr != nil && errors.Is(htmlErr, ErrSourceCookieExpired) && strings.TrimSpace(passkey) == "" {
		logx.Warnf(sourceDownloadLogModule, "详情页鉴权失败 source_site=%s torrent_id=%s detail_url=%s err=%v", siteName, trimmedID, detailURL, htmlErr)
		return "", detailURL, nil, htmlErr
	}
	if htmlErr == nil && strings.TrimSpace(html) != "" {
		links := extractDownloadCandidatesFromDetail(html, baseURL, siteCode, detailURL)
		downloadCandidates = append(downloadCandidates, links...)
		logx.Infof(sourceDownloadLogModule, "详情页解析成功 source_site=%s torrent_id=%s html_len=%d 新增候选=%d", siteName, trimmedID, len(html), len(links))
	} else if htmlErr != nil {
		logx.Warnf(sourceDownloadLogModule, "详情页获取失败 source_site=%s torrent_id=%s detail_url=%s err=%v", siteName, trimmedID, detailURL, htmlErr)
	}

	downloadCandidates = deduplicateURLs(downloadCandidates)
	logx.Infof(sourceDownloadLogModule, "下载候选已生成 source_site=%s torrent_id=%s 候选数量=%d", siteName, trimmedID, len(downloadCandidates))
	if len(downloadCandidates) == 0 {
		if htmlErr != nil {
			logx.Warnf(sourceDownloadLogModule, "下载候选为空 source_site=%s torrent_id=%s 原因=详情页获取失败", siteName, trimmedID)
			return "", detailURL, nil, fmt.Errorf("未能获取详情页并解析下载链接: %v", htmlErr)
		}
		logx.Warnf(sourceDownloadLogModule, "下载候选为空 source_site=%s torrent_id=%s 原因=详情页无有效链接", siteName, trimmedID)
		return "", detailURL, nil, errors.New("详情页中未找到可用下载链接")
	}

	var torrentBytes []byte
	var lastErr error
	var cookieExpiredErr error
	for _, candidate := range downloadCandidates {
		logx.Infof(sourceDownloadLogModule, "尝试下载候选 source_site=%s torrent_id=%s candidate=%s", siteName, trimmedID, candidate)
		body, err := fetchBinaryWithCookie(candidate, detailURL, cookie, 90*time.Second)
		if err != nil {
			logx.Warnf(sourceDownloadLogModule, "候选下载失败 source_site=%s torrent_id=%s candidate=%s err=%v", siteName, trimmedID, candidate, err)
			if errors.Is(err, ErrSourceCookieExpired) {
				cookieExpiredErr = err
				if strings.TrimSpace(passkey) == "" {
					lastErr = err
					break
				}
			}
			lastErr = err
			continue
		}
		if !isLikelyTorrent(body) {
			logx.Warnf(sourceDownloadLogModule, "候选内容无效 source_site=%s torrent_id=%s candidate=%s bytes=%d", siteName, trimmedID, candidate, len(body))
			lastErr = fmt.Errorf("下载内容不是 torrent 文件: %s", candidate)
			continue
		}
		torrentBytes = body
		logx.Infof(sourceDownloadLogModule, "候选下载成功 source_site=%s torrent_id=%s candidate=%s bytes=%d", siteName, trimmedID, candidate, len(body))
		break
	}
	if len(torrentBytes) == 0 {
		if lastErr == nil {
			lastErr = errors.New("下载失败")
		}
		if cookieExpiredErr != nil {
			lastErr = cookieExpiredErr
		}
		logx.Errorf(sourceDownloadLogModule, "全部候选下载失败 source_site=%s torrent_id=%s err=%v", siteName, trimmedID, lastErr)
		return "", detailURL, nil, lastErr
	}

	paths := config.ResolveRuntimePaths()
	torrentDir := filepath.Join(paths.DataDir, "tmp", "torrents")
	if err := os.MkdirAll(torrentDir, 0o755); err != nil {
		logx.Errorf(sourceDownloadLogModule, "创建临时目录失败 source_site=%s torrent_id=%s dir=%s err=%v", siteName, trimmedID, torrentDir, err)
		return "", detailURL, nil, err
	}
	fileName := fmt.Sprintf("%s-%s-%d.torrent", sanitizeTorrentFilePart(siteCode), sanitizeTorrentFilePart(trimmedID), time.Now().UnixNano())
	torrentPath := filepath.Join(torrentDir, fileName)
	if err := os.WriteFile(torrentPath, torrentBytes, 0o644); err != nil {
		logx.Errorf(sourceDownloadLogModule, "写入临时种子失败 source_site=%s torrent_id=%s torrent_path=%s err=%v", siteName, trimmedID, torrentPath, err)
		return "", detailURL, nil, err
	}
	logx.Infof(sourceDownloadLogModule, "下载流程完成 source_site=%s torrent_id=%s torrent_path=%s detail_url=%s", siteName, trimmedID, torrentPath, detailURL)
	return torrentPath, detailURL, torrentBytes, nil
}

// NormalizeSiteBaseURL 规范化站点根地址。
// 参数/返回：输入任意 base_url 文本，返回补全协议并去除尾部斜杠后的 URL。
// 失败场景：不适用。
// 副作用：无。
func NormalizeSiteBaseURL(baseURL string) string {
	return normalizeSiteBaseURL(baseURL)
}

// FetchPageWithCookie 使用 Cookie 抓取页面 HTML 文本。
// 参数/返回：targetURL 为页面地址，cookie 为站点登录态，timeout 为请求超时；返回页面文本。
// 失败场景：网络错误、HTTP 非 2xx、读取响应失败。
// 副作用：发起网络请求。
func FetchPageWithCookie(targetURL, cookie string, timeout time.Duration) (string, error) {
	return fetchPageWithCookie(targetURL, cookie, timeout)
}

// MinInt 返回两个整数中的较小值。
// 参数/返回：a、b 为输入整数，返回较小者。
// 失败场景：不适用。
// 副作用：无。
func MinInt(a, b int) int {
	return minInt(a, b)
}

// ParseTorrentMeta 解析 torrent 字节并提取 name/size/infohash。
// 参数/返回：content 为 torrent 文件内容，返回解析后的元信息。
// 失败场景：bencode 非法、缺少 info 字段、结构异常。
// 副作用：无。
func ParseTorrentMeta(content []byte) (TorrentMeta, error) {
	parsed, err := ParseTorrentContentMeta(content)
	if err != nil {
		return TorrentMeta{}, err
	}
	return parsed.Meta, nil
}

// ParseTorrentContentMeta 解析 torrent 字节并提取 name/size/infohash/文件名列表。
// 参数/返回：content 为 torrent 文件内容，返回解析后的元信息与 info.files 文件名。
// 失败场景：bencode 非法、缺少 info 字段、结构异常。
// 副作用：无。
func ParseTorrentContentMeta(content []byte) (TorrentContentMeta, error) {
	if len(content) == 0 {
		return TorrentContentMeta{}, errors.New("torrent 内容为空")
	}
	p := &bdecodeParser{data: content}
	if err := p.expect('d'); err != nil {
		return TorrentContentMeta{}, err
	}
	var infoValue any
	infoStart := -1
	infoEnd := -1
	for p.idx < len(p.data) && p.data[p.idx] != 'e' {
		keyBytes, err := p.parseBytes()
		if err != nil {
			return TorrentContentMeta{}, err
		}
		key := string(keyBytes)
		if key == "info" {
			infoStart = p.idx
			value, err := p.parseValue()
			if err != nil {
				return TorrentContentMeta{}, err
			}
			infoEnd = p.idx
			infoValue = value
			continue
		}
		if _, err := p.parseValue(); err != nil {
			return TorrentContentMeta{}, err
		}
	}
	if err := p.expect('e'); err != nil {
		return TorrentContentMeta{}, err
	}
	if infoStart < 0 || infoEnd <= infoStart || infoValue == nil {
		return TorrentContentMeta{}, errors.New("torrent 缺少 info 字段")
	}

	infoBytes := content[infoStart:infoEnd]
	hash := sha1.Sum(infoBytes)
	meta := TorrentMeta{InfoHash: strings.ToLower(hex.EncodeToString(hash[:]))}

	infoMap, ok := infoValue.(map[string]any)
	if !ok {
		return TorrentContentMeta{}, errors.New("torrent info 结构异常")
	}
	meta.Name = strings.TrimSpace(firstNonEmptyString(infoMap["name.utf-8"], infoMap["name"]))
	if meta.Name == "" {
		meta.Name = meta.InfoHash
	}
	meta.Size = calculateTorrentSize(infoMap)
	if meta.Size < 0 {
		meta.Size = 0
	}
	return TorrentContentMeta{
		Meta:      meta,
		FileNames: extractTorrentFileNames(infoMap),
	}, nil
}

func extractTorrentFileNames(info map[string]any) []string {
	if len(info) == 0 {
		return []string{}
	}

	files, ok := info["files"].([]any)
	if !ok {
		name := strings.TrimSpace(firstNonEmptyString(info["name.utf-8"], info["name"]))
		if name == "" {
			return []string{}
		}
		return []string{name}
	}

	result := make([]string, 0, len(files))
	seen := map[string]struct{}{}
	for _, item := range files {
		fileMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pathValue, ok := fileMap["path.utf-8"]
		if !ok {
			pathValue = fileMap["path"]
		}
		pathList, ok := pathValue.([]any)
		if !ok || len(pathList) == 0 {
			continue
		}
		name := strings.TrimSpace(toStringAny(pathList[len(pathList)-1], ""))
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

func (p *bdecodeParser) expect(ch byte) error {
	if p.idx >= len(p.data) || p.data[p.idx] != ch {
		return fmt.Errorf("bdecode expect %q at %d", ch, p.idx)
	}
	p.idx++
	return nil
}

func (p *bdecodeParser) parseValue() (any, error) {
	if p.idx >= len(p.data) {
		return nil, errors.New("bdecode unexpected eof")
	}
	switch p.data[p.idx] {
	case 'i':
		return p.parseInt()
	case 'l':
		p.idx++
		items := make([]any, 0)
		for p.idx < len(p.data) && p.data[p.idx] != 'e' {
			item, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		if err := p.expect('e'); err != nil {
			return nil, err
		}
		return items, nil
	case 'd':
		p.idx++
		m := map[string]any{}
		for p.idx < len(p.data) && p.data[p.idx] != 'e' {
			keyBytes, err := p.parseBytes()
			if err != nil {
				return nil, err
			}
			value, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			m[string(keyBytes)] = value
		}
		if err := p.expect('e'); err != nil {
			return nil, err
		}
		return m, nil
	default:
		if p.data[p.idx] >= '0' && p.data[p.idx] <= '9' {
			return p.parseBytes()
		}
		return nil, fmt.Errorf("bdecode invalid token %q at %d", p.data[p.idx], p.idx)
	}
}

func (p *bdecodeParser) parseInt() (int64, error) {
	if err := p.expect('i'); err != nil {
		return 0, err
	}
	start := p.idx
	for p.idx < len(p.data) && p.data[p.idx] != 'e' {
		p.idx++
	}
	if p.idx >= len(p.data) {
		return 0, errors.New("bdecode int missing terminator")
	}
	text := string(p.data[start:p.idx])
	if err := p.expect('e'); err != nil {
		return 0, err
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func (p *bdecodeParser) parseBytes() ([]byte, error) {
	start := p.idx
	for p.idx < len(p.data) && p.data[p.idx] >= '0' && p.data[p.idx] <= '9' {
		p.idx++
	}
	if p.idx >= len(p.data) || p.data[p.idx] != ':' || p.idx == start {
		return nil, fmt.Errorf("bdecode string length invalid at %d", start)
	}
	lengthText := string(p.data[start:p.idx])
	p.idx++
	length, err := strconv.Atoi(lengthText)
	if err != nil {
		return nil, err
	}
	if length < 0 || p.idx+length > len(p.data) {
		return nil, errors.New("bdecode string out of bounds")
	}
	value := p.data[p.idx : p.idx+length]
	p.idx += length
	return value, nil
}

func calculateTorrentSize(info map[string]any) int64 {
	if val, ok := info["length"]; ok {
		return toInt64Any(val)
	}
	files, ok := info["files"].([]any)
	if !ok {
		return 0
	}
	var total int64
	for _, item := range files {
		fileMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		total += toInt64Any(fileMap["length"])
	}
	return total
}

func toInt64Any(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		text := strings.TrimSpace(toStringAny(value, ""))
		if text != "" {
			return text
		}
	}
	return ""
}

func normalizeSiteBaseURL(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(trimmed), "http://") && !strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		trimmed = "https://" + trimmed
	}
	return strings.TrimRight(trimmed, "/")
}

func buildDetailURL(baseURL, siteCode, torrentID string) string {
	trimmed := strings.TrimSpace(torrentID)
	if trimmed == "" {
		return baseURL
	}
	if strings.Contains(trimmed, "details.php") || strings.Contains(trimmed, "torrent/") {
		if strings.HasPrefix(strings.ToLower(trimmed), "http://") || strings.HasPrefix(strings.ToLower(trimmed), "https://") {
			return trimmed
		}
		return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(trimmed, "/")
	}
	if strings.Contains(strings.ToLower(siteCode), "rousi") && reUUIDInURL.MatchString(trimmed) {
		return fmt.Sprintf("%s/torrent/%s", strings.TrimRight(baseURL, "/"), reUUIDInURL.FindString(trimmed))
	}
	if reIDInURL.MatchString(trimmed) {
		id := reIDInURL.FindStringSubmatch(trimmed)[1]
		return fmt.Sprintf("%s/details.php?id=%s", strings.TrimRight(baseURL, "/"), id)
	}
	if _, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return fmt.Sprintf("%s/details.php?id=%s", strings.TrimRight(baseURL, "/"), trimmed)
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(trimmed, "/")
}

func buildDirectDownloadURL(baseURL, siteCode, detailURL, torrentID, passkey string) string {
	if baseURL == "" {
		return ""
	}
	trimmedPasskey := strings.TrimSpace(passkey)
	if strings.Contains(strings.ToLower(siteCode), "rousi") && trimmedPasskey != "" {
		uuid := reUUIDInURL.FindString(detailURL)
		if uuid != "" {
			return fmt.Sprintf("%s/api/torrent/%s/download/%s", strings.TrimRight(baseURL, "/"), uuid, neturl.PathEscape(trimmedPasskey))
		}
	}
	id := extractDownloadID(siteCode, detailURL, torrentID)
	if id == "" {
		return ""
	}
	if trimmedPasskey != "" {
		return fmt.Sprintf("%s/download.php?id=%s&passkey=%s", strings.TrimRight(baseURL, "/"), id, neturl.QueryEscape(trimmedPasskey))
	}
	return fmt.Sprintf("%s/download.php?id=%s", strings.TrimRight(baseURL, "/"), id)
}

func extractDownloadCandidatesFromDetail(html, baseURL, siteCode, detailURL string) []string {
	matches := reDownloadLink.FindAllStringSubmatch(html, -1)
	candidates := make([]string, 0, len(matches))
	haidanTorrentID := ""
	if strings.Contains(strings.ToLower(strings.TrimSpace(siteCode)), "haidan") {
		haidanTorrentID = extractTorrentIDParam(detailURL)
	}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		candidate := strings.TrimSpace(match[1])
		if candidate == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(candidate), "javascript:") {
			continue
		}
		if strings.HasPrefix(candidate, "//") {
			candidate = "https:" + candidate
		} else if !strings.HasPrefix(strings.ToLower(candidate), "http://") && !strings.HasPrefix(strings.ToLower(candidate), "https://") {
			candidate = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(candidate, "/")
		}
		if haidanTorrentID != "" {
			candidate = rewriteDownloadIDParam(candidate, haidanTorrentID)
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func extractDownloadID(siteCode, detailURL, torrentID string) string {
	trimmedSiteCode := strings.ToLower(strings.TrimSpace(siteCode))
	if strings.Contains(trimmedSiteCode, "haidan") {
		if id := extractTorrentIDParam(detailURL); id != "" {
			return id
		}
		if id := extractTorrentIDParam(torrentID); id != "" {
			return id
		}
	}

	if sub := reIDInURL.FindStringSubmatch(detailURL); len(sub) >= 2 {
		return sub[1]
	}
	if sub := reIDInURL.FindStringSubmatch(torrentID); len(sub) >= 2 {
		return sub[1]
	}
	if _, err := strconv.ParseInt(strings.TrimSpace(torrentID), 10, 64); err == nil {
		return strings.TrimSpace(torrentID)
	}
	return ""
}

func extractTorrentIDParam(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if sub := reTorrentIDURL.FindStringSubmatch(trimmed); len(sub) >= 2 {
		return strings.TrimSpace(sub[1])
	}
	parsed, err := neturl.Parse(trimmed)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get("torrent_id"))
}

func rewriteDownloadIDParam(candidate, desiredID string) string {
	trimmedCandidate := strings.TrimSpace(candidate)
	trimmedDesiredID := strings.TrimSpace(desiredID)
	if trimmedCandidate == "" || trimmedDesiredID == "" {
		return trimmedCandidate
	}
	parsed, err := neturl.Parse(trimmedCandidate)
	if err != nil {
		return reIDInURL.ReplaceAllString(trimmedCandidate, "id="+trimmedDesiredID)
	}
	query := parsed.Query()
	if query.Get("id") == "" {
		return trimmedCandidate
	}
	query.Set("id", trimmedDesiredID)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func deduplicateURLs(urls []string) []string {
	result := make([]string, 0, len(urls))
	seen := map[string]struct{}{}
	for _, item := range urls {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func fetchPageWithCookie(targetURL, cookie string, timeout time.Duration) (string, error) {
	body, err := fetchBinaryWithCookie(targetURL, "", cookie, timeout)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func fetchBinaryWithCookie(targetURL, referer, cookie string, timeout time.Duration) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	if strings.TrimSpace(cookie) != "" {
		request.Header.Set("Cookie", cookie)
	}
	if strings.TrimSpace(referer) != "" {
		request.Header.Set("Referer", referer)
	}
	redirectCount := 0
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			redirectCount = len(via)
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			return nil
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	finalURL := ""
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL.String()
	}
	if isLikelyLoginResponse(response.StatusCode, finalURL, body) {
		redactedTarget := redactURLQuery(targetURL)
		redactedFinal := redactURLQuery(finalURL)
		logx.Warnf(
			sourceDownloadLogModule,
			"请求命中登录页 target_url=%s final_url=%s status=%d redirects=%d",
			redactedTarget,
			redactedFinal,
			response.StatusCode,
			redirectCount,
		)
		return nil, ErrSourceCookieExpired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func redactURLQuery(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := neturl.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func isLikelyLoginResponse(statusCode int, finalURL string, body []byte) bool {
	if isLoginURL(finalURL) {
		return true
	}
	if statusCode < 200 || statusCode >= 400 {
		return false
	}
	return isLikelyLoginHTML(body)
}

func isLoginURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	parsed, err := neturl.Parse(trimmed)
	if err != nil {
		return strings.Contains(strings.ToLower(trimmed), "login.php")
	}
	path := strings.ToLower(strings.TrimSpace(parsed.Path))
	if strings.HasSuffix(path, "/login.php") || path == "login.php" {
		return true
	}
	query := strings.ToLower(parsed.RawQuery)
	return strings.Contains(path, "login.php") && strings.Contains(query, "returnto=")
}

func isLikelyLoginHTML(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	limit := minInt(len(content), 4096)
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

func isLikelyTorrent(content []byte) bool {
	if len(content) < 32 {
		return false
	}
	prefix := strings.ToLower(strings.TrimSpace(string(content[:minInt(len(content), 96)])))
	if strings.HasPrefix(prefix, "<html") || strings.HasPrefix(prefix, "<!doctype") {
		return false
	}
	if strings.Contains(prefix, "access denied") || strings.Contains(prefix, "cloudflare") {
		return false
	}
	return bytesHasPrefix(content, []byte("d"))
}

func bytesHasPrefix(content []byte, prefix []byte) bool {
	if len(content) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if content[i] != prefix[i] {
			return false
		}
	}
	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sanitizeTorrentFilePart(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	cleaned := rePathUnsafe.ReplaceAllString(trimmed, "_")
	cleaned = strings.Trim(cleaned, "._-")
	if cleaned == "" {
		return "unknown"
	}
	return cleaned
}

func toStringAny(value any, fallback string) string {
	switch typed := value.(type) {
	case nil:
		return fallback
	case string:
		return typed
	case []byte:
		return string(typed)
	case int:
		return strconv.Itoa(typed)
	case int8:
		return strconv.FormatInt(int64(typed), 10)
	case int16:
		return strconv.FormatInt(int64(typed), 10)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint:
		return strconv.FormatUint(uint64(typed), 10)
	case uint8:
		return strconv.FormatUint(uint64(typed), 10)
	case uint16:
		return strconv.FormatUint(uint64(typed), 10)
	case uint32:
		return strconv.FormatUint(uint64(typed), 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fallback
	}
}

func toFloatAny(value any) float64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case float64:
		return typed
	case float32:
		return float64(typed)
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
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0
		}
		if parsed, err := strconv.ParseFloat(text, 64); err == nil {
			return parsed
		}
		return 0
	default:
		return 0
	}
}
