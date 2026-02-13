package bdflow

import processingshared "github.com/pt-nexus/server-go/internal/service/processing/shared"

// toStringAny 统一将任意值转换为字符串。
// 说明：复用 shared.ToString，避免重复实现。
func toStringAny(value any) string {
	return processingshared.ToString(value, "")
}
