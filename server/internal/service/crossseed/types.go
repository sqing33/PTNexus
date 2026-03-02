package crossseed

import "github.com/pt-nexus/server-go/internal/repository"

type CrossSeedQueryParams struct {
	Page               int
	PageSize           int
	Search             string
	PathFilters        []string
	IsDeleted          string
	ExcludeTargetSites string
	ReviewStatus       string
}

type CrossSeedService struct {
	repo *repository.CrossSeedRepository
}

func NewCrossSeedService(repo *repository.CrossSeedRepository) *CrossSeedService {
	return &CrossSeedService{repo: repo}
}
