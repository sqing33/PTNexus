package migrate

import migrationflow "github.com/pt-nexus/server-go/internal/service/migrationflow"

type Handler struct {
	service *migrationflow.MigrateService
}

func New(svc *migrationflow.MigrateService) *Handler {
	return &Handler{service: svc}
}
