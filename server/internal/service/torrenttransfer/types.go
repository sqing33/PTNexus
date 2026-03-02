package torrenttransfer

import (
	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
)

type TorrentTransferService struct {
	repo *repository.MigrateRepository
	cfg  *config.Manager
}

func NewTorrentTransferService(repo *repository.MigrateRepository, cfg *config.Manager) *TorrentTransferService {
	return &TorrentTransferService{repo: repo, cfg: cfg}
}
