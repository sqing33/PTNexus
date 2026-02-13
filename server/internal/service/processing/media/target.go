package media

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PickMediaTarget 从保存路径中选择一个可用于提取媒体信息/截图的目标文件。
// 参数/返回：支持传入文件路径或目录路径；返回最合适的视频文件路径。
// 失败场景：路径不存在、目录里没有可识别视频文件时返回错误。
// 副作用：会遍历目录文件系统。
func PickMediaTarget(savePath string) (string, error) {
	trimmed := strings.TrimSpace(savePath)
	if trimmed == "" {
		return "", errors.New("保存路径为空")
	}

	info, err := os.Stat(trimmed)
	if err != nil {
		return "", fmt.Errorf("访问保存路径失败: %w", err)
	}
	if !info.IsDir() {
		return trimmed, nil
	}

	allowedExt := map[string]struct{}{
		".mkv": {}, ".mp4": {}, ".m2ts": {}, ".ts": {}, ".avi": {}, ".iso": {},
	}
	largest := ""
	largestSize := int64(0)
	walkErr := filepath.WalkDir(trimmed, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if _, ok := allowedExt[ext]; !ok {
			return nil
		}
		stat, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if stat.Size() > largestSize {
			largestSize = stat.Size()
			largest = path
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if largest == "" {
		return "", fmt.Errorf("目录中未找到可分析的视频文件: %s", trimmed)
	}
	return largest, nil
}

// ExtractMediaInfo 使用系统工具提取媒体文本信息，优先 mediainfo，回退 ffprobe。
// 参数/返回：输入目标媒体文件路径，返回可写入数据库的媒体文本。
// 失败场景：系统未安装相关工具、命令执行失败或输出为空。
// 副作用：会调用外部命令。
func ExtractMediaInfo(targetFile string) (string, error) {
	if output, err := runCommandCapture("mediainfo", targetFile); err == nil && strings.TrimSpace(output) != "" {
		return output, nil
	}
	if output, err := runCommandCapture("ffprobe", "-hide_banner", "-i", targetFile); err == nil && strings.TrimSpace(output) != "" {
		return output, nil
	}
	return "", fmt.Errorf("无法提取媒体信息，请确认系统已安装 mediainfo 或 ffprobe")
}

func runCommandCapture(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("%s 执行失败: %s", name, text)
	}
	return text, nil
}
