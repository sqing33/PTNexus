package migrationflow

import (
	"errors"
	"os"
	pathpkg "path"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
	processingbdflow "github.com/pt-nexus/server/internal/service/processing/bdflow"
	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
)

const bdinfoTaskLogModule = "BDInfo任务"

// RefreshBDInfo 手动触发 BDInfo 重新获取任务（异步）。
// 参数/返回：seedID 为种子唯一标识；返回启动结果与 HTTP 状态码。
// 失败场景：任务启动失败或依赖异常时返回 500。
// 副作用：会更新数据库任务状态，并启动后台提取任务（本地或盒子代理）。
func (s *MigrateService) RefreshBDInfo(seedID string) (map[string]any, int) {
	taskID, err := s.startBDInfoTask(seedID, true)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, 500
	}
	return map[string]any{"success": true, "message": "BDInfo 重新获取任务已启动", "task_id": taskID}, 200
}

// RestartBDInfo 重启 BDInfo 任务（等价于重新启动一次提取）。
// 参数/返回：seedID 为种子唯一标识；返回启动结果与 HTTP 状态码。
// 失败场景：任务启动失败或依赖异常时返回 500。
// 副作用：会更新数据库任务状态，并启动后台提取任务（本地或盒子代理）。
func (s *MigrateService) RestartBDInfo(seedID string) (map[string]any, int) {
	taskID, err := s.startBDInfoTask(seedID, true)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, 500
	}
	return map[string]any{"success": true, "message": "BDInfo 任务已重启", "task_id": taskID}, 200
}

// startBDInfoTask 启动 BDInfo 任务：优先使用盒子代理异步提取，否则回退本地提取。
// 参数/返回：seedID 为种子唯一标识；force 当前仅用于保持接口一致（对齐历史实现）；返回 task_id。
// 失败场景：seed_id 解析失败、数据库状态更新失败或代理/本地均不可用时返回 error。
// 副作用：会更新数据库任务状态、注册内存任务状态，并启动后台 goroutine（远程轮询或本地执行）。
func (s *MigrateService) startBDInfoTask(seedID string, force bool) (string, error) {
	_ = force
	rootConfig := map[string]any{}
	if s != nil && s.cfg != nil {
		rootConfig = s.cfg.Get()
	}

	trimmedSeedID := strings.TrimSpace(seedID)
	if trimmedSeedID == "" {
		return "", errors.New("缺少 seed_id 参数")
	}

	// 统一：先生成 task_id 并把任务写入数据库（processing_bdinfo）+ 注册内存任务状态。
	taskID := s.newID("bdinfo")
	now := time.Now()
	launch, err := processingbdflow.StartAndRegisterTask(
		processingbdflow.StartAndRegisterInput{SeedID: trimmedSeedID, TaskID: taskID, Now: now},
		processingbdflow.StartAndRegisterDeps{
			Repo:        s.repo,
			State:       s.bdinfoState,
			ParseSeedID: processingpersist.ParseSeedID,
		},
	)
	if err != nil {
		logx.Warnf(bdinfoTaskLogModule, "启动失败：无法初始化任务 seed_id=%s err=%v", trimmedSeedID, err)
		return "", err
	}
	logx.Infof(bdinfoTaskLogModule, "任务已入队 task_id=%s seed_id=%s torrent_id=%s site=%s torrent_name=%s save_path=%s downloader_id=%s", taskID, trimmedSeedID, launch.TorrentID, launch.SiteName, launch.TorrentName, launch.SeedSavePath, launch.SeedDownloaderID)

	downloaderID := strings.TrimSpace(launch.SeedDownloaderID)
	if downloaderID == "" {
		logx.Warnf(bdinfoTaskLogModule, "代理判断跳过：downloader_id 为空 task_id=%s seed_id=%s", taskID, trimmedSeedID)
		go s.runLocalBDInfoTask(taskID, launch, rootConfig)
		return taskID, nil
	}

	downloader, decision, derr := downloaderclient.DecideProxy(rootConfig, downloaderID)
	if !decision.Enabled {
		if derr != nil {
			logx.Warnf(bdinfoTaskLogModule, "代理判断跳过：读取下载器配置失败 task_id=%s seed_id=%s downloader_id=%s reason=%s err=%v", taskID, trimmedSeedID, downloaderID, strings.TrimSpace(decision.Reason), derr)
		}
		go s.runLocalBDInfoTask(taskID, launch, rootConfig)
		return taskID, nil
	}

	// 对齐 Python：当 downloader.use_proxy=true 时，优先把 BDInfo 任务提交给盒子代理（异步），控制端通过轮询获取进度与结果。
	remoteCandidates := buildBDInfoRemoteCandidates(launch.SeedSavePath, launch.TorrentName)
	startErr := error(nil)
	usedRemotePath := ""
	for _, candidate := range remoteCandidates {
		startErr = downloader.StartBDInfoByProxy(candidate, taskID, "")
		if startErr == nil {
			usedRemotePath = candidate
			break
		}
	}
	if usedRemotePath != "" {
		logx.Infof(bdinfoTaskLogModule, "已提交盒子代理BDInfo task_id=%s seed_id=%s remote_path=%s", taskID, trimmedSeedID, usedRemotePath)
		go s.pollBDInfoProgressByProxy(taskID, trimmedSeedID, downloader)
		return taskID, nil
	}

	// 远程启动失败时，仅在本机路径确实可访问时才回退本地，避免在“只挂盒子不挂载”的环境里反复 stat 失败。
	mappedSavePath := strings.TrimSpace(downloaderclient.TranslateDownloaderPath(rootConfig, downloaderID, launch.SeedSavePath))
	localCandidates := buildBDInfoRemoteCandidates(mappedSavePath, launch.TorrentName)
	if hasAnyExistingLocalPath(localCandidates) {
		logx.Warnf(bdinfoTaskLogModule, "盒子代理BDInfo启动失败，回退本地BDInfo task_id=%s seed_id=%s err=%v", taskID, trimmedSeedID, startErr)
		go s.runLocalBDInfoTask(taskID, launch, rootConfig)
		return taskID, nil
	}

	logx.Warnf(bdinfoTaskLogModule, "盒子代理BDInfo启动失败且本地不可访问：任务标记失败 task_id=%s seed_id=%s remote_candidates=%v mapped_save_path=%s err=%v", taskID, trimmedSeedID, remoteCandidates, mappedSavePath, startErr)
	errText := ""
	if startErr != nil {
		errText = startErr.Error()
	}
	_, _ = s.BDInfoCompleteCallback(map[string]any{
		"task_id": strings.TrimSpace(taskID),
		"success": false,
		"bdinfo":  "",
		"error_message": "BDInfo 任务启动失败：盒子代理无法启动且本地路径不可访问" + func() string {
			if strings.TrimSpace(errText) == "" {
				return ""
			}
			return "，错误=" + strings.TrimSpace(errText)
		}(),
	})
	return taskID, nil
}

// buildBDInfoRemoteCandidates 生成 BDInfo 远程/本地路径候选。
// 参数/返回：savePath 为保存路径；torrentName 为种子名称；返回候选路径列表（优先 save_path/torrent_name）。
func buildBDInfoRemoteCandidates(savePath, torrentName string) []string {
	trimmedSavePath := strings.TrimSpace(savePath)
	trimmedTorrentName := strings.TrimSpace(torrentName)
	candidates := make([]string, 0, 2)
	if trimmedSavePath != "" && trimmedTorrentName != "" {
		candidates = append(candidates, joinBDInfoProxyRemotePath(trimmedSavePath, trimmedTorrentName))
	}
	if trimmedSavePath != "" {
		candidates = append(candidates, normalizeBDInfoProxyRemotePath(trimmedSavePath))
	}
	return candidates
}

func joinBDInfoProxyRemotePath(base, name string) string {
	normalizedBase := normalizeBDInfoProxyRemotePath(base)
	normalizedName := strings.Trim(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"), "/")
	if normalizedBase == "" {
		return normalizedName
	}
	if normalizedName == "" {
		return normalizedBase
	}
	return pathpkg.Join(normalizedBase, normalizedName)
}

func normalizeBDInfoProxyRemotePath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return strings.ReplaceAll(trimmed, "\\", "/")
}

// runLocalBDInfoTask 使用本地 BDInfo 流程执行任务（复用已写库/已注册的 task_id）。
// 参数/返回：taskID 为任务标识；launch 为 StartAndRegisterTask 的启动信息；rootConfig 为全局配置；无返回值。
// 失败场景：本机无法访问保存路径或 BDInfo 可执行文件缺失时会写入失败状态。
// 副作用：后台 goroutine 会执行本地 BDInfo 流程并写回数据库与任务状态。
func (s *MigrateService) runLocalBDInfoTask(taskID string, launch processingbdflow.StartAndRegisterResult, rootConfig map[string]any) {
	processingbdflow.RunTask(
		processingbdflow.RunTaskInput{
			TaskID:           taskID,
			Hash:             launch.Hash,
			TorrentID:        launch.TorrentID,
			SiteName:         launch.SiteName,
			TorrentName:      launch.TorrentName,
			SeedSavePath:     launch.SeedSavePath,
			SeedDownloaderID: launch.SeedDownloaderID,
		},
		processingbdflow.RunTaskDeps{
			LogModule:     bdinfoTaskLogModule,
			Repo:          s.repo,
			State:         s.bdinfoState,
			RootConfig:    rootConfig,
			ComposeSeedID: processingpersist.ComposeSeedID,
			RecomputeTags: func(hash, torrentID, siteName, savePath, torrentName, reason string) {
				s.recomputeAndPersistTags(hash, torrentID, siteName, savePath, torrentName, reason)
			},
			Now: time.Now,
		},
	)
}

// hasAnyExistingLocalPath 判断候选路径是否存在于本机文件系统（用于决定是否允许回退本地提取）。
func hasAnyExistingLocalPath(candidates []string) bool {
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		if _, err := os.Stat(trimmed); err == nil {
			return true
		}
	}
	return false
}

// pollBDInfoProgressByProxy 轮询盒子代理的 BDInfo 任务进度与结果，并回写控制端状态与数据库。
// 参数/返回：taskID/seedID 用于日志与回调识别；downloader 为代理下载器配置；无返回值。
// 失败场景：连续轮询失败、任务超时、任务失败时，会调用完成回调写库为 failed。
// 副作用：后台 goroutine 持续请求盒子代理进度接口，并调用 BDInfoProgressCallback/BDInfoCompleteCallback。
func (s *MigrateService) pollBDInfoProgressByProxy(taskID string, seedID string, downloader downloaderclient.Downloader) {
	startedAt := time.Now()
	logx.Infof(bdinfoTaskLogModule, "开始轮询盒子代理BDInfo task_id=%s seed_id=%s", strings.TrimSpace(taskID), strings.TrimSpace(seedID))

	ticker := time.NewTicker(1200 * time.Millisecond)
	defer ticker.Stop()
	timeoutAt := time.Now().Add(30 * time.Minute)

	lastSignature := ""
	lastStatus := ""
	consecutiveErrors := 0

	for range ticker.C {
		if time.Now().After(timeoutAt) {
			logx.Warnf(bdinfoTaskLogModule, "盒子代理BDInfo超时 task_id=%s seed_id=%s elapsed=%s", strings.TrimSpace(taskID), strings.TrimSpace(seedID), time.Since(startedAt).String())
			_, _ = s.BDInfoCompleteCallback(map[string]any{
				"task_id":       strings.TrimSpace(taskID),
				"success":       false,
				"bdinfo":        "",
				"error_message": "远程 BDInfo 任务执行超时",
			})
			return
		}

		status, err := downloader.FetchBDInfoProgressByProxy(taskID)
		if err != nil {
			consecutiveErrors++
			if consecutiveErrors == 1 || consecutiveErrors%10 == 0 {
				logx.Warnf(bdinfoTaskLogModule, "轮询盒子代理BDInfo失败 task_id=%s seed_id=%s errors=%d err=%v", strings.TrimSpace(taskID), strings.TrimSpace(seedID), consecutiveErrors, err)
			}
			if consecutiveErrors >= 60 {
				_, _ = s.BDInfoCompleteCallback(map[string]any{
					"task_id":       strings.TrimSpace(taskID),
					"success":       false,
					"bdinfo":        "",
					"error_message": "远程 BDInfo 轮询失败次数过多: " + err.Error(),
				})
				return
			}
			continue
		}
		consecutiveErrors = 0

		taskState := strings.TrimSpace(status.Status)
		if taskState != lastStatus {
			lastStatus = taskState
			logx.Infof(bdinfoTaskLogModule, "盒子代理BDInfo状态变更 task_id=%s seed_id=%s state=%s", strings.TrimSpace(taskID), strings.TrimSpace(seedID), taskState)
		}

		switch taskState {
		case "completed":
			_, _ = s.BDInfoCompleteCallback(map[string]any{
				"task_id":       strings.TrimSpace(taskID),
				"success":       true,
				"bdinfo":        strings.TrimSpace(status.BDInfoContent),
				"error_message": "",
			})
			return
		case "failed":
			errMsg := strings.TrimSpace(status.ErrorMessage)
			if errMsg == "" {
				errMsg = "远程 BDInfo 提取失败"
			}
			_, _ = s.BDInfoCompleteCallback(map[string]any{
				"task_id":       strings.TrimSpace(taskID),
				"success":       false,
				"bdinfo":        "",
				"error_message": errMsg,
			})
			return
		default:
			progressPayload := map[string]any{
				"task_id":          strings.TrimSpace(taskID),
				"progress_percent": status.ProgressPercent,
				"current_file":     strings.TrimSpace(status.CurrentFile),
				"elapsed_time":     strings.TrimSpace(status.ElapsedTime),
				"remaining_time":   strings.TrimSpace(status.RemainingTime),
				"disc_size":        status.DiscSize,
			}
			signature := fmtProgressSignature(progressPayload)
			if signature != lastSignature {
				lastSignature = signature
				_, _ = s.BDInfoProgressCallback(progressPayload)
			}
		}
	}
}

func fmtProgressSignature(payload map[string]any) string {
	return strings.TrimSpace(processingshared.ToString(payload["current_file"], "")) + "|" +
		strings.TrimSpace(processingshared.ToString(payload["elapsed_time"], "")) + "|" +
		strings.TrimSpace(processingshared.ToString(payload["remaining_time"], "")) + "|" +
		strings.TrimSpace(processingshared.ToString(payload["progress_percent"], "")) + "|" +
		strings.TrimSpace(processingshared.ToString(payload["disc_size"], ""))
}
