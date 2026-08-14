package service

import (
	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
	crossseedpkg "github.com/pt-nexus/server/internal/service/crossseed"
)

type CrossSeedQueryParams = crossseedpkg.CrossSeedQueryParams
type CrossSeedService = crossseedpkg.CrossSeedService

func NewCrossSeedService(repo *repository.CrossSeedRepository, cfg *config.Manager) *CrossSeedService {
	return crossseedpkg.NewCrossSeedService(repo, cfg)
}
