package service

import (
	"github.com/pt-nexus/server-go/internal/config"
	"github.com/pt-nexus/server-go/internal/repository"
	trackersvc "github.com/pt-nexus/server-go/internal/service/tracker"
)

type TrackerService = trackersvc.Service

// NewTrackerService 创建流量采集服务实例，负责周期性采集并写入 traffic_stats。
// 参数/返回：repo 提供写库和聚合能力，cfg 提供实时配置快照。
// 失败场景：无直接失败，具体错误在服务启动后记录到日志。
// 副作用：无副作用，仅构造服务对象。
func NewTrackerService(repo *repository.StatsRepository, cfg *config.Manager) *TrackerService {
	return trackersvc.New(repo, cfg)
}
