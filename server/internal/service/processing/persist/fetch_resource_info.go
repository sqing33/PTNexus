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
	// 从简介正文中按行提取“译名 / 产地”原始值（兼容 ◎ 标记与全角空格）。
	translatedNamePattern   = regexp.MustCompile(`(?im)^[◎❁]?\s*译\s*名\s*[:：]?\s*(.+?)(?:\r?\n|$)`)
	countryFromIntroPattern = regexp.MustCompile(`(?im)^[◎❁]?\s*(?:制\s*片\s*国\s*家/地\s*区|国\s*家|地\s*区|产\s*地)\s*[:：]?\s*(.+?)(?:\r?\n|$)`)
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
	UpdateResourceInfo(info *repository.ResourceInfo) error
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

// extractIntroLineValue 从简介文本中按行提取指定标记后的取值，兼容全角空格（U+3000）。
func extractIntroLineValue(body string, pattern *regexp.Regexp) string {
	if body == "" {
		return ""
	}
	norm := strings.ReplaceAll(body, "\u3000", " ")
	if m := pattern.FindStringSubmatch(norm); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// ExtractTranslatedName 从简介文本中提取“译名”作为资源标题。
// 参数/返回：body 为简介正文；未匹配返回空字符串。
// 失败场景：无。
// 副作用：无。
func ExtractTranslatedName(body string) string {
	return extractIntroLineValue(body, translatedNamePattern)
}

// ExtractCountryFromIntro 从简介文本中提取“产地/制片国家/地区”作为国家名（人类可读，如“日本”）。
// 参数/返回：body 为简介正文；未匹配返回空字符串。
// 失败场景：无。
// 副作用：无。
func ExtractCountryFromIntro(body string) string {
	return extractIntroLineValue(body, countryFromIntroPattern)
}

// StandardizeSourceKeyFromCountryText 将人类可读的产地文本（如“日本”）映射为标准化 source.* 键；无法识别返回空。
// 参数/返回：text 为产地文本；返回 source.japan 等标准化键或空字符串。
// 失败场景：text 为空或无法识别时返回空字符串。
// 副作用：无。
func StandardizeSourceKeyFromCountryText(text string) string {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return ""
	}
	switch {
	case strings.Contains(t, "台湾"), strings.Contains(t, "taiwan"), strings.Contains(t, "twn"):
		return "source.taiwan"
	case strings.Contains(t, "香港"), strings.Contains(t, "hong kong"), strings.Contains(t, "hkg"):
		return "source.hongkong"
	case strings.Contains(t, "中国"), strings.Contains(t, "china"), strings.Contains(t, "chn"):
		return "source.china"
	case strings.Contains(t, "日本"), strings.Contains(t, "japan"), strings.Contains(t, "jpn"):
		return "source.japan"
	case strings.Contains(t, "韩国"), strings.Contains(t, "korea"), strings.Contains(t, "kor"):
		return "source.korea"
	case strings.Contains(t, "英国"), strings.Contains(t, "uk"):
		return "source.uk"
	case strings.Contains(t, "美国"), strings.Contains(t, "usa"), strings.Contains(t, "united states"):
		return "source.western"
	default:
		return ""
	}
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
		// 资源信息库的国家是人类可读文本（如“日本”），复用发布时需映射回标准化 source.* 键。
		if key := StandardizeSourceKeyFromCountryText(country); key != "" {
			draft.Source = key
		} else {
			draft.Source = country
		}
	}
	if poster := strings.TrimSpace(info.PosterURL); poster != "" {
		draft.Poster = poster
	}
	if summary := strings.TrimSpace(info.Summary); summary != "" {
		draft.Body = summary
	}
	// 命中资源信息库时，若库内已存视频截图则以库内截图替换本次抓取截图（按豆瓣>IMDb>TMDb 命中即视为同一资源）。
	if screenshots := strings.TrimSpace(info.Screenshots); screenshots != "" {
		draft.Screenshots = screenshots
	}
}

// SaveResourceInfoFromDraft 在资源信息库未命中时，把当前草稿解析出的资源信息入库以便后续复用。
// 参数/返回：store 为资源信息仓储；draft 为种子草稿；三个 ID 均为空时不入库。
// 失败场景：入库失败仅记录日志，不中断抓取主流程。
// 副作用：仅当资源不存在时向 resource_info 表插入新记录；已存在则不写入（已存在则不修改）。
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
	title := ExtractTranslatedName(summary)
	if title == "" {
		title = strings.TrimSpace(draft.Title)
	}
	country := ExtractCountryFromIntro(summary)
	if country == "" {
		country = strings.TrimSpace(draft.Source)
	}
	year := ExtractYearFromText(summary)
	if year == "" {
		year = ExtractYearFromText(draft.Title)
	}
	info := &repository.ResourceInfo{
		Title:       title,
		Year:        year,
		Country:     country,
		DoubanID:    doubanID,
		ImdbID:      imdbID,
		TmdbID:      tmdbID,
		PosterURL:   strings.TrimSpace(draft.Poster),
		Summary:     summary,
		Screenshots: strings.TrimSpace(draft.Screenshots),
	}
	if err := store.UpsertResourceInfo(info); err != nil {
		logx.Warnf(resourceInfoLogModule, "保存资源信息失败 douban_id=%s imdb_id=%s tmdb_id=%s err=%v", doubanID, imdbID, tmdbID, err)
		return
	}
	logx.Infof(resourceInfoLogModule, "资源信息已入库 douban_id=%s imdb_id=%s tmdb_id=%s title=%s", doubanID, imdbID, tmdbID, info.Title)
}

// SaveResourceInfoFromRow 在资源信息库未命中时，把 get_db_seed_info 归一化数据中的资源信息入库以便后续复用。
// 参数/返回：store 为资源信息仓储；normalized 为归一化种子数据；三个 ID 均为空时不入库。
// 失败场景：入库失败仅记录日志，不影响预览响应。
// 副作用：仅当资源不存在时向 resource_info 表插入新记录；已存在则不写入（已存在则不修改）。
func SaveResourceInfoFromRow(store ResourceInfoStore, normalized map[string]any) {
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
	title := strings.TrimSpace(toStringSimple(normalized["title"]))
	name := strings.TrimSpace(toStringSimple(normalized["name"]))
	if title == "" {
		title = name
	}
	summary := strings.TrimSpace(toStringSimple(normalized["body"]))
	if summary == "" {
		summary = strings.TrimSpace(toStringSimple(normalized["subtitle"]))
	}
	if tn := ExtractTranslatedName(summary); tn != "" {
		title = tn
	}
	country := ExtractCountryFromIntro(summary)
	if country == "" {
		country = strings.TrimSpace(toStringSimple(normalized["source"]))
	}
	year := ExtractYearFromText(summary)
	if year == "" {
		year = ExtractYearFromText(title + " " + name)
	}
	info := &repository.ResourceInfo{
		Title:       title,
		Year:        year,
		Country:     country,
		DoubanID:    doubanID,
		ImdbID:      imdbID,
		TmdbID:      tmdbID,
		PosterURL:   strings.TrimSpace(toStringSimple(normalized["poster"])),
		Summary:     summary,
		Screenshots: strings.TrimSpace(toStringSimple(normalized["screenshots"])),
	}
	if err := store.UpsertResourceInfo(info); err != nil {
		logx.Warnf(resourceInfoLogModule, "预览时保存资源信息失败 douban_id=%s imdb_id=%s tmdb_id=%s err=%v", doubanID, imdbID, tmdbID, err)
		return
	}
	logx.Infof(resourceInfoLogModule, "预览时资源信息已入库 douban_id=%s imdb_id=%s tmdb_id=%s title=%s", doubanID, imdbID, tmdbID, info.Title)
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
		"title":       strings.TrimSpace(matched.Title),
		"year":        strings.TrimSpace(matched.Year),
		"country":     strings.TrimSpace(matched.Country),
		"douban_id":   strings.TrimSpace(matched.DoubanID),
		"imdb_id":     strings.TrimSpace(matched.ImdbID),
		"tmdb_id":     strings.TrimSpace(matched.TmdbID),
		"poster_url":  strings.TrimSpace(matched.PosterURL),
		"summary":     strings.TrimSpace(matched.Summary),
		"screenshots": strings.TrimSpace(matched.Screenshots),
	}
}

// SyncResourceInfoFromParams 将用户在转种面板中编辑后的参数强制同步到资源信息库。
// 与 UpsertResourceInfo（已存在则不修改）不同，本函数是用户主动触发的同步操作：
//   - 按豆瓣ID > IMDbID > TMDbID 优先级查库；
//   - 命中则用 UpdateResourceInfo 覆写整行可编辑字段（标题/年份/国家/海报/简介/截图）；
//   - 未命中则插入新记录。
//
// 参数/返回：store 为资源信息仓储；params 为前端 updated_parameters（含 title/poster/body/screenshots/douban_link 等）。
// 失败场景：三个 ID 均为空或仓储为 nil 时静默跳过；写入失败仅记录日志。
// 副作用：可能更新或插入 resource_info 表记录。
func SyncResourceInfoFromParams(store ResourceInfoStore, params map[string]any) {
	if store == nil || params == nil {
		return
	}
	doubanLink := toStringSimple(params["douban_link"])
	imdbLink := toStringSimple(params["imdb_link"])
	tmdbLink := toStringSimple(params["tmdb_link"])
	doubanID, imdbID, tmdbID := ResourceSeedIDs(doubanLink, imdbLink, tmdbLink)
	if doubanID == "" && imdbID == "" && tmdbID == "" {
		return
	}

	title := strings.TrimSpace(toStringSimple(params["title"]))
	summary := strings.TrimSpace(toStringSimple(params["body"]))
	if summary == "" {
		summary = strings.TrimSpace(toStringSimple(params["statement"]))
	}
	if tn := ExtractTranslatedName(summary); tn != "" {
		title = tn
	}
	country := ExtractCountryFromIntro(summary)
	if country == "" {
		country = strings.TrimSpace(toStringSimple(params["source"]))
	}
	year := ExtractYearFromText(summary)
	if year == "" {
		year = ExtractYearFromText(title)
	}
	info := &repository.ResourceInfo{
		Title:       title,
		Year:        year,
		Country:     country,
		DoubanID:    doubanID,
		ImdbID:      imdbID,
		TmdbID:      tmdbID,
		PosterURL:   strings.TrimSpace(toStringSimple(params["poster"])),
		Summary:     summary,
		Screenshots: strings.TrimSpace(toStringSimple(params["screenshots"])),
	}

	// 按优先级查找已有记录
	matched := findResourceInfoByAnyID(store, doubanID, imdbID, tmdbID)
	if matched != nil {
		info.ID = matched.ID // 复用主键，走 UpdateResourceInfo 强制覆写
		if err := store.UpdateResourceInfo(info); err != nil {
			logx.Warnf(resourceInfoLogModule, "同步资源信息失败(更新) id=%d err=%v", matched.ID, err)
			return
		}
		logx.Infof(resourceInfoLogModule, "资源信息已同步(更新) id=%d title=%s", matched.ID, info.Title)
		return
	}

	// 未命中则插入新记录
	if err := store.UpsertResourceInfo(info); err != nil {
		logx.Warnf(resourceInfoLogModule, "同步资源信息失败(插入) douban_id=%s err=%v", doubanID, err)
		return
	}
	logx.Infof(resourceInfoLogModule, "资源信息已同步(新建) douban_id=%s imdb_id=%s tmdb_id=%s title=%s", doubanID, imdbID, tmdbID, info.Title)
}

// findResourceInfoByAnyID 按优先级尝试三个 ID 查找资源信息记录，返回首个命中的记录。
func findResourceInfoByAnyID(store ResourceInfoStore, doubanID, imdbID, tmdbID string) *repository.ResourceInfo {
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
			continue
		}
		if row != nil {
			return row
		}
	}
	return nil
}
