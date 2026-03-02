package stats

import (
	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
)

type Service struct {
	repo *repository.StatsRepository
	cfg  *config.Manager
}

func New(repo *repository.StatsRepository, cfg *config.Manager) *Service {
	return &Service{repo: repo, cfg: cfg}
}
