package service

import (
	"github.com/pt-nexus/server/internal/config"
	settingspkg "github.com/pt-nexus/server/internal/service/settings"
)

const (
	BatchPublishMaxConcurrency     = settingspkg.BatchPublishMaxConcurrency
	BatchPublishDefaultConcurrency = settingspkg.BatchPublishDefaultConcurrency
)

type SettingsService = settingspkg.SettingsService

// InvalidSettingsError 暴露设置校验错误类型，供 HTTP Handler 返回 400 使用。
type InvalidSettingsError = settingspkg.InvalidSettingsError

func NewSettingsService(cfg *config.Manager) *SettingsService {
	return settingspkg.NewSettingsService(cfg)
}

// NewSettingsServiceWithDataDir 创建带数据目录的设置服务（支持本地背景图）。
func NewSettingsServiceWithDataDir(cfg *config.Manager, dataDir string) *SettingsService {
	return settingspkg.NewSettingsServiceWithDataDir(cfg, dataDir)
}

// IsInvalidSettingsError 判断错误是否属于前端提交内容不合法。
func IsInvalidSettingsError(err error) bool {
	return settingspkg.IsInvalidSettingsError(err)
}
