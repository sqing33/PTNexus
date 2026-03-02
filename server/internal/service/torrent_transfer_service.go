package service

import (
	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
	torrenttransferpkg "github.com/pt-nexus/server/internal/service/torrenttransfer"
)

type TorrentTransferService = torrenttransferpkg.TorrentTransferService

func NewTorrentTransferService(repo *repository.MigrateRepository, cfg *config.Manager) *TorrentTransferService {
	return torrenttransferpkg.NewTorrentTransferService(repo, cfg)
}
