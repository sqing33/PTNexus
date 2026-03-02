package migrationflow

import "github.com/pt-nexus/server/internal/service/reversemapping"

func (s *MigrateService) reverseMappings() map[string]any {
	if s == nil || s.cfg == nil {
		return reversemapping.Build(nil)
	}
	return reversemapping.Build(s.cfg.Get())
}
