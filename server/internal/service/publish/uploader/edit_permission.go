package uploader

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

var reEditPermissionHTMLTag = regexp.MustCompile(`(?is)<[^>]+>`)

// CheckExistingTorrentEditPermission 校验“已存在种子自动编辑”是否允许执行。
// 参数/返回：detailHTML 为详情页 HTML，torrentID 为详情页 id；返回是否允许编辑及失败原因。
// 失败场景：缺少编辑按钮、无法解析发布者/当前登录用户、或发布者与当前用户不一致。
// 副作用：无。
func CheckExistingTorrentEditPermission(detailHTML, torrentID string) (bool, string) {
	trimmedHTML := strings.TrimSpace(detailHTML)
	trimmedID := strings.TrimSpace(torrentID)
	if trimmedHTML == "" {
		return false, "详情页内容为空"
	}
	if trimmedID == "" {
		return false, "缺少 torrent_id"
	}
	if !hasEditButtonForTorrent(trimmedHTML, trimmedID) {
		return false, "详情页未检测到编辑按钮"
	}

	publisherName := extractTorrentPublisherName(trimmedHTML, trimmedID)
	if publisherName == "" {
		return false, "详情页未解析到发布者"
	}
	currentUserName := extractCurrentLoginName(trimmedHTML)
	if currentUserName == "" {
		return false, "详情页未解析到当前登录用户"
	}
	if !strings.EqualFold(publisherName, currentUserName) {
		return false, fmt.Sprintf("详情页发布者(%s)与当前登录用户(%s)不一致", publisherName, currentUserName)
	}
	return true, ""
}

func hasEditButtonForTorrent(detailHTML, torrentID string) bool {
	idPattern := regexp.QuoteMeta(strings.TrimSpace(torrentID))
	pattern := regexp.MustCompile(`(?i)href=["'][^"']*edit\.php\?[^"']*\bid=` + idPattern + `\b[^"']*["']`)
	return pattern.MatchString(detailHTML)
}

func extractTorrentPublisherName(detailHTML, torrentID string) string {
	idPattern := regexp.QuoteMeta(strings.TrimSpace(torrentID))
	patterns := []string{
		`(?is)<td[^>]*class=["'][^"']*rowfollow[^"']*["'][^>]*>.*?download\.php\?[^"'>]*\bid=` + idPattern + `\b[^"'>]*.*?由.*?<a[^>]*href=["'][^"']*userdetails\.php[^"']*["'][^>]*>(.*?)</a>.*?发布于.*?</td>`,
		`(?is)由(?:\s|&nbsp;|&#160;)*<span[^>]*>.*?<a[^>]*href=["'][^"']*userdetails\.php[^"']*["'][^>]*>(.*?)</a>.*?发布于`,
	}
	return extractNameByPatterns(detailHTML, patterns)
}

func extractCurrentLoginName(detailHTML string) string {
	patterns := []string{
		`(?is)欢迎回来\s*,?.{0,1200}?<a[^>]*href=["'][^"']*userdetails\.php[^"']*["'][^>]*>(.*?)</a>`,
		`(?is)welcome\s+back\s*,?.{0,1200}?<a[^>]*href=["'][^"']*userdetails\.php[^"']*["'][^>]*>(.*?)</a>`,
	}
	return extractNameByPatterns(detailHTML, patterns)
}

func extractNameByPatterns(detailHTML string, patterns []string) string {
	for _, rawPattern := range patterns {
		pattern := regexp.MustCompile(rawPattern)
		match := pattern.FindStringSubmatch(detailHTML)
		if len(match) < 2 {
			continue
		}
		candidate := normalizeExtractedName(match[1])
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func normalizeExtractedName(raw string) string {
	text := reEditPermissionHTMLTag.ReplaceAllString(strings.TrimSpace(raw), " ")
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}
