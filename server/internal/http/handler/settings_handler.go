package handler

import (
	settingshandler "github.com/pt-nexus/server-go/internal/http/handler/settings"
	"github.com/pt-nexus/server-go/internal/repository"
	"github.com/pt-nexus/server-go/internal/service"
)

type SettingsHandler = settingshandler.Handler

func NewSettingsHandler(settings *service.SettingsService, torrents *repository.TorrentRepository, sites *repository.SiteRepository) *SettingsHandler {
	return settingshandler.New(settings, torrents, sites)
}
