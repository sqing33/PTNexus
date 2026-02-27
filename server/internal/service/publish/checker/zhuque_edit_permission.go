package checker

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reZhuqueEditEntry = regexp.MustCompile(`(?is)<a[^>]*>\s*\[?编辑种子\]?\s*</a>`)
	reZhuqueAntSpace  = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*ant-space[^"']*ant-space-align-center[^"']*["'][^>]*>.*?<span[^>]*>(.*?)</span>`)
	reZhuqueUserSide  = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*user-info-side[^"']*["'][^>]*>`)
	reZhuqueUserName  = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*user-info-side[^"']*["'][^>]*>.*?<div[^>]*class=["'][^"']*ant-space[^"']*ant-space-align-center[^"']*["'][^>]*>.*?<span[^>]*>(.*?)</span>`)
)

func looksLikeZhuqueDetailPage(detailHTML string) bool {
	trimmed := strings.TrimSpace(detailHTML)
	if trimmed == "" {
		return false
	}
	// 朱雀（TNode）详情页的稳定特征：user-info-side（左侧用户信息）+ 发布于（详情页发布时间文案）
	return reZhuqueUserSide.MatchString(trimmed) && strings.Contains(trimmed, "发布于")
}

func checkZhuqueExistingTorrentEditPermission(detailHTML string) (bool, string) {
	trimmedHTML := strings.TrimSpace(detailHTML)
	if trimmedHTML == "" {
		return false, "详情页内容为空"
	}
	if !reZhuqueEditEntry.MatchString(trimmedHTML) {
		return false, "详情页未检测到编辑入口"
	}

	publisherName := extractZhuquePublisherName(trimmedHTML)
	if publisherName == "" {
		return false, "详情页未解析到发布者"
	}
	currentUserName := extractZhuqueCurrentLoginName(trimmedHTML)
	if currentUserName == "" {
		return false, "详情页未解析到当前登录用户"
	}
	if !strings.EqualFold(publisherName, currentUserName) {
		return false, fmt.Sprintf("详情页发布者(%s)与当前登录用户(%s)不一致", publisherName, currentUserName)
	}
	return true, ""
}

func extractZhuqueCurrentLoginName(detailHTML string) string {
	match := reZhuqueUserName.FindStringSubmatch(detailHTML)
	if len(match) < 2 {
		return ""
	}
	return normalizeExtractedName(match[1])
}

func extractZhuquePublisherName(detailHTML string) string {
	// 以“发布于”为锚点，向前取一段窗口，抓取最靠近“发布于”的 ant-space 用户名。
	anchor := strings.Index(detailHTML, "发布于")
	if anchor < 0 {
		return ""
	}

	start := anchor - 8000
	if start < 0 {
		start = 0
	}
	window := detailHTML[start:anchor]

	matches := reZhuqueAntSpace.FindAllStringSubmatch(window, -1)
	for idx := len(matches) - 1; idx >= 0; idx-- {
		if len(matches[idx]) < 2 {
			continue
		}
		if candidate := normalizeExtractedName(matches[idx][1]); candidate != "" {
			return candidate
		}
	}
	return ""
}
