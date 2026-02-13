package fetch

import (
	"strings"
	"time"
)

// CurrentTorrentLookupRepo 定义抓取后上下文补全所需的最小仓储接口。
type CurrentTorrentLookupRepo interface {
	GetCurrentTorrentByHash(hash string) (map[string]any, error)
	GetCurrentTorrentByName(name string) (map[string]any, error)
}

// EnrichFetchedContextInput 定义抓取后上下文补全输入。
type EnrichFetchedContextInput struct {
	SourceSite    string
	SourceInfo    map[string]any
	SearchTerm    string
	Meta          TorrentMeta
	TorrentName   string
	SavePath      string
	DownloaderID  string
	DetailURL     string
	DetailTimeout time.Duration
}

// EnrichFetchedContextResult 定义抓取后上下文补全输出。
type EnrichFetchedContextResult struct {
	SiteIdentifier     string
	Nickname           string
	TorrentName        string
	TorrentNameForPath string
	SavePath           string
	DownloaderID       string
	DetailHTML         string
	DetailFetchError   error
}

// EnrichFetchedContext 在种子下载与元数据解析完成后，补全站点标识、路径信息和详情页 HTML。
// 参数/返回：输入包含 sourceInfo、torrent meta 与用户可选覆盖参数；返回补全后的统一上下文。
// 失败场景：详情页抓取失败不会中断流程，错误通过 DetailFetchError 返回。
// 副作用：会进行数据库读取（当前种子回填）与网络请求（详情页抓取）。
func EnrichFetchedContext(repo CurrentTorrentLookupRepo, input EnrichFetchedContextInput) EnrichFetchedContextResult {
	siteIdentifier := strings.TrimSpace(toStringAny(input.SourceInfo["site"], ""))
	if siteIdentifier == "" {
		siteIdentifier = strings.ToLower(strings.TrimSpace(input.SourceSite))
	}
	siteIdentifier = strings.ToLower(siteIdentifier)

	nickname := strings.TrimSpace(toStringAny(input.SourceInfo["nickname"], input.SourceSite))
	if nickname == "" {
		nickname = strings.TrimSpace(input.SourceSite)
	}

	torrentName := strings.TrimSpace(input.TorrentName)
	if torrentName == "" {
		torrentName = strings.TrimSpace(input.Meta.Name)
	}
	torrentNameForPath := strings.TrimSpace(input.Meta.Name)
	if torrentNameForPath == "" {
		torrentNameForPath = torrentName
	}

	savePath := strings.TrimSpace(input.SavePath)
	downloaderID := strings.TrimSpace(input.DownloaderID)
	if repo != nil {
		current := map[string]any{}
		if row, err := repo.GetCurrentTorrentByHash(strings.TrimSpace(input.Meta.InfoHash)); err == nil && len(row) > 0 {
			current = row
		}
		if len(current) == 0 && torrentName != "" {
			if row, err := repo.GetCurrentTorrentByName(torrentName); err == nil && len(row) > 0 {
				current = row
			}
		}
		if savePath == "" {
			savePath = strings.TrimSpace(toStringAny(current["save_path"], ""))
		}
		if downloaderID == "" {
			downloaderID = strings.TrimSpace(toStringAny(current["downloader_id"], ""))
		}
	}

	detailHTML := ""
	var detailFetchError error
	detailURL := strings.TrimSpace(input.DetailURL)
	if detailURL != "" {
		timeout := input.DetailTimeout
		if timeout <= 0 {
			timeout = 45 * time.Second
		}
		page, pageErr := FetchPageWithCookie(
			detailURL,
			strings.TrimSpace(toStringAny(input.SourceInfo["cookie"], "")),
			timeout,
		)
		if pageErr != nil {
			detailFetchError = pageErr
		} else {
			detailHTML = page
		}
	}

	return EnrichFetchedContextResult{
		SiteIdentifier:     siteIdentifier,
		Nickname:           nickname,
		TorrentName:        torrentName,
		TorrentNameForPath: torrentNameForPath,
		SavePath:           savePath,
		DownloaderID:       downloaderID,
		DetailHTML:         detailHTML,
		DetailFetchError:   detailFetchError,
	}
}
