package migrationflow

import (
	publishdownloader "github.com/pt-nexus/server/internal/service/publish/downloader"
)

func (s *MigrateService) GetDownloaderInfo(payload map[string]any) (map[string]any, int) {
	return publishdownloader.GetDownloaderInfo(payload, s.repo)
}

func (s *MigrateService) AddToDownloader(payload map[string]any) (map[string]any, int) {
	rootConfig := map[string]any{}
	if s != nil && s.cfg != nil {
		rootConfig = s.cfg.Get()
	}
	return publishdownloader.AddToDownloader(payload, rootConfig, s.repo)
}
