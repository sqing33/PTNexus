package migrate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func bindMapPayload(c *gin.Context) (map[string]any, bool) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return nil, false
	}
	return payload, true
}

func prepareSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
}

func writeSSEEvent(c *gin.Context, payload map[string]any) bool {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	if _, err := c.Writer.WriteString("data: " + string(encoded) + "\n\n"); err != nil {
		return false
	}
	c.Writer.Flush()
	return true
}

func handlerToString(value any, fallback string) string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback
		}
		return typed
	case nil:
		return fallback
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text == "" {
			return fallback
		}
		return text
	}
}

func handlerToFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed := strings.TrimSpace(typed)
		if parsed == "" {
			return 0
		}
		var number float64
		if _, err := fmt.Sscanf(parsed, "%f", &number); err == nil {
			return number
		}
		return 0
	default:
		return 0
	}
}
