package service

import (
	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/repository"
	localquerysvc "github.com/pt-nexus/server/internal/service/localquery"
)

type LocalQueryService = localquerysvc.Service

func NewLocalQueryService(repo *repository.LocalQueryRepository, cfg *config.Manager, dataDir string) *LocalQueryService {
	return localquerysvc.New(repo, cfg, dataDir)
}
