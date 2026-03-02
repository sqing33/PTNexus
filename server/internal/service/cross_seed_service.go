package service

import (
	"github.com/pt-nexus/server/internal/repository"
	crossseedpkg "github.com/pt-nexus/server/internal/service/crossseed"
)

type CrossSeedQueryParams = crossseedpkg.CrossSeedQueryParams
type CrossSeedService = crossseedpkg.CrossSeedService

func NewCrossSeedService(repo *repository.CrossSeedRepository) *CrossSeedService {
	return crossseedpkg.NewCrossSeedService(repo)
}
