package checker

import (
	"fmt"
	"strings"
)

// BuildExistingTorrentEditPermissionLog 构建已存在种子自动编辑权限的诊断日志。
// 参数/返回：detailHTML 为详情页 HTML，torrentID 为详情页 id；返回可直接追加到流程日志的单行文本。
// 失败场景：详情页为空时返回空字符串；用户名解析失败时会输出“未解析到”占位文案。
// 副作用：无。
func BuildExistingTorrentEditPermissionLog(detailHTML, torrentID string) string {
	trimmedHTML := strings.TrimSpace(detailHTML)
	trimmedID := strings.TrimSpace(torrentID)
	if trimmedHTML == "" {
		return ""
	}

	if looksLikeZhuqueDetailPage(trimmedHTML) {
		currentUser := normalizeDiagnosticName(extractZhuqueCurrentLoginName(trimmedHTML))
		publisher := normalizeDiagnosticName(extractZhuquePublisherName(trimmedHTML))
		editEntry := "否"
		if reZhuqueEditEntry.MatchString(trimmedHTML) {
			editEntry = "是"
		}
		return fmt.Sprintf("权限校验信息(朱雀): 当前登录用户=%s，发布者=%s，编辑入口=%s", currentUser, publisher, editEntry)
	}

	currentUser := normalizeDiagnosticName(extractCurrentLoginName(trimmedHTML))
	publisher := normalizeDiagnosticName(extractTorrentPublisherName(trimmedHTML, trimmedID))
	editEntry := "否"
	if trimmedID != "" && hasEditButtonForTorrent(trimmedHTML, trimmedID) {
		editEntry = "是"
	}
	return fmt.Sprintf("权限校验信息(NexusPHP): 当前登录用户=%s，发布者=%s，编辑入口=%s，torrent_id=%s", currentUser, publisher, editEntry, diagnosticTorrentID(trimmedID))
}

func normalizeDiagnosticName(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "未解析到"
	}
	return trimmed
}

func diagnosticTorrentID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "未提供"
	}
	return trimmed
}
