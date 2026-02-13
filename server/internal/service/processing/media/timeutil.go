package media

import (
	"fmt"
	"time"
)

// ElapsedString 返回从开始时间到当前的秒级耗时文本（如 12s）。
// 参数/返回：begin 为开始时间；返回 `Ns` 格式字符串。
// 失败场景：begin 在未来时按 0 秒处理。
// 副作用：无。
func ElapsedString(begin time.Time) string {
	seconds := int(time.Since(begin).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("%ds", seconds)
}
