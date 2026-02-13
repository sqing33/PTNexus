package stats

import (
	"github.com/pt-nexus/server-go/internal/config"
	"github.com/pt-nexus/server-go/internal/repository"
)

type Service struct {
	repo *repository.StatsRepository
	cfg  *config.Manager
}

func New(repo *repository.StatsRepository, cfg *config.Manager) *Service {
	return &Service{repo: repo, cfg: cfg}
}
