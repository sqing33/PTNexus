package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service"
)

// LogsHandler 处理日志导出相关接口。
type LogsHandler struct {
	service *service.LogExportService
}

// NewLogsHandler 创建日志导出处理器。
func NewLogsHandler(svc *service.LogExportService) *LogsHandler {
	return &LogsHandler{service: svc}
}

// Export 导出最近24小时后端日志为单个 TXT 文件，便于用户提交问题反馈。
// 参数/返回：通过 gin.Context 读取请求并将过滤后的日志流式写入响应。
// 失败场景：导出服务未初始化、日志计划生成失败、文件流读取失败。
// 副作用：读取本地日志文件并向客户端输出下载内容，同时写入导出审计日志。
func (h *LogsHandler) Export(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "日志导出服务未初始化"})
		return
	}

	plan, err := h.service.PrepareExport()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	ts := time.Now().Format("20060102-150405")
	asciiFileName := fmt.Sprintf("ptnexus-go-log-24h-%s.txt", ts)
	utf8FileName := fmt.Sprintf("ptnexus-go-日志-24h-%s.txt", ts)

	c.Header("Content-Type", "text/plain; charset=utf-8")
	// 同时提供 ASCII filename 和 UTF-8 filename*，避免不同浏览器对中文文件名产生乱码。
	c.Header(
		"Content-Disposition",
		fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", asciiFileName, url.PathEscape(utf8FileName)),
	)
	c.Header("Cache-Control", "no-store")
	c.Header(
		"Trailer",
		"X-Log-Export-Window-Hours, X-Log-Export-Cutoff, X-Log-Lines-Exported, X-Log-Total-Bytes",
	)
	c.Header("X-Log-File-Count", strconv.Itoa(len(plan.Files)))
	c.Status(http.StatusOK)

	logx.Infof(
		"日志导出",
		"开始导出最近24小时日志，请求ID=%s 文件数量=%d 候选总大小=%d 字节",
		c.GetString("request_id"),
		len(plan.Files),
		plan.TotalSize,
	)
	result, err := h.service.StreamExport(c.Writer, plan)
	if err != nil {
		logx.Errorf("日志导出", "导出失败，请求ID=%s 错误=%v", c.GetString("request_id"), err)
		_, _ = c.Writer.WriteString(fmt.Sprintf("\n\n[导出中断] %v\n", err))
		return
	}

	c.Writer.Header().Set("X-Log-Export-Window-Hours", strconv.Itoa(result.WindowHours))
	c.Writer.Header().Set("X-Log-Export-Cutoff", result.Cutoff.Format(time.RFC3339))
	c.Writer.Header().Set("X-Log-Lines-Exported", strconv.FormatInt(result.ExportedLines, 10))
	c.Writer.Header().Set("X-Log-Total-Bytes", strconv.FormatInt(result.ExportedBytes, 10))

	logx.Infof(
		"日志导出",
		"导出完成，请求ID=%s 时间窗=%dh 截止=%s 导出行数=%d 导出字节=%d",
		c.GetString("request_id"),
		result.WindowHours,
		result.Cutoff.Format(time.RFC3339),
		result.ExportedLines,
		result.ExportedBytes,
	)
}
