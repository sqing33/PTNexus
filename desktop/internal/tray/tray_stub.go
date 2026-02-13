//go:build !windows

package tray

import "context"

type noopService struct{}

func newService() Service {
	return &noopService{}
}

func (s *noopService) Start(context.Context) {}

func (s *noopService) Stop() {}
