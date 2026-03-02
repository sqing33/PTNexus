package bdflow

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	processingmedia "github.com/pt-nexus/server/internal/service/processing/media"
)

// ResolveAndExtractForBDInfoInput 定义 BDInfo/MediaInfo 提取阶段的输入参数。
type ResolveAndExtractForBDInfoInput struct {
	TaskID         string
	TorrentName    string
	SavePath       string
	MappedSavePath string
	DownloaderID   string
}

// ResolveAndExtractForBDInfoResult 定义 BDInfo/MediaInfo 提取阶段的输出结果。
type ResolveAndExtractForBDInfoResult struct {
	PathCandidates   []string
	UsedBDInfo       bool
	DetectedBluray   string
	SelectedMedia    string
	CurrentFileLabel string
	MediaInfoText    string
}

// ResolveAndExtractForBDInfo 解析候选路径并提取媒体文本，优先 BDInfo，回退 MediaInfo。
// 参数/返回：输入包含保存路径、映射路径与种子名；返回候选路径、命中类型与媒体文本结果。
// 失败场景：找不到蓝光根目录且无法定位媒体文件，或提取命令执行失败时返回错误。
// 副作用：访问文件系统、调用外部工具并打印 BDInfo 任务日志。
func ResolveAndExtractForBDInfo(input ResolveAndExtractForBDInfoInput) (ResolveAndExtractForBDInfoResult, error) {
	savePath := strings.TrimSpace(input.SavePath)
	mappedSavePath := strings.TrimSpace(input.MappedSavePath)
	torrentName := strings.TrimSpace(input.TorrentName)
	downloaderID := strings.TrimSpace(input.DownloaderID)
	if mappedSavePath == "" {
		mappedSavePath = savePath
	}

	pathCandidates := make([]string, 0, 4)
	seenPaths := map[string]struct{}{}
	for _, candidate := range []string{
		filepath.Join(mappedSavePath, torrentName),
		filepath.Join(savePath, torrentName),
		mappedSavePath,
		savePath,
	} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, exists := seenPaths[candidate]; exists {
			continue
		}
		seenPaths[candidate] = struct{}{}
		pathCandidates = append(pathCandidates, candidate)
	}
	logx.Infof(bdinfoToolLogModule, "路径候选列表 task_id=%s candidates=%v", strings.TrimSpace(input.TaskID), pathCandidates)

	result := ResolveAndExtractForBDInfoResult{
		PathCandidates: pathCandidates,
	}

	blurayRoot := ""
	for _, candidate := range pathCandidates {
		root := FindBlurayRootPath(candidate)
		if root != "" {
			blurayRoot = root
			logx.Warnf(bdinfoToolLogModule, "识别到蓝光根目录 task_id=%s candidate=%s root=%s", strings.TrimSpace(input.TaskID), candidate, root)
			break
		}
		logx.Infof(bdinfoToolLogModule, "候选路径未发现蓝光根目录 task_id=%s candidate=%s", strings.TrimSpace(input.TaskID), candidate)
	}
	if blurayRoot != "" {
		bdinfoText, bdinfoErr := ExtractBDInfo(blurayRoot)
		if bdinfoErr != nil {
			return result, bdinfoErr
		}
		result.UsedBDInfo = true
		result.DetectedBluray = blurayRoot
		result.CurrentFileLabel = filepath.Base(blurayRoot)
		result.MediaInfoText = bdinfoText
		logx.Infof(bdinfoToolLogModule, "BDInfo提取完成 task_id=%s root=%s bytes=%d", strings.TrimSpace(input.TaskID), blurayRoot, len(result.MediaInfoText))
		return result, nil
	}

	targetFile := ""
	pickErrors := make([]string, 0)
	for _, candidate := range pathCandidates {
		logx.Infof(bdinfoToolLogModule, "尝试选择媒体目标 task_id=%s candidate=%s", strings.TrimSpace(input.TaskID), candidate)
		picked, pickErr := processingmedia.PickMediaTarget(candidate)
		if pickErr != nil {
			logx.Warnf(bdinfoToolLogModule, "选择媒体目标失败 task_id=%s path=%s err=%v", strings.TrimSpace(input.TaskID), candidate, pickErr)
			pickErrors = append(pickErrors, fmt.Sprintf("%s => %v", candidate, pickErr))
			continue
		}
		targetFile = picked
		logx.Infof(bdinfoToolLogModule, "已选定媒体目标 task_id=%s target_file=%s", strings.TrimSpace(input.TaskID), targetFile)
		break
	}
	if targetFile == "" {
		errMsg := fmt.Sprintf("无法定位媒体文件: downloader_id=%s, save_path=%s, mapped_save_path=%s", downloaderID, savePath, mappedSavePath)
		if len(pickErrors) > 0 {
			errMsg = errMsg + " | " + strings.Join(pickErrors, " ; ")
		}
		return result, fmt.Errorf("%s", errMsg)
	}

	mediaInfoText, extractErr := processingmedia.ExtractMediaInfo(targetFile)
	if extractErr != nil {
		return result, extractErr
	}
	result.UsedBDInfo = false
	result.SelectedMedia = targetFile
	result.CurrentFileLabel = filepath.Base(targetFile)
	result.MediaInfoText = mediaInfoText
	logx.Warnf(bdinfoToolLogModule, "未识别蓝光目录，回退MediaInfo提取 task_id=%s file=%s bytes=%d", strings.TrimSpace(input.TaskID), targetFile, len(result.MediaInfoText))
	return result, nil
}
