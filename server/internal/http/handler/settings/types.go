package settings

import (
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service"
)

type Handler struct {
	settings    *service.SettingsService
	torrents    *repository.TorrentRepository
	torrentData *repository.TorrentDataRepository
	sites       *repository.SiteRepository
}

func New(settings *service.SettingsService, torrents *repository.TorrentRepository, torrentData *repository.TorrentDataRepository, sites *repository.SiteRepository) *Handler {
	return &Handler{settings: settings, torrents: torrents, torrentData: torrentData, sites: sites}
}
