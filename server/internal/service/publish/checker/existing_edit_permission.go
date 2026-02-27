package checker

import "strings"

// CheckExistingTorrentEditPermission 校验“已存在种子自动编辑”是否允许执行。
// 参数/返回：detailHTML 为详情页 HTML，torrentID 为详情页 id；返回是否允许编辑及失败原因。
// 失败场景：详情页为空、缺少编辑入口、无法解析发布者/当前登录用户、或发布者与当前用户不一致等返回 false 与原因。
// 副作用：无。
func CheckExistingTorrentEditPermission(detailHTML, torrentID string) (bool, string) {
	trimmedHTML := strings.TrimSpace(detailHTML)
	if trimmedHTML == "" {
		return false, "详情页内容为空"
	}

	if looksLikeZhuqueDetailPage(trimmedHTML) {
		return checkZhuqueExistingTorrentEditPermission(trimmedHTML)
	}

	return checkNexusPHPExistingTorrentEditPermission(trimmedHTML, torrentID)
}
