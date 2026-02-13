package settings

import (
	"github.com/pt-nexus/server-go/internal/repository"
	"github.com/pt-nexus/server-go/internal/service"
)

type Handler struct {
	settings *service.SettingsService
	torrents *repository.TorrentRepository
	sites    *repository.SiteRepository
}

func New(settings *service.SettingsService, torrents *repository.TorrentRepository, sites *repository.SiteRepository) *Handler {
	return &Handler{settings: settings, torrents: torrents, sites: sites}
}
