package settings

import "errors"

// InvalidSettingsError 表示前端提交的设置内容未通过业务校验。
// 说明：用于让 HTTP Handler 区分 400（用户输入错误）与 500（服务内部错误）。
type InvalidSettingsError struct {
	Message string
}

func (e *InvalidSettingsError) Error() string {
	if e == nil {
		return "设置内容不合法"
	}
	if e.Message == "" {
		return "设置内容不合法"
	}
	return e.Message
}

// IsInvalidSettingsError 判断错误是否属于设置校验失败。
// 参数/返回：err 为待判断错误；返回 true 表示应向前端返回 400。
// 失败场景：无。
// 副作用：无。
func IsInvalidSettingsError(err error) bool {
	var target *InvalidSettingsError
	return errors.As(err, &target)
}
