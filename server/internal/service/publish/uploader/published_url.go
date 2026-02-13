package uploader

import (
	"fmt"
	neturl "net/url"
	"regexp"
	"strings"
)

var (
	reOfferID         = regexp.MustCompile(`(?is)offers\.php\?id=(\d+)`)
	reDetailID        = regexp.MustCompile(`(?is)details\.php\?id=(\d+)`)
	reDetailTorrentID = regexp.MustCompile(`(?is)details\.php\?[^\s"']*torrent_id=(\d+)`)
	reRousiUUIDs      = regexp.MustCompile(`(?is)/torrent/([0-9a-fA-F\-]{36})`)
	reZhuqueID        = regexp.MustCompile(`(?is)/torrent/info/(\d+)`)
)

// NormalizePublishURLWithOfferSupport 标准化发布链接，并将 offer 链接转换为 details 链接。
func NormalizePublishURLWithOfferSupport(baseURL, candidate string) string {
	normalized := NormalizePublishURL(baseURL, candidate)
	if normalized == "" {
		return ""
	}
	if match := reOfferID.FindStringSubmatch(normalized); len(match) >= 2 {
		return strings.TrimRight(baseURL, "/") + "/details.php?id=" + match[1]
	}
	return normalized
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
	case strings.Contains(trimmedSiteCode, "hdtime"):
		if trimmedPasskey == "" {
			return ""
		}
		return fmt.Sprintf("%s/download.php?id=%s&passkey=%s&https=1", strings.TrimRight(normalizedBase, "/"), id, trimmedPasskey)
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
