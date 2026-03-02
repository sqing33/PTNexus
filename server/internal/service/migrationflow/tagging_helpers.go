package migrationflow

import (
	"time"

	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	processingtagging "github.com/pt-nexus/server/internal/service/processing/tagging"
)

// recomputeAndPersistTags 统一执行标签重算并回写。
func (s *MigrateService) recomputeAndPersistTags(hash, torrentID, siteName, savePath, torrentNameForPath, reason string) {
	rootConfig := map[string]any{}
	if s != nil && s.cfg != nil {
		rootConfig = s.cfg.Get()
	}
	processingtagging.RecomputeAndPersistTagsIfNeeded(processingtagging.RecomputeAndPersistInput{
		Repo:               s.repo,
		Hash:               hash,
		TorrentID:          torrentID,
		SiteName:           siteName,
		SavePath:           savePath,
		TorrentNameForPath: torrentNameForPath,
		RootConfig:         rootConfig,
		Reason:             reason,
		Now:                time.Now(),
		ComposeSeedID:      processingpersist.ComposeSeedID,
	})
}
