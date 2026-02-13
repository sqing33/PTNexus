package fetch

import "strings"

// ResolvePublishTorrentPathInput 定义发布阶段查找种子文件所需上下文。
type ResolvePublishTorrentPathInput struct {
	OriginalTorrentPath string
	TorrentDir          string
	SiteName            string
	TorrentID           string
	SourceNickname      string
	SourceDetailURL     string
}

// ResolvePublishTorrentPath 为发布阶段定位可用 torrent 文件路径。
// 参数/返回：reader 用于按站点读取配置；input 提供上下文字段；返回本地可用 torrent 路径。
// 失败场景：本地未命中且源站配置缺失或重新下载失败时返回空字符串。
// 副作用：可能读取本地目录并向源站发起下载请求。
func ResolvePublishTorrentPath(reader SiteInfoReader, input ResolvePublishTorrentPathInput) string {
	torrentPath := FindDownloadedTorrentPath(
		input.OriginalTorrentPath,
		input.TorrentDir,
		input.SiteName,
		input.TorrentID,
	)
	if torrentPath != "" {
		return torrentPath
	}

	if reader == nil {
		return ""
	}

	sourceLookup := strings.TrimSpace(input.SourceNickname)
	if sourceLookup == "" {
		sourceLookup = strings.TrimSpace(input.SiteName)
	}
	sourceDetailURL := strings.TrimSpace(input.SourceDetailURL)
	if sourceLookup == "" || sourceDetailURL == "" {
		return ""
	}

	sourceInfo, sourceErr := reader.GetSiteByName(sourceLookup)
	if sourceErr != nil {
		return ""
	}
	downloadedPath, _, _, dlErr := DownloadTorrentForSource(sourceInfo, sourceDetailURL)
	if dlErr != nil {
		return ""
	}
	return downloadedPath
}
