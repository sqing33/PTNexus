package migrationflow

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/pt-nexus/server-go/internal/config"
	"github.com/pt-nexus/server-go/internal/repository"
	extract "github.com/pt-nexus/server-go/internal/service/acquire/extract"
	acquirefetch "github.com/pt-nexus/server-go/internal/service/acquire/fetch"
	processingbdflow "github.com/pt-nexus/server-go/internal/service/processing/bdflow"
	publishworkflow "github.com/pt-nexus/server-go/internal/service/publish/workflow"
)

type MigrateService struct {
	repo *repository.MigrateRepository
	cfg  *config.Manager

	extractorEngine *extract.Engine

	contextState    *publishworkflow.ContextState
	logStreamState  *acquirefetch.LogStreamState
	batchFetchState *acquirefetch.BatchFetchState
	publishState    *publishworkflow.BatchState
	bdinfoState     *processingbdflow.BDInfoState
}

func NewMigrateService(repo *repository.MigrateRepository, cfg *config.Manager) *MigrateService {
	return &MigrateService{
		repo:            repo,
		cfg:             cfg,
		extractorEngine: extract.NewPageExtractorEngine(),
		contextState:    publishworkflow.NewContextState(),
		logStreamState:  acquirefetch.NewLogStreamState(),
		batchFetchState: acquirefetch.NewBatchFetchState(),
		publishState:    publishworkflow.NewBatchState(),
		bdinfoState:     processingbdflow.NewBDInfoState(),
	}
}

func (s *MigrateService) newID(prefix string) string {
	return fmt.Sprintf("%s-%d-%06d", prefix, time.Now().UnixNano(), rand.Intn(1000000))
}
