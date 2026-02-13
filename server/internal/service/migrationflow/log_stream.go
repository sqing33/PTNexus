package migrationflow

import "time"

func (s *MigrateService) ensureLogStream(taskID string) chan map[string]any {
	if s == nil || s.logStreamState == nil {
		return nil
	}
	return s.logStreamState.Ensure(taskID)
}

func (s *MigrateService) GetOrCreateLogStream(taskID string) chan map[string]any {
	return s.ensureLogStream(taskID)
}

func (s *MigrateService) emitLog(taskID, step, message, status string) {
	if s == nil || s.logStreamState == nil {
		return
	}
	s.logStreamState.EmitStep(taskID, step, message, status, time.Now())
}

func (s *MigrateService) closeLogStream(taskID string) {
	if s == nil || s.logStreamState == nil {
		return
	}
	s.logStreamState.Close(taskID)
}
