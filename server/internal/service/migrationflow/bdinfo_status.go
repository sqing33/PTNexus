package migrationflow

import (
	"errors"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	processingbdflow "github.com/pt-nexus/server/internal/service/processing/bdflow"
	"gorm.io/gorm"
)

const bdinfoStatusLogModule = "BDInfo状态"

func (s *MigrateService) BDInfoStatus(seedID string) (map[string]any, int) {
	result, err := processingbdflow.QueryBDInfoStatus(s.repo, seedID)
	if err != nil {
		if errors.Is(err, processingbdflow.ErrInvalidSeedID) {
			logx.Warnf(bdinfoStatusLogModule, "查询失败 seed_id=%s 解析失败 err=%v", seedID, err)
			return map[string]any{"error": err.Error()}, 400
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logx.Warnf(bdinfoStatusLogModule, "查询失败 seed_id=%s 数据不存在", seedID)
			return map[string]any{"error": "种子数据不存在"}, 404
		}
		logx.Errorf(bdinfoStatusLogModule, "查询失败 seed_id=%s 数据库异常 err=%v", seedID, err)
		return map[string]any{"error": err.Error()}, 500
	}

	response := result.Response
	taskID := strings.TrimSpace(result.TaskID)
	processingbdflow.EnrichBDInfoStatusResponseWithTaskProgress(response, taskID, s.bdinfoState)
	return response, 200
}

func (s *MigrateService) BDInfoRecords(statusFilter string, page int, pageSize int, videoOnly bool) (map[string]any, int) {
	queryResult, err := processingbdflow.QueryBDInfoRecords(s.repo.DB(), statusFilter, page, pageSize, videoOnly)
	if err != nil {
		return map[string]any{"success": false, "message": err.Error()}, 500
	}
	processingbdflow.EnrichBDInfoRecordsWithTaskProgress(queryResult.Records, s.bdinfoState)

	return map[string]any{
		"success":  true,
		"data":     queryResult.Records,
		"total":    queryResult.Total,
		"page":     queryResult.Page,
		"pageSize": queryResult.PageSize,
	}, 200
}
