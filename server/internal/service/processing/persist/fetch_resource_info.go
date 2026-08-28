package persist

import (
	"regexp"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/repository"
)

const resourceInfoLogModule = "迁移-资源信息"

var (
	doubanIDPattern = regexp.MustCompile(`movie\.douban\.com/subject/(\d+)`)
	imdbIDPattern   = regexp.MustCompile(`tt\d{7,10}`)
	tmdbIDPattern   = regexp.MustCompile(`themoviedb\.org/(?:movie|tv)/(\d+)`)
	yearPattern     = regexp.MustCompile(`(?:19|20)\d{2}`)
)

// resourceInfoStoreFromRepo 从流水线仓储中提取资源信息仓储能力。
// 参数/返回：repo 为流水线仓储；支持时返回 ResourceInfoStore，不支持时返回 nil。
// 失败场景：仓储未实现资源信息接口时返回 nil。
// 副作用：无。
func resourceInfoStoreFromRepo(repo FetchPersistPipelineRepo) ResourceInfoStore {
	if store, ok := repo.(ResourceInfoStore); ok && store != nil {
		return store
	}
	return nil
}

// ResourceInfoStore 定义资源信息库读写所需的最小仓储接口。
// 失败场景：实现方在数据库异常时返回错误。
type ResourceInfoStore interface {
	FindResourceInfoByDoubanID(doubanID string) (*repository.ResourceInfo, error)
	FindResourceInfoByImdbID(imdbID string) (*repository.ResourceInfo, error)
	FindResourceInfoByTmdbID(tmdbID string) (*repository.ResourceInfo, error)
	UpsertResourceInfo(info *repository.ResourceInfo) error
}

// ExtractDoubanID 从豆瓣链接中提取 subject 数字 ID。
// 参数/返回：link 为豆瓣详情页链接；未匹配返回空字符串。
// 失败场景：无。
// 副作用：无。
func ExtractDoubanID(link string) string {
	match := doubanIDPattern.FindStringSubmatch(link)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

// ExtractImdbID 从 IMDb 链接或文本中提取 tt 开头的 ID。
// 参数/返回：link 为 IMDb 链接；未匹配返回空字符串。
// 失败场景：无。
// 副作用：无。
func ExtractImdbID(link string) string {
	match := imdbIDPattern.FindStringSubmatch(link)
	if len(match) > 0 {
		return match[0]
	}
	return ""
}

// ExtractTmdbID 从 TMDB 链接中提取数字 ID。
// 参数/返回：link 为 themoviedb.org 详情页链接；未匹配返回空字符串。
// 失败场景：无。
// 副作用：无。
func ExtractTmdbID(link string) string {
	match := tmdbIDPattern.FindStringSubmatch(link)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

// ExtractYearFromText 从文本（如种子标题）中提取首个 4 位年份。
// 参数/返回：text 为待解析文本；未匹配返回空字符串。
// 失败场景：无。
// 副作用：无。
func ExtractYearFromText(text string) string {
	match := yearPattern.FindStringSubmatch(text)
	if len(match) > 0 {
		return match[0]
	}
	return ""
}

// ResourceSeedIDs 依次从豆瓣/IMDb/TMDb 链接提取三个外部 ID。
// 参数/返回：三个链接文本；返回对应的 doubanID/imdbID/tmdbID（可能为空）。
// 失败场景：无。
// 副作用：无。
func ResourceSeedIDs(doubanLink, imdbLink, tmdbLink string) (string, string, string) {
	return ExtractDoubanID(doubanLink), ExtractImdbID(imdbLink), ExtractTmdbID(tmdbLink)
}

// FindResourceInfoForDraft 按 豆瓣ID > IMDbID > TMDbID 的固定优先级查库匹配资源信息。
// 参数/返回：store 为资源信息仓储；draft 为种子草稿；命中返回匹配记录，未命中返回 nil。
// 失败场景：查库错误仅记录日志并继续尝试下一优先级，不中断主流程。
// 副作用：仅读取 resource_info 表。
func FindResourceInfoForDraft(store ResourceInfoStore, draft *SeedDraft) *repository.ResourceInfo {
	if store == nil || draft == nil {
		return nil
	}
	doubanID, imdbID, tmdbID := ResourceSeedIDs(draft.DoubanLink, draft.IMDbLink, draft.TMDbLink)
	lookups := []struct {
		id     string
		source string
		lookup func(string) (*repository.ResourceInfo, error)
	}{
		{doubanID, "douban_id", store.FindResourceInfoByDoubanID},
		{imdbID, "imdb_id", store.FindResourceInfoByImdbID},
		{tmdbID, "tmdb_id", store.FindResourceInfoByTmdbID},
	}
	for _, item := range lookups {
		if item.id == "" {
			continue
		}
		row, err := item.lookup(item.id)
		if err != nil {
			logx.Warnf(resourceInfoLogModule, "按 %s 查询资源信息失败 id=%s err=%v", item.source, item.id, err)
			continue
		}
		if row != nil {
			logx.Infof(resourceInfoLogModule, "资源信息库命中 source=%s id=%s title=%s", item.source, item.id, strings.TrimSpace(row.Title))
			return row
		}
	}
	return nil
}

// ApplyResourceInfoToDraft 将命中的资源信息覆盖到草稿的发布数据中（标题/产地/海报/简介）。
// 参数/返回：draft 为种子草稿；info 为命中的资源信息；字段为空时保持草稿原值。
// 失败场景：无（入参为空时直接返回）。
// 副作用：原地修改 draft.Title/Source/Poster/Body。
func ApplyResourceInfoToDraft(draft *SeedDraft, info *repository.ResourceInfo) {
	if draft == nil || info == nil {
		return
	}
	if title := strings.TrimSpace(info.Title); title != "" {
		draft.Title = title
	}
	if country := strings.TrimSpace(info.Country); country != "" {
		draft.Source = country
	}
	if poster := strings.TrimSpace(info.PosterURL); poster != "" {
		draft.Poster = poster
	}
	if summary := strings.TrimSpace(info.Summary); summary != "" {
		draft.Body = summary
	}
}

// ResourceInfoRepairSkips 判断命中资源信息后可跳过的修复任务（海报/简介）。
// 参数/返回：info 为命中资源；返回是否可跳过海报修复与简介修复。
// 失败场景：无。
// 副作用：无。
func ResourceInfoRepairSkips(info *repository.ResourceInfo) (bool, bool) {
	if info == nil {
		return false, false
	}
	skipPoster := strings.TrimSpace(info.PosterURL) != ""
	skipIntro := strings.TrimSpace(info.Summary) != ""
	return skipPoster, skipIntro
}

// SaveResourceInfoFromDraft 在资源信息库未命中时，把当前草稿解析出的资源信息入库以便后续复用。
// 参数/返回：store 为资源信息仓储；draft 为种子草稿；三个 ID 均为空时不入库。
// 失败场景：入库失败仅记录日志，不中断抓取主流程。
// 副作用：可能向 resource_info 表插入新记录或补齐已有记录的空字段。
func SaveResourceInfoFromDraft(store ResourceInfoStore, draft *SeedDraft) {
	if store == nil || draft == nil {
		return
	}
	doubanID, imdbID, tmdbID := ResourceSeedIDs(draft.DoubanLink, draft.IMDbLink, draft.TMDbLink)
	if doubanID == "" && imdbID == "" && tmdbID == "" {
		return
	}
	summary := strings.TrimSpace(draft.Body)
	if summary == "" {
		summary = strings.TrimSpace(draft.Subtitle)
	}
	info := &repository.ResourceInfo{
		Title:     strings.TrimSpace(draft.Title),
		Year:      ExtractYearFromText(draft.Title),
		Country:   strings.TrimSpace(draft.Source),
		DoubanID:  doubanID,
		ImdbID:    imdbID,
		TmdbID:    tmdbID,
		PosterURL: strings.TrimSpace(draft.Poster),
		Summary:   summary,
	}
	if err := store.UpsertResourceInfo(info); err != nil {
		logx.Warnf(resourceInfoLogModule, "保存资源信息失败 douban_id=%s imdb_id=%s tmdb_id=%s err=%v", doubanID, imdbID, tmdbID, err)
		return
	}
	logx.Infof(resourceInfoLogModule, "资源信息已入库 douban_id=%s imdb_id=%s tmdb_id=%s title=%s", doubanID, imdbID, tmdbID, info.Title)
}

// AttachResourceInfoToRow 按归一化种子数据中的外链 ID 匹配资源信息并附加到响应。
// 参数/返回：store 为资源信息仓储；normalized 为 get_db_seed_info 的归一化数据；
// 命中时写入 normalized["resource_info"]，未命中或无 ID 时不写入。
// 失败场景：查库错误仅记录日志，不影响主响应。
// 副作用：仅读取 resource_info 表，可能原地写入 normalized 的 resource_info 键。
func AttachResourceInfoToRow(store ResourceInfoStore, normalized map[string]any) {
	if store == nil || normalized == nil {
		return
	}
	doubanID, imdbID, tmdbID := ResourceSeedIDs(
		toStringSimple(normalized["douban_link"]),
		toStringSimple(normalized["imdb_link"]),
		toStringSimple(normalized["tmdb_link"]),
	)
	if doubanID == "" && imdbID == "" && tmdbID == "" {
		return
	}
	var matched *repository.ResourceInfo
	lookups := []struct {
		id     string
		lookup func(string) (*repository.ResourceInfo, error)
	}{
		{doubanID, store.FindResourceInfoByDoubanID},
		{imdbID, store.FindResourceInfoByImdbID},
		{tmdbID, store.FindResourceInfoByTmdbID},
	}
	for _, item := range lookups {
		if item.id == "" {
			continue
		}
		row, err := item.lookup(item.id)
		if err != nil {
			logx.Warnf(resourceInfoLogModule, "预览查询资源信息失败 id=%s err=%v", item.id, err)
			continue
		}
		if row != nil {
			matched = row
			break
		}
	}
	if matched == nil {
		return
	}
	normalized["resource_info"] = map[string]any{
		"title":     strings.TrimSpace(matched.Title),
		"year":      strings.TrimSpace(matched.Year),
		"country":   strings.TrimSpace(matched.Country),
		"douban_id": strings.TrimSpace(matched.DoubanID),
		"imdb_id":   strings.TrimSpace(matched.ImdbID),
		"tmdb_id":   strings.TrimSpace(matched.TmdbID),
		"poster_url": strings.TrimSpace(matched.PosterURL),
		"summary":   strings.TrimSpace(matched.Summary),
	}
}
