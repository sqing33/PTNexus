package middleware

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server-go/internal/platform/logx"
)

// AccessLoggerMiddleware 记录统一中文访问日志，并附带请求链路信息。
func AccessLoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestPath := c.Request.URL.Path
		rawQuery := strings.TrimSpace(c.Request.URL.RawQuery)
		if rawQuery != "" {
			requestPath = fmt.Sprintf("%s?%s", requestPath, rawQuery)
		}

		c.Next()

		requestID := GetRequestID(c)
		userID := GetUserID(c)
		status := c.Writer.Status()
		latency := time.Since(start)
		base := fmt.Sprintf(
			"请求ID=%s 用户=%s 方法=%s 路径=%s 状态码=%d 耗时=%s 客户端=%s",
			requestID,
			userID,
			c.Request.Method,
			requestPath,
			status,
			latency,
			c.ClientIP(),
		)

		if len(c.Errors) > 0 {
			logx.Warnf("访问日志", "请求结束（含错误） %s 错误=%s", base, c.Errors.String())
			return
		}

		if status >= 500 {
			logx.Errorf("访问日志", "请求结束（服务端错误） %s", base)
			return
		}

		if status >= 400 {
			logx.Warnf("访问日志", "请求结束（客户端错误） %s", base)
			return
		}

		logx.Infof("访问日志", "请求结束（成功） %s", base)
	}
}
