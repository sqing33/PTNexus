package service

import (
	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
	torrentdatapkg "github.com/pt-nexus/server/internal/service/torrentdata"
)

type TorrentsDataParams = torrentdatapkg.TorrentsDataParams
type TorrentDataService = torrentdatapkg.TorrentDataService
type IYUUBatchTask = torrentdatapkg.IYUUBatchTask

func NewTorrentDataService(repo *repository.TorrentDataRepository, cfg *config.Manager) *TorrentDataService {
	return torrentdatapkg.NewTorrentDataService(repo, cfg)
}
