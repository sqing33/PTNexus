package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// RequestIDHeader 是用于跨服务链路追踪的请求头名称。
	RequestIDHeader = "X-Request-ID"
	// ContextKeyRequestID 是 Gin 上下文中保存请求 ID 的键名。
	ContextKeyRequestID = "request_id"
	// ContextKeyUserID 是 Gin 上下文中保存用户标识的键名。
	ContextKeyUserID = "user_id"
)

// RequestContextMiddleware 为每个请求注入请求 ID，便于排查日志。
func RequestContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader(RequestIDHeader))
		if requestID == "" {
			requestID = generateRequestID()
		}
		c.Set(ContextKeyRequestID, requestID)
		c.Writer.Header().Set(RequestIDHeader, requestID)
		c.Next()
	}
}

// GetRequestID 从上下文读取请求 ID，不存在时返回空字符串。
func GetRequestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	value, exists := c.Get(ContextKeyRequestID)
	if !exists {
		return ""
	}
	requestID, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(requestID)
}

// GetUserID 从上下文读取用户 ID，不存在时返回“匿名用户”。
func GetUserID(c *gin.Context) string {
	if c == nil {
		return "匿名用户"
	}
	value, exists := c.Get(ContextKeyUserID)
	if !exists {
		return "匿名用户"
	}
	userID, ok := value.(string)
	if !ok || strings.TrimSpace(userID) == "" {
		return "匿名用户"
	}
	return userID
}

// generateRequestID 生成高可读且可追踪的请求 ID。
func generateRequestID() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("req-%s", hex.EncodeToString(raw))
}
