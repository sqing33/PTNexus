package fetch

import "errors"

var (
	// ErrSourceCookieExpired 表示源站点 Cookie 已失效或登录态不可用。
	// 参数/返回：作为哨兵错误供上层 errors.Is 判断与 HTTP 状态码映射。
	// 失败场景：源站点将详情页/下载页重定向到 login.php 或返回登录页 HTML。
	// 副作用：无。
	ErrSourceCookieExpired = errors.New("源站点 Cookie 已失效，请更新 Cookie 后重试")
)
