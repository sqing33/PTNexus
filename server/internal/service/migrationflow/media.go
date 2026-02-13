package migrationflow

import (
	"strings"

	processingpersist "github.com/pt-nexus/server-go/internal/service/processing/persist"
	processingrepair "github.com/pt-nexus/server-go/internal/service/processing/repair"
	processingshared "github.com/pt-nexus/server-go/internal/service/processing/shared"
	processingtitle "github.com/pt-nexus/server-go/internal/service/processing/title"
)

func (s *MigrateService) ParseTitle(title string, mediainfo string, requestID string) (map[string]any, int) {
	return processingtitle.ParseTitleEntry(processingtitle.ParseTitleEntryInput{
		Title:     strings.TrimSpace(title),
		Mediainfo: strings.TrimSpace(mediainfo),
		RequestID: requestID,
		LogModule: "迁移-标题解析",
	})
}

func (s *MigrateService) MediaValidate(payload map[string]any) (map[string]any, int) {
	return processingrepair.MediaValidateEntry(payload, processingrepair.MediaValidateEntryDeps{
		GetRootConfig: func() map[string]any {
			if s == nil || s.cfg == nil {
				return map[string]any{}
			}
			return s.cfg.Get()
		},
		GetCSPTToken: s.csptToken,
		GetSeedMedia: func(seedID string) string {
			hash, torrentID, siteName, parseErr := processingpersist.ParseSeedID(seedID)
			if parseErr != nil {
				return ""
			}
			row, rowErr := s.repo.GetSeedParameterByKey(hash, torrentID, siteName)
			if rowErr != nil {
				return ""
			}
			return strings.TrimSpace(processingshared.ToString(row["mediainfo"], ""))
		},
	})
}

func (s *MigrateService) csptToken() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	root := s.cfg.Get()
	crossSeed, ok := root["cross_seed"].(map[string]any)
	if !ok {
		return ""
	}
	return strings.TrimSpace(processingshared.ToString(crossSeed["cspt_ptgen_token"], ""))
}

func (s *MigrateService) RefreshMediainfoAsync(payload map[string]any) (map[string]any, int) {
	return s.refreshMediainfoAsync(payload)
}
