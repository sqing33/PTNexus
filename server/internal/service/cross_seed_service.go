package service

import (
	"github.com/pt-nexus/server-go/internal/repository"
	crossseedpkg "github.com/pt-nexus/server-go/internal/service/crossseed"
)

type CrossSeedQueryParams = crossseedpkg.CrossSeedQueryParams
type BatchRecordQueryParams = crossseedpkg.BatchRecordQueryParams
type CrossSeedService = crossseedpkg.CrossSeedService

func NewCrossSeedService(repo *repository.CrossSeedRepository) *CrossSeedService {
	return crossseedpkg.NewCrossSeedService(repo)
}
