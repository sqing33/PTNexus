package uploader

import (
	"os"
	"strings"
)

// LooksLikeLocalTorrentPath 判断输入是否为可访问的本地 .torrent 文件路径。
func LooksLikeLocalTorrentPath(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "http://") || strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		return false
	}
	if !strings.HasSuffix(strings.ToLower(trimmed), ".torrent") {
		return false
	}
	info, err := os.Stat(trimmed)
	if err != nil || info.IsDir() {
		return false
	}
	return true
}
