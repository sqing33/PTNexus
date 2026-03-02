package handler

import (
	settingshandler "github.com/pt-nexus/server/internal/http/handler/settings"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service"
)

type SettingsHandler = settingshandler.Handler

func NewSettingsHandler(settings *service.SettingsService, torrents *repository.TorrentRepository, torrentData *repository.TorrentDataRepository, sites *repository.SiteRepository) *SettingsHandler {
	return settingshandler.New(settings, torrents, torrentData, sites)
}
