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

// IsInvalidSettingsError 判断错误是否属于前端提交内容不合法。
func IsInvalidSettingsError(err error) bool {
	return settingspkg.IsInvalidSettingsError(err)
}
