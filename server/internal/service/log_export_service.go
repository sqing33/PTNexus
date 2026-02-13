package service

import logexport "github.com/pt-nexus/server-go/internal/service/logexport"

// LogExportService 对日志导出服务进行别名暴露，兼容统一 service 入口。
type LogExportService = logexport.ExportService

// NewLogExportService 创建日志导出服务实例。
func NewLogExportService() *LogExportService {
	return logexport.New()
}
