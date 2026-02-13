package persist

import (
	"errors"
	"strings"
)

// ErrMissingSeedQueryParam 表示种子查询参数缺失。
var ErrMissingSeedQueryParam = errors.New("torrent_id 和 site_name 不能为空")

// DBSeedLookupInput 定义数据库种子查询输入。
type DBSeedLookupInput struct {
	TorrentID string
	SiteName  string
}

// DBSeedLookupResult 定义数据库种子查询输出。
type DBSeedLookupResult struct {
	Normalized map[string]any
	SeedID     string

	SiteName     string
	Hash         string
	Name         string
	SavePath     string
	DownloaderID string
	Nickname     string
}

// LookupSeedForMigration 执行“数据库查询并标准化”流程，并返回构建上下文所需字段。
// 参数/返回：input 为 torrent_id/site_name；repo 为查询依赖；返回标准化行及上下文字段。
// 失败场景：参数为空、记录不存在或数据库异常时返回 error。
// 副作用：仅执行数据库读取。
func LookupSeedForMigration(input DBSeedLookupInput, repo SeedQueryRepo) (DBSeedLookupResult, error) {
	torrentID := strings.TrimSpace(input.TorrentID)
	siteName := strings.TrimSpace(input.SiteName)
	if torrentID == "" || siteName == "" {
		return DBSeedLookupResult{}, ErrMissingSeedQueryParam
	}

	normalized, seedID, err := QueryAndNormalizeSeed(repo, torrentID, siteName)
	if err != nil {
		return DBSeedLookupResult{}, err
	}
	return DBSeedLookupResult{
		Normalized:   normalized,
		SeedID:       seedID,
		SiteName:     strings.TrimSpace(toStringAny(normalized["site_name"], siteName)),
		Hash:         strings.TrimSpace(toStringAny(normalized["hash"], "")),
		Name:         strings.TrimSpace(toStringAny(normalized["name"], "")),
		SavePath:     strings.TrimSpace(toStringAny(normalized["save_path"], "")),
		DownloaderID: strings.TrimSpace(toStringAny(normalized["downloader_id"], "")),
		Nickname:     strings.TrimSpace(toStringAny(normalized["nickname"], siteName)),
	}, nil
}
