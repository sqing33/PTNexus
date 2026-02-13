package fetch

import (
	"os"
	"path/filepath"
	"strings"
)

// FindDownloadedTorrentPath 根据上下文定位可用的本地 torrent 文件。
// 参数/返回：优先使用 originalPath；否则在 torrentDir 中按 siteName-torrentID 前缀匹配最新生成的 torrent。
// 失败场景：路径不存在、目录不可读或未找到匹配文件时返回空字符串。
// 副作用：读取本地文件系统目录。
func FindDownloadedTorrentPath(originalPath string, torrentDir string, siteName string, torrentID string) string {
	if strings.TrimSpace(originalPath) != "" {
		if info, err := os.Stat(originalPath); err == nil && !info.IsDir() {
			return originalPath
		}
	}
	if strings.TrimSpace(torrentDir) == "" {
		return ""
	}
	prefix := sanitizeTorrentFilePart(siteName) + "-" + sanitizeTorrentFilePart(torrentID)
	entries, err := os.ReadDir(torrentDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(strings.ToLower(name), ".torrent") {
			return filepath.Join(torrentDir, name)
		}
	}
	return ""
}
