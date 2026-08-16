package uploader

import (
	"fmt"
	"html"
	neturl "net/url"
	"regexp"
	"strings"
)

var (
	reOfferID          = regexp.MustCompile(`(?is)offers\.php\?id=(\d+)`)
	reDetailID         = regexp.MustCompile(`(?is)details\.php\?id=(\d+)`)
	reDetailTorrentID  = regexp.MustCompile(`(?is)details\.php\?[^\s"']*torrent_id=(\d+)`)
	reRousiUUIDs       = regexp.MustCompile(`(?is)/torrent/([0-9a-fA-F\-]{36})`)
	reZhuqueID         = regexp.MustCompile(`(?is)/torrent/info/(\d+)`)
	reTTGDownload      = regexp.MustCompile(`(?is)/dl/(\d+)/([a-zA-Z0-9]+)`)
	reDownhashAbsolute = regexp.MustCompile(`(?is)https?://[^"'\s<>]+/download\.php\?[^"'\s<>]*downhash=[^"'\s<>]+`)
	reDownhashRelative = regexp.MustCompile(`(?is)(?:^|["'\s(>])(/?download\.php\?[^"'\s<>]*downhash=[^"'\s<>]+)`)
)

// isTTGDownloadURL 判断 URL 是否为 TTG 的 /dl/{id}/{passkey} 下载链接格式。
func isTTGDownloadURL(raw string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return false
	}
	return reTTGDownload.MatchString(trimmed)
}

// NormalizePublishURLWithOfferSupport 标准化发布链接，并将 offer 链接转换为 details 链接，将 TTG /dl/ 链接转换为详情页链接。
func NormalizePublishURLWithOfferSupport(baseURL, candidate string) string {
	normalized := NormalizePublishURL(baseURL, candidate)
	if normalized == "" {
		return ""
	}
	if match := reOfferID.FindStringSubmatch(normalized); len(match) >= 2 {
		return strings.TrimRight(baseURL, "/") + "/details.php?id=" + match[1]
	}
	// TTG 上传后重定向到 /dl/{id}/{passkey} 下载 URL，转换为详情页 URL 以便后续流程统一处理
	if match := reTTGDownload.FindStringSubmatch(normalized); len(match) >= 2 {
		return strings.TrimRight(baseURL, "/") + "/details.php?id=" + match[1]
	}
	return normalized
}

// ExtractDownhashDownloadURLFromText 从 HTML/跳转文本中提取带 downhash 的下载直链。
// 参数/返回：baseURL 用于补全相对链接，text 为上传响应或详情页 HTML；提取成功返回绝对下载 URL。
// 失败场景：文本为空、链接缺少 id/downhash 或 URL 非法时返回空字符串。
// 副作用：无。
func ExtractDownhashDownloadURLFromText(baseURL, text string) string {
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return ""
	}
	if direct := normalizeDownhashDownloadURL(baseURL, trimmedText); direct != "" {
		return direct
	}
	if match := reDownhashAbsolute.FindString(trimmedText); match != "" {
		if direct := normalizeDownhashDownloadURL(baseURL, match); direct != "" {
			return direct
		}
	}
	for _, match := range reDownhashRelative.FindAllStringSubmatch(trimmedText, -1) {
		if len(match) < 2 {
			continue
		}
		if direct := normalizeDownhashDownloadURL(baseURL, match[1]); direct != "" {
			return direct
		}
	}
	return ""
}

// BuildDirectDownloadURLForPublished 基于发布详情页地址构造目标站直链下载地址。
func BuildDirectDownloadURLForPublished(baseURL string, passkey string, siteCode string, publishURL string) string {
	url := strings.TrimSpace(publishURL)
	if url == "" {
		return ""
	}
	normalizedBase := normalizeBaseURL(baseURL)
	if normalizedBase == "" {
		return ""
	}
	if direct := ExtractDownhashDownloadURLFromText(normalizedBase, url); direct != "" {
		return direct
	}
	trimmedPasskey := strings.TrimSpace(passkey)
	trimmedSiteCode := strings.ToLower(strings.TrimSpace(siteCode))
	if strings.Contains(trimmedSiteCode, "rousi") {
		if trimmedPasskey == "" {
			return ""
		}
		if match := reRousiUUIDs.FindStringSubmatch(url); len(match) >= 2 {
			return fmt.Sprintf("%s/api/torrent/%s/download/%s", strings.TrimRight(normalizedBase, "/"), match[1], neturl.PathEscape(trimmedPasskey))
		}
	}

	if strings.Contains(trimmedSiteCode, "zhuque") {
		match := reZhuqueID.FindStringSubmatch(url)
		if len(match) >= 2 {
			id := strings.TrimSpace(match[1])
			if id != "" && trimmedPasskey != "" {
				return fmt.Sprintf(
					"%s/api/torrent/download/%s/%s",
					strings.TrimRight(normalizedBase, "/"),
					neturl.PathEscape(id),
					neturl.PathEscape(trimmedPasskey),
				)
			}
		}
	}

	id := extractDetailIDForDownload(trimmedSiteCode, url)
	if id == "" {
		return ""
	}

	switch {
	case strings.Contains(trimmedSiteCode, "hddolby"):
		if trimmedPasskey == "" {
			return ""
		}
		return fmt.Sprintf("%s/download.php?id=%s&downhash=%s", strings.TrimRight(normalizedBase, "/"), id, trimmedPasskey)
	case strings.Contains(trimmedSiteCode, "hdhome"):
		return ""
	case strings.Contains(trimmedSiteCode, "hdtime"):
		if trimmedPasskey == "" {
			return ""
		}
		return fmt.Sprintf("%s/download.php?id=%s&passkey=%s&https=1", strings.TrimRight(normalizedBase, "/"), id, trimmedPasskey)
	case strings.Contains(trimmedSiteCode, "ttg"):
		if trimmedPasskey == "" {
			return ""
		}
		return fmt.Sprintf("%s/dl/%s/%s", strings.TrimRight(normalizedBase, "/"), id, trimmedPasskey)
	default:
		if trimmedPasskey == "" {
			return ""
		}
		return fmt.Sprintf("%s/download.php?id=%s&passkey=%s", strings.TrimRight(normalizedBase, "/"), id, trimmedPasskey)
	}
}

func extractDetailIDForDownload(siteCode, publishURL string) string {
	trimmedSiteCode := strings.ToLower(strings.TrimSpace(siteCode))
	if strings.Contains(trimmedSiteCode, "haidan") {
		if id := extractURLQueryValue(publishURL, "torrent_id"); id != "" {
			return id
		}
	}
	if id := extractURLQueryValue(publishURL, "id"); id != "" {
		return id
	}
	if id := extractURLQueryValue(publishURL, "torrent_id"); id != "" {
		return id
	}
	if match := reDetailID.FindStringSubmatch(publishURL); len(match) >= 2 {
		return strings.TrimSpace(match[1])
	}
	if match := reDetailTorrentID.FindStringSubmatch(publishURL); len(match) >= 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func normalizeDownhashDownloadURL(baseURL, candidate string) string {
	trimmed := strings.TrimSpace(html.UnescapeString(candidate))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.Trim(trimmed, `"'<>`)
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "download.php") || !strings.Contains(lower, "downhash=") {
		return ""
	}

	normalized := trimmed
	if strings.HasPrefix(normalized, "//") {
		scheme := "https:"
		if parsedBase, err := neturl.Parse(normalizeBaseURL(baseURL)); err == nil && parsedBase.Scheme != "" {
			scheme = parsedBase.Scheme + ":"
		}
		normalized = scheme + normalized
	} else if strings.HasPrefix(normalized, "/") {
		normalized = strings.TrimRight(normalizeBaseURL(baseURL), "/") + normalized
	} else if !strings.HasPrefix(strings.ToLower(normalized), "http://") && !strings.HasPrefix(strings.ToLower(normalized), "https://") {
		normalized = strings.TrimRight(normalizeBaseURL(baseURL), "/") + "/" + strings.TrimLeft(normalized, "/")
	}

	parsed, err := neturl.Parse(normalized)
	if err != nil {
		return ""
	}
	query := parsed.Query()
	if strings.TrimSpace(query.Get("id")) == "" || strings.TrimSpace(query.Get("downhash")) == "" {
		return ""
	}
	return parsed.String()
}

func extractURLQueryValue(rawURL, key string) string {
	trimmedURL := strings.TrimSpace(rawURL)
	trimmedKey := strings.TrimSpace(key)
	if trimmedURL == "" || trimmedKey == "" {
		return ""
	}
	parsed, err := neturl.Parse(trimmedURL)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get(trimmedKey))
}

func normalizeBaseURL(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(trimmed), "http://") && !strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		trimmed = "https://" + trimmed
	}
	return strings.TrimRight(trimmed, "/")
}
