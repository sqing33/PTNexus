package fetch

import (
	"errors"
	"strings"
	"time"

	"github.com/pt-nexus/server-go/internal/platform/logx"
)

const fetchAcquireLogModule = "迁移-源站抓取"

// FetchAcquireRepo 定义抓取获取流程所需仓储接口。
type FetchAcquireRepo interface {
	SiteInfoReader
	CurrentTorrentLookupRepo
}

// FetchAcquireInput 定义抓取获取流程输入参数。
type FetchAcquireInput struct {
	SourceSite   string
	SearchTerm   string
	TorrentName  string
	SavePath     string
	DownloaderID string
	TaskID       string
}

// FetchAcquireDeps 定义抓取获取流程依赖。
type FetchAcquireDeps struct {
	Repo    FetchAcquireRepo
	EmitLog func(step, message, status string)
}

// FetchAcquireResult 定义抓取获取流程输出。
type FetchAcquireResult struct {
	SourceInfo map[string]any

	Meta      TorrentMeta
	TorrentID string

	TorrentPath string
	DetailURL   string

	SiteIdentifier     string
	Nickname           string
	TorrentName        string
	TorrentNameForPath string
	SavePath           string
	DownloaderID       string

	DetailHTML string
}

// AcquireSeedForFetch 执行抓取“获取阶段”：校验源站、下载种子、解析元数据、抓详情页。
// 参数/返回：input 为抓取请求参数，deps 注入仓储与日志回调；返回获取阶段统一结果。
// 失败场景：站点不可用、下载失败、解析失败等返回错误与对应 HTTP 状态码。
// 副作用：会发起网络请求、读取数据库、写临时 torrent 文件，并打印关键流程日志。
func AcquireSeedForFetch(input FetchAcquireInput, deps FetchAcquireDeps) (FetchAcquireResult, int, error) {
	sourceSite := strings.TrimSpace(input.SourceSite)
	searchTerm := strings.TrimSpace(input.SearchTerm)
	taskID := strings.TrimSpace(input.TaskID)

	emit := func(step, message, status string) {
		if deps.EmitLog != nil {
			deps.EmitLog(step, message, status)
		}
	}

	sourceInfo, sourceStatus, err := ResolveSourceSiteForFetch(deps.Repo, sourceSite)
	if err != nil {
		logx.Warnf(fetchAcquireLogModule, "源站点校验失败 source_site=%s search_term=%s task_id=%s status=%d err=%v", sourceSite, searchTerm, taskID, sourceStatus, err)
		emit("站点校验", err.Error(), "error")
		return FetchAcquireResult{}, sourceStatus, err
	}

	emit("下载种子", "正在下载源站种子文件...", "processing")
	torrentPath, detailURL, torrentBytes, err := DownloadTorrentForSource(sourceInfo, searchTerm)
	if err != nil {
		statusCode := 500
		displayMessage := err.Error()
		if errors.Is(err, ErrSourceCookieExpired) {
			// 源站点 Cookie 失效属于业务错误，不应触发前端的“退出登录”逻辑。
			// 统一按 200 返回，由上层响应体中的 error_code 进行区分。
			statusCode = 200
			displayMessage = "源站点登录状态失效，请更新 Cookie 后重试"
			logx.Warnf(fetchAcquireLogModule, "下载种子失败 source_site=%s search_term=%s task_id=%s status=%d err=%v", sourceSite, searchTerm, taskID, statusCode, err)
		} else {
			logx.Errorf(fetchAcquireLogModule, "下载种子失败 source_site=%s search_term=%s task_id=%s err=%v", sourceSite, searchTerm, taskID, err)
		}
		emit("下载种子", displayMessage, "error")
		return FetchAcquireResult{}, statusCode, err
	}
	logx.Infof(fetchAcquireLogModule, "下载种子成功 source_site=%s search_term=%s task_id=%s detail_url=%s torrent_path=%s bytes=%d", sourceSite, searchTerm, taskID, detailURL, torrentPath, len(torrentBytes))
	emit("下载种子", "种子文件下载完成", "success")

	emit("解析种子", "正在解析种子元数据...", "processing")
	meta, err := ParseTorrentMeta(torrentBytes)
	if err != nil {
		logx.Errorf(fetchAcquireLogModule, "解析种子元数据失败 source_site=%s search_term=%s task_id=%s err=%v", sourceSite, searchTerm, taskID, err)
		emit("解析种子", err.Error(), "error")
		return FetchAcquireResult{}, 500, err
	}
	logx.Infof(fetchAcquireLogModule, "解析种子元数据成功 source_site=%s search_term=%s task_id=%s info_hash=%s name=%s size=%d", sourceSite, searchTerm, taskID, meta.InfoHash, meta.Name, meta.Size)
	emit("解析种子", "种子元数据解析完成", "success")

	enriched := EnrichFetchedContext(deps.Repo, EnrichFetchedContextInput{
		SourceSite:    sourceSite,
		SourceInfo:    sourceInfo,
		SearchTerm:    searchTerm,
		Meta:          meta,
		TorrentName:   strings.TrimSpace(input.TorrentName),
		SavePath:      strings.TrimSpace(input.SavePath),
		DownloaderID:  strings.TrimSpace(input.DownloaderID),
		DetailURL:     detailURL,
		DetailTimeout: 45 * time.Second,
	})
	html := enriched.DetailHTML
	if detailURL != "" {
		if enriched.DetailFetchError == nil {
			logx.Infof(fetchAcquireLogModule, "详情页抓取成功 source_site=%s search_term=%s task_id=%s detail_url=%s html_len=%d", sourceSite, searchTerm, taskID, detailURL, len(html))
		} else {
			logx.Warnf(fetchAcquireLogModule, "详情页抓取失败 source_site=%s search_term=%s task_id=%s detail_url=%s err=%v", sourceSite, searchTerm, taskID, detailURL, enriched.DetailFetchError)
		}
	}

	return FetchAcquireResult{
		SourceInfo: sourceInfo,
		Meta:       meta,
		TorrentID:  searchTerm,

		TorrentPath: torrentPath,
		DetailURL:   detailURL,

		SiteIdentifier:     enriched.SiteIdentifier,
		Nickname:           enriched.Nickname,
		TorrentName:        enriched.TorrentName,
		TorrentNameForPath: enriched.TorrentNameForPath,
		SavePath:           enriched.SavePath,
		DownloaderID:       enriched.DownloaderID,

		DetailHTML: html,
	}, 200, nil
}
