package workflow

import (
	"path/filepath"

	"github.com/pt-nexus/server-go/internal/config"
)

// BuildContextFromDBRow 根据数据库命中记录构造迁移上下文。
func BuildContextFromDBRow(taskID, torrentID, siteName, hash, name, savePath, downloaderID, sourceNickname, sourceDetailURL string) Context {
	paths := config.ResolveRuntimePaths()
	return Context{
		TaskID:          taskID,
		TorrentID:       torrentID,
		SiteName:        siteName,
		Hash:            hash,
		Name:            name,
		SavePath:        savePath,
		DownloaderID:    downloaderID,
		SourceNickname:  sourceNickname,
		SourceDetailURL: sourceDetailURL,
		// 对齐 Python：数据库命中时也优先在统一的临时 torrents 目录中查找已下载的 .torrent。
		TorrentDir: filepath.Join(paths.DataDir, "tmp", "torrents"),
	}
}

// BuildContextFromFetch 根据抓取结果构造迁移上下文。
func BuildContextFromFetch(taskID, torrentID, siteName, hash, name, savePath, downloaderID, sourceNickname, sourceDetailURL, originalTorrentPath string) Context {
	return Context{
		TaskID:              taskID,
		TorrentID:           torrentID,
		SiteName:            siteName,
		Hash:                hash,
		Name:                name,
		SavePath:            savePath,
		DownloaderID:        downloaderID,
		SourceNickname:      sourceNickname,
		SourceDetailURL:     sourceDetailURL,
		OriginalTorrentPath: originalTorrentPath,
		TorrentDir:          filepath.Dir(originalTorrentPath),
	}
}
