package service

import (
	"github.com/pt-nexus/server-go/internal/config"
	"github.com/pt-nexus/server-go/internal/repository"
	torrenttransferpkg "github.com/pt-nexus/server-go/internal/service/torrenttransfer"
)

type TorrentTransferService = torrenttransferpkg.TorrentTransferService

func NewTorrentTransferService(repo *repository.MigrateRepository, cfg *config.Manager) *TorrentTransferService {
	return torrenttransferpkg.NewTorrentTransferService(repo, cfg)
}
