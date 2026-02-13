package migrationflow

import (
	"strings"

	acquirefetch "github.com/pt-nexus/server-go/internal/service/acquire/fetch"
	processingpersist "github.com/pt-nexus/server-go/internal/service/processing/persist"
	processingshared "github.com/pt-nexus/server-go/internal/service/processing/shared"
	publishworkflow "github.com/pt-nexus/server-go/internal/service/publish/workflow"
)

func (s *MigrateService) DownloadTorrentOnly(payload map[string]any) (map[string]any, int) {
	return acquirefetch.DownloadTorrentOnly(payload, s.repo)
}

func (s *MigrateService) MigrateTorrent(payload map[string]any) (map[string]any, int) {
	return publishworkflow.ExecuteOneStepMigrate(payload, publishworkflow.OneStepMigrateDeps{
		Publish: s.Publish,
	})
}

func (s *MigrateService) UpdatePreviewData(payload map[string]any) (map[string]any, int) {
	return publishworkflow.UpdatePreviewData(payload, publishworkflow.PreviewUpdateDeps{
		ContextState:    s.contextState,
		ReverseMappings: s.reverseMappings(),
		BuildSeedID: func(ctx publishworkflow.Context) string {
			return processingpersist.ComposeSeedID(ctx.Hash, ctx.TorrentID, ctx.SiteName)
		},
	})
}

func (s *MigrateService) GetAggregatedTorrents(payload map[string]any) (map[string]any, int) {
	result, err := acquirefetch.QueryAggregatedTorrents(s.repo.DB(), acquirefetch.AggregatedQueryInput{
		Page:              int(processingshared.ToFloat(payload["page"])),
		PageSize:          int(processingshared.ToFloat(payload["pageSize"])),
		NameSearch:        strings.TrimSpace(processingshared.ToString(payload["nameSearch"], "")),
		PathFilters:       processingshared.ToStringSlice(payload["pathFilters"]),
		StateFilters:      processingshared.ToStringSlice(payload["stateFilters"]),
		DownloaderFilters: processingshared.ToStringSlice(payload["downloaderFilters"]),
	})
	if err != nil {
		return map[string]any{"success": false, "message": "获取聚合种子失败: " + err.Error()}, 500
	}
	return map[string]any{
		"success":             true,
		"data":                result.Items,
		"aggregated_torrents": result.Items,
		"total":               result.Total,
		"page":                result.Page,
		"pageSize":            result.PageSize,
		"totalPages":          result.TotalPages,
	}, 200
}
