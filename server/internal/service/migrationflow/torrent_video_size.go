package migrationflow

import (
	acquirefetch "github.com/pt-nexus/server/internal/service/acquire/fetch"
)

// ExtractVideoSizeFromTorrentFile 解析 torrent 文件并统计视频文件总体积。
// 参数/返回：torrentPath 为 torrent 文件路径；返回视频文件总体积（字节）、最大视频文件名与错误。
// 失败场景：文件读取失败、torrent 结构非法、未包含视频文件时返回错误。
// 副作用：读取磁盘文件。
func (s *MigrateService) ExtractVideoSizeFromTorrentFile(torrentPath string) (int64, string, error) {
	return acquirefetch.ExtractVideoSizeFromTorrentFile(torrentPath)
}
