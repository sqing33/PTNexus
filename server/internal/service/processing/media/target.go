package media

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// PickMediaTarget 从保存路径中选择一个可用于提取媒体信息/截图的目标文件。
// 参数/返回：支持传入文件路径或目录路径；返回最合适的视频文件路径。
// 失败场景：路径不存在、目录里没有可识别视频文件时返回错误。
// 副作用：会遍历目录文件系统。
func PickMediaTarget(savePath string) (string, error) {
	target, err := pickMediaEntry(savePath, false)
	if err != nil {
		return "", err
	}
	if isISOPath(target) {
		return "", errors.New("ISO 路径必须先完成挂载后再解析媒体文件")
	}
	return target, nil
}

// ExtractMediaInfo 使用系统工具提取媒体文本信息，优先 mediainfo，回退 ffprobe。
// 参数/返回：输入目标媒体文件路径，返回可写入数据库的媒体文本。
// 失败场景：系统未安装相关工具、命令执行失败或输出为空。
// 副作用：会调用外部命令。
func ExtractMediaInfo(targetFile string) (string, error) {
	if output, err := runCommandCaptureWithEnv("mediainfo", "PTNEXUS_MEDIAINFO_PATH", targetFile); err == nil && strings.TrimSpace(output) != "" {
		return output, nil
	}
	if output, err := runCommandCaptureWithEnv("ffprobe", "PTNEXUS_FFPROBE_PATH", "-hide_banner", "-i", targetFile); err == nil && strings.TrimSpace(output) != "" {
		return output, nil
	}
	return "", fmt.Errorf("无法提取媒体信息，请确认系统已安装 mediainfo 或 ffprobe")
}

func runCommandCaptureWithEnv(defaultName string, envKey string, args ...string) (string, error) {
	commandName, err := resolveCommandPath(defaultName, envKey)
	if err != nil {
		return "", err
	}
	return runCommandCapture(commandName, args...)
}

func resolveCommandPath(defaultName string, envKey string) (string, error) {
	if configured := strings.TrimSpace(os.Getenv(envKey)); configured != "" {
		stat, err := os.Stat(configured)
		if err != nil {
			return "", fmt.Errorf("%s 指向的文件不存在: %s", envKey, configured)
		}
		if stat.IsDir() {
			return "", fmt.Errorf("%s 指向的是目录而不是可执行文件: %s", envKey, configured)
		}
		return configured, nil
	}
	path, err := exec.LookPath(defaultName)
	if err != nil {
		return "", fmt.Errorf("未找到可执行文件: %s", defaultName)
	}
	return path, nil
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
