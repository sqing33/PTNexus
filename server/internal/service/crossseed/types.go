package crossseed

import "github.com/pt-nexus/server/internal/repository"

type CrossSeedQueryParams struct {
	Page               int
	PageSize           int
	Search             string
	PathFilters        []string
	IsDeleted          string
	ExcludeTargetSites string
	ReviewStatus       string
	IncludeUniquePaths bool
	VideoOnly          bool
}

type CrossSeedService struct {
	repo *repository.CrossSeedRepository
}

func NewCrossSeedService(repo *repository.CrossSeedRepository) *CrossSeedService {
	return &CrossSeedService{repo: repo}
}
