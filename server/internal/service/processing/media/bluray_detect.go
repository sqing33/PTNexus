package media

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/pt-nexus/server-go/internal/platform/logx"
)

const blurayDetectLogModule = "蓝光判定"

// DetectBlurayDiscByCandidates 按候选路径探测是否为蓝光原盘目录。
// 参数/返回：savePath 为下载目录，torrentName/contentName 为候选子目录名；返回是否命中及最终命中的路径。
// 失败场景：savePath 为空时返回 false 与空路径。
// 副作用：读取文件系统并输出蓝光判定日志。
func DetectBlurayDiscByCandidates(savePath, torrentName, contentName string) (bool, string) {
	trimmedSavePath := strings.TrimSpace(savePath)
	if trimmedSavePath == "" {
		logx.Warnf(blurayDetectLogModule, "参数异常：save_path 为空")
		return false, ""
	}
	candidates := []string{}
	if strings.TrimSpace(torrentName) != "" {
		candidates = append(candidates, filepath.Join(trimmedSavePath, strings.TrimSpace(torrentName)))
	}
	if strings.TrimSpace(contentName) != "" && !strings.EqualFold(strings.TrimSpace(contentName), strings.TrimSpace(torrentName)) {
		candidates = append(candidates, filepath.Join(trimmedSavePath, strings.TrimSpace(contentName)))
	}
	candidates = append(candidates, trimmedSavePath)
	logx.Infof(
		blurayDetectLogModule,
		"候选路径生成：save_path=%s torrent_name=%s content_name=%s candidates=%v",
		trimmedSavePath, strings.TrimSpace(torrentName), strings.TrimSpace(contentName), candidates,
	)

	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			logx.Debugf(blurayDetectLogModule, "跳过重复候选：candidate=%s", candidate)
			continue
		}
		seen[candidate] = struct{}{}
		logx.Infof(blurayDetectLogModule, "检测候选路径：candidate=%s", candidate)
		if detectBlurayDiscAtPath(candidate) {
			logx.Warnf(blurayDetectLogModule, "命中原盘目录：candidate=%s", candidate)
			return true, candidate
		}
	}
	logx.Infof(blurayDetectLogModule, "未命中原盘目录：fallback_path=%s", trimmedSavePath)
	return false, trimmedSavePath
}

func detectBlurayDiscAtPath(rawPath string) bool {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		logx.Debugf(blurayDetectLogModule, "跳过空路径")
		return false
	}
	bdmvPath := filepath.Join(trimmed, "BDMV")
	info, err := os.Stat(bdmvPath)
	if err != nil || !info.IsDir() {
		bdmvPath = filepath.Join(trimmed, "bdmv")
		info, err = os.Stat(bdmvPath)
		if err != nil || !info.IsDir() {
			logx.Debugf(blurayDetectLogModule, "未找到BDMV目录：candidate=%s", trimmed)
			return false
		}
	}
	logx.Infof(blurayDetectLogModule, "发现BDMV目录：candidate=%s bdmv_path=%s", trimmed, bdmvPath)

	candidates := []string{
		"index.bdmv", "INDEX.BDMV",
		"index.bdm", "INDEX.BDM",
		"MovieObject.bdmv", "MOVIEOBJECT.BDMV",
	}
	for _, fileName := range candidates {
		target := filepath.Join(bdmvPath, fileName)
		if _, err := os.Stat(target); err == nil {
			logx.Warnf(blurayDetectLogModule, "命中蓝光标记文件：candidate=%s marker=%s", trimmed, target)
			return true
		}
	}
	logx.Infof(blurayDetectLogModule, "缺少蓝光标记文件：candidate=%s bdmv_path=%s", trimmed, bdmvPath)

	return false
}
