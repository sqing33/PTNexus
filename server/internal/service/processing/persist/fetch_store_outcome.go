package persist

import (
	"errors"
	"strings"

	"github.com/pt-nexus/server-go/internal/platform/logx"
	acquirefetch "github.com/pt-nexus/server-go/internal/service/acquire/fetch"
	processingrepair "github.com/pt-nexus/server-go/internal/service/processing/repair"
)

// FetchAndStoreOutcomeInput 定义抓取入口执行后的收口输入。
type FetchAndStoreOutcomeInput struct {
	SourceSite string
	SearchTerm string
	TaskID     string

	EntryResult    FetchAndStoreEntryResult
	StatusCode     int
	IsAcquireError bool
	RunErr         error

	FetchStoreModule  string
	FetchRepairModule string
}

// FetchAndStoreOutcomeDeps 定义抓取入口执行后的收口依赖。
type FetchAndStoreOutcomeDeps struct {
	EmitLog  func(step, message, status string)
	CloseLog func()

	BuildSuccessResponse func(contextID string) map[string]any
}

// BuildFetchAndStoreOutcome 统一完成 FetchAndStore 的日志收口与 HTTP 响应组装。
// 参数/返回：input 包含入口执行结果与日志模块名；deps 注入日志回调与成功响应构造器；返回接口响应体与状态码。
// 失败场景：获取阶段失败按原状态码返回；处理中段失败返回 500。
// 副作用：写入迁移抓取与修复日志，并按需写入任务步骤日志。
func BuildFetchAndStoreOutcome(input FetchAndStoreOutcomeInput, deps FetchAndStoreOutcomeDeps) (map[string]any, int) {
	processResult := input.EntryResult.ProcessResult
	extractMeta := processResult.ExtractMeta
	if extractMeta.UsedFallback {
		logx.Warnf(
			input.FetchStoreModule,
			"详情页参数提取完成 source_site=%s search_term=%s task_id=%s extractor=%s fallback=%t reason=%s",
			input.SourceSite,
			input.SearchTerm,
			input.TaskID,
			extractMeta.ExtractorName,
			extractMeta.UsedFallback,
			processingrepair.CompactLogText(extractMeta.FallbackReason, 200),
		)
	} else {
		logx.Infof(
			input.FetchStoreModule,
			"详情页参数提取完成 source_site=%s search_term=%s task_id=%s extractor=%s fallback=%t",
			input.SourceSite,
			input.SearchTerm,
			input.TaskID,
			extractMeta.ExtractorName,
			extractMeta.UsedFallback,
		)
	}

	if input.RunErr != nil {
		if deps.CloseLog != nil {
			deps.CloseLog()
		}
		if input.IsAcquireError {
			if errors.Is(input.RunErr, acquirefetch.ErrSourceCookieExpired) {
				return map[string]any{
					"success":    false,
					"message":    "源站点登录状态失效，请更新 Cookie 后重试",
					"error_code": "SOURCE_COOKIE_EXPIRED",
				}, 200
			}
			if input.StatusCode == 500 {
				return map[string]any{"success": false, "message": "抓取失败: " + input.RunErr.Error()}, 500
			}
			return map[string]any{"success": false, "message": "错误：" + input.RunErr.Error()}, input.StatusCode
		}
		logx.Errorf(input.FetchStoreModule, "抓取结果收敛失败 source_site=%s search_term=%s task_id=%s err=%v", input.SourceSite, input.SearchTerm, input.TaskID, input.RunErr)
		if deps.EmitLog != nil {
			deps.EmitLog("抓取修复", "抓取结果收敛失败: "+input.RunErr.Error(), "error")
		}
		return map[string]any{"success": false, "message": "抓取结果收敛失败: " + input.RunErr.Error()}, 500
	}

	repairResult := processResult.PipelineResult.RepairFinalizeResult.RepairResult
	logx.Infof(input.FetchRepairModule, "并发修复结果 source_site=%s search_term=%s task_id=%s %s", input.SourceSite, input.SearchTerm, input.TaskID, repairResult.Summary())
	loggingResult := processResult.LoggingResult
	mediainfoStatus := loggingResult.MediainfoStatus

	torrentName := ""
	if processResult.Draft != nil {
		torrentName = strings.TrimSpace(processResult.Draft.Title)
	}
	logx.Infof(input.FetchStoreModule, "抓取解析结果 source_site=%s search_term=%s task_id=%s mediainfo_status=%s title=%s", input.SourceSite, input.SearchTerm, input.TaskID, mediainfoStatus, torrentName)

	logx.Infof(input.FetchStoreModule, "写入数据库成功 source_site=%s search_term=%s task_id=%s", input.SourceSite, input.SearchTerm, input.TaskID)
	finalMediainfoStatus := processResult.PipelineResult.PostPersistResult.FinalMediainfoStatus
	finalBDInfoTaskID := processResult.PipelineResult.PostPersistResult.FinalBDInfoTaskID
	logx.Infof(input.FetchRepairModule, "抓取修复完成 source_site=%s search_term=%s task_id=%s mediainfo_status=%s bdinfo_task_id=%s", input.SourceSite, input.SearchTerm, input.TaskID, finalMediainfoStatus, finalBDInfoTaskID)
	if deps.EmitLog != nil {
		deps.EmitLog("抓取修复", "抓取修复完成", "success")
		deps.EmitLog("完成", "种子信息抓取完成", "success")
	}
	if deps.CloseLog != nil {
		deps.CloseLog()
	}

	contextID := strings.TrimSpace(input.EntryResult.ContextID)
	logx.Infof(input.FetchStoreModule, "抓取流程完成 source_site=%s search_term=%s task_id=%s context_id=%s save_path=%s downloader_id=%s", input.SourceSite, input.SearchTerm, input.TaskID, contextID, processResult.SavePath, processResult.DownloaderID)

	if deps.BuildSuccessResponse != nil {
		return deps.BuildSuccessResponse(contextID), 200
	}
	return map[string]any{"success": true, "task_id": contextID}, 200
}
