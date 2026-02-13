package service

import (
	"github.com/pt-nexus/server-go/internal/config"
	"github.com/pt-nexus/server-go/internal/repository"
	statssvc "github.com/pt-nexus/server-go/internal/service/stats"
)

type StatsService = statssvc.Service

func NewStatsService(repo *repository.StatsRepository, cfg *config.Manager) *StatsService {
	return statssvc.New(repo, cfg)
}
