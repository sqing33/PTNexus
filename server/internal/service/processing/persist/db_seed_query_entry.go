package persist

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// DBSeedQueryEntryInput 定义数据库种子查询入口输入。
type DBSeedQueryEntryInput struct {
	TorrentID string
	SiteName  string
	TaskID    string
}

// DBSeedQueryEntryDeps 定义数据库种子查询入口依赖。
type DBSeedQueryEntryDeps struct {
	Repo SeedQueryRepo

	EmitLog  func(step, message, status string)
	CloseLog func()

	NewContextID      func() string
	RegisterDBContext func(contextID string, lookup DBSeedLookupResult, torrentID string)
	ReverseMappings   map[string]any
}

// ExecuteDBSeedQueryEntry 执行数据库种子查询入口流程，并返回标准接口响应。
// 参数/返回：input 为查询参数；deps 注入查询依赖与上下文注册回调；返回响应体、状态码、成功时的查询结果。
// 失败场景：参数为空返回 400，数据库未命中返回 202，数据库异常返回 500。
// 副作用：可能写入迁移上下文状态，并通过回调写入任务日志。
func ExecuteDBSeedQueryEntry(input DBSeedQueryEntryInput, deps DBSeedQueryEntryDeps) (map[string]any, int, *DBSeedLookupResult) {
	torrentID := strings.TrimSpace(input.TorrentID)
	siteName := strings.TrimSpace(input.SiteName)
	taskID := strings.TrimSpace(input.TaskID)
	if torrentID == "" || siteName == "" {
		return map[string]any{"success": false, "message": "错误：torrent_id和site_name参数不能为空"}, 400, nil
	}
	if taskID != "" && deps.EmitLog != nil {
		deps.EmitLog("数据库查询", "正在从数据库读取种子信息...", "processing")
	}

	lookupResult, err := LookupSeedForMigration(DBSeedLookupInput{TorrentID: torrentID, SiteName: siteName}, deps.Repo)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if taskID != "" && deps.EmitLog != nil {
				deps.EmitLog("数据库查询", "数据库中未找到缓存", "error")
			}
			return map[string]any{
				"success":      false,
				"message":      "数据库中未找到种子信息，准备转源站抓取",
				"should_fetch": true,
				"task_id":      taskID,
			}, 202, nil
		}
		return map[string]any{"success": false, "message": "数据库读取失败: " + err.Error()}, 500, nil
	}

	contextID := ""
	if deps.NewContextID != nil {
		contextID = strings.TrimSpace(deps.NewContextID())
	}
	if contextID != "" && deps.RegisterDBContext != nil {
		deps.RegisterDBContext(contextID, lookupResult, torrentID)
	}
	if taskID != "" && deps.EmitLog != nil {
		deps.EmitLog("数据库查询", "数据库读取完成", "success")
		deps.EmitLog("完成", "数据加载完成", "success")
	}
	if taskID != "" && deps.CloseLog != nil {
		deps.CloseLog()
	}

	return map[string]any{
		"success":          true,
		"data":             lookupResult.Normalized,
		"source":           "database",
		"task_id":          contextID,
		"reverse_mappings": deps.ReverseMappings,
	}, 200, &lookupResult
}
