package crossseed

import (
	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
)

type CrossSeedQueryParams struct {
	Page               int
	PageSize           int
	Search             string
	PathFilters        []string
	DownloaderFilters  []string
	IsDeleted          string
	ExcludeTargetSites string
	ReviewStatus       string
}

type CrossSeedService struct {
	repo *repository.CrossSeedRepository
	cfg  *config.Manager
}

func NewCrossSeedService(repo *repository.CrossSeedRepository, cfg *config.Manager) *CrossSeedService {
	return &CrossSeedService{repo: repo, cfg: cfg}
}
