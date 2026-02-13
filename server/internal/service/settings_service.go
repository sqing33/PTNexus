package service

import (
	"github.com/pt-nexus/server-go/internal/config"
	settingspkg "github.com/pt-nexus/server-go/internal/service/settings"
)

const (
	BatchPublishMaxConcurrency     = settingspkg.BatchPublishMaxConcurrency
	BatchPublishDefaultConcurrency = settingspkg.BatchPublishDefaultConcurrency
)

type SettingsService = settingspkg.SettingsService

func NewSettingsService(cfg *config.Manager) *SettingsService {
	return settingspkg.NewSettingsService(cfg)
}
