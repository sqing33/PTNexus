package migrationflow

import (
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	publishworkflow "github.com/pt-nexus/server/internal/service/publish/workflow"
)

const dbSeedQueryLogModule = "迁移-种子查询"

func (s *MigrateService) GetDBSeedInfo(torrentID, siteName, taskID string) (map[string]any, int) {
	taskID = strings.TrimSpace(taskID)
	logx.Infof(dbSeedQueryLogModule, "开始查询 torrent_id=%s site_name=%s task_id=%s", torrentID, siteName, taskID)
	if taskID != "" {
		s.ensureLogStream(taskID)
	}

	response, statusCode, lookupResult := processingpersist.ExecuteDBSeedQueryEntry(
		processingpersist.DBSeedQueryEntryInput{
			TorrentID: torrentID,
			SiteName:  siteName,
			TaskID:    taskID,
		},
		processingpersist.DBSeedQueryEntryDeps{
			Repo: s.repo,
			EmitLog: func(step, message, status string) {
				s.emitLog(taskID, step, message, status)
			},
			CloseLog: func() {
				s.closeLogStream(taskID)
			},
			NewContextID: func() string {
				return s.newID("ctx")
			},
			RegisterDBContext: func(contextID string, lookup processingpersist.DBSeedLookupResult, tid string) {
				publishworkflow.RegisterDBContext(
					s.contextState,
					contextID,
					tid,
					lookup.SiteName,
					lookup.Hash,
					lookup.Name,
					lookup.SavePath,
					lookup.DownloaderID,
					lookup.Nickname,
					// 对齐 Python：数据库命中时也允许发布阶段重新下载种子文件。
					tid,
				)
			},
			ReverseMappings: s.reverseMappings(),
		},
	)

	switch statusCode {
	case 400:
		logx.Warnf(dbSeedQueryLogModule, "参数校验失败 torrent_id=%s site_name=%s", torrentID, siteName)
	case 202:
		logx.Infof(dbSeedQueryLogModule, "数据库未命中 torrent_id=%s site_name=%s task_id=%s", torrentID, siteName, taskID)
	case 500:
		logx.Errorf(dbSeedQueryLogModule, "数据库读取失败 torrent_id=%s site_name=%s message=%s", torrentID, siteName, strings.TrimSpace(processingshared.ToString(response["message"], "")))
	case 200:
		if lookupResult != nil {
			logx.Infof(
				dbSeedQueryLogModule,
				"数据库查询完成 torrent_id=%s site_name=%s seed_id=%s hash=%s source=database",
				torrentID,
				siteName,
				lookupResult.SeedID,
				lookupResult.Hash,
			)
		}
	}

	return response, statusCode
}
