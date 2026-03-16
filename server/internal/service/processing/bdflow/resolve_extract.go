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

	pathCandidates := processingmedia.BuildMediaPathCandidatesForRoots([]string{mappedSavePath, savePath}, torrentName, "")
	logx.Infof(bdinfoToolLogModule, "路径候选列表 task_id=%s candidates=%v", strings.TrimSpace(input.TaskID), pathCandidates)

	result := ResolveAndExtractForBDInfoResult{
		PathCandidates: pathCandidates,
	}

	isBluray, detectedPath := processingmedia.DetectBlurayDiscByRoots([]string{mappedSavePath, savePath}, torrentName, "")
	if isBluray {
		access, accessErr := processingmedia.ResolveMediaAccessForPath(detectedPath, "BDInfo提取")
		if accessErr != nil {
			return result, accessErr
		}
		root := processingmedia.FindBlurayRootPath(access.ResolvedPath)
		if root == "" {
			closeErr := access.Close()
			if closeErr != nil {
				logx.Warnf(bdinfoToolLogModule, "关闭蓝光访问会话失败 task_id=%s detected_path=%s err=%v", strings.TrimSpace(input.TaskID), detectedPath, closeErr)
			}
			return result, fmt.Errorf("已命中蓝光候选但未定位到蓝光根目录: %s", detectedPath)
		}
		logx.Warnf(bdinfoToolLogModule, "识别到蓝光根目录 task_id=%s detected_path=%s source=%s root=%s", strings.TrimSpace(input.TaskID), detectedPath, access.SourcePath, root)
		bdinfoText, bdinfoErr := ExtractBDInfo(root)
		closeErr := access.Close()
		if bdinfoErr != nil {
			if closeErr != nil {
				logx.Warnf(bdinfoToolLogModule, "关闭蓝光访问会话失败 task_id=%s detected_path=%s err=%v", strings.TrimSpace(input.TaskID), detectedPath, closeErr)
			}
			return result, bdinfoErr
		}
		if closeErr != nil {
			return result, closeErr
		}
		result.UsedBDInfo = true
		result.DetectedBluray = root
		if strings.TrimSpace(access.SourcePath) != "" {
			result.CurrentFileLabel = filepath.Base(access.SourcePath)
		} else {
			result.CurrentFileLabel = filepath.Base(root)
		}
		result.MediaInfoText = bdinfoText
		logx.Infof(bdinfoToolLogModule, "BDInfo提取完成 task_id=%s root=%s bytes=%d", strings.TrimSpace(input.TaskID), root, len(result.MediaInfoText))
		return result, nil
	}

	targetResult, targetErr := processingmedia.ResolveMediaTargetByRoots([]string{mappedSavePath, savePath}, torrentName, "", "BDInfo提取")
	if targetErr != nil {
		return result, fmt.Errorf("无法定位媒体文件: downloader_id=%s, save_path=%s, mapped_save_path=%s | %w", downloaderID, savePath, mappedSavePath, targetErr)
	}
	defer func() {
		if closeErr := targetResult.Close(); closeErr != nil {
			logx.Warnf(bdinfoToolLogModule, "关闭媒体访问会话失败 task_id=%s source_path=%s err=%v", strings.TrimSpace(input.TaskID), targetResult.SourcePath, closeErr)
		}
	}()

	mediaInfoText, extractErr := processingmedia.ExtractMediaInfo(targetResult.TargetFile)
	if extractErr != nil {
		return result, extractErr
	}
	result.UsedBDInfo = false
	result.SelectedMedia = targetResult.TargetFile
	result.CurrentFileLabel = filepath.Base(targetResult.TargetFile)
	result.MediaInfoText = mediaInfoText
	logx.Warnf(bdinfoToolLogModule, "未识别蓝光目录，回退MediaInfo提取 task_id=%s file=%s bytes=%d", strings.TrimSpace(input.TaskID), targetResult.TargetFile, len(result.MediaInfoText))
	return result, nil
}
