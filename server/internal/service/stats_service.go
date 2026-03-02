package service

import (
	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
	statssvc "github.com/pt-nexus/server/internal/service/stats"
)

type StatsService = statssvc.Service

func NewStatsService(repo *repository.StatsRepository, cfg *config.Manager) *StatsService {
	return statssvc.New(repo, cfg)
}
