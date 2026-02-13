package tray

import "context"

// Service 定义托盘服务能力。
type Service interface {
	Start(ctx context.Context)
	Stop()
}

// New 创建当前平台的托盘服务。
func New() Service {
	return newService()
}
