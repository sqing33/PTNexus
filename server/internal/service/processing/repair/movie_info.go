package repair

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
)

const ptgenLogModule = "媒体校验-PTGen"
const movieInfoLogModule = "媒体校验-电影信息"

var (
	reDoubanLink   = regexp.MustCompile(`https?://movie\.douban\.com/subject/\d+/?`)
	reIMDbLink     = regexp.MustCompile(`https?://(?:www\.)?imdb\.com/title/tt\d+/?`)
	reTMDbLink     = regexp.MustCompile(`https?://(?:www\.)?themoviedb\.org/[a-zA-Z]+/\d+/?`)
	reIMDbInFormat = regexp.MustCompile(`https?://(?:www\.)?imdb\.com/title/tt\d+/?`)
	reTMDbInFormat = regexp.MustCompile(`https?://(?:www\.)?themoviedb\.org/[a-zA-Z]+/\d+/?`)
	reDoubanInText = regexp.MustCompile(`https?://movie\.douban\.com/subject/\d+/?`)

	reOgImage      = regexp.MustCompile(`(?is)<meta[^>]+property=["']og:image["'][^>]*content=["']([^"']+)["']`)
	reNbgPoster    = regexp.MustCompile(`(?is)<a[^>]+class=["']nbg["'][^>]*>\s*<img[^>]+src=["']([^"']+)["']`)
	reSummarySpan  = regexp.MustCompile(`(?is)<span[^>]+property=["']v:summary["'][^>]*>(.*?)</span>`)
	reMetaDesc     = regexp.MustCompile(`(?is)<meta[^>]+name=["']description["'][^>]*content=["']([^"']+)["']`)
	reBreakLineTag = regexp.MustCompile(`(?is)<br\s*/?>`)
	reAnyHTMLTag   = regexp.MustCompile(`(?is)<[^>]+>`)
	reMultiSpace   = regexp.MustCompile(`[ \t]+`)
	reMultiNewLine = regexp.MustCompile(`\n{3,}`)
	reDoubanID     = regexp.MustCompile(`/subject/(\d+)`)
	reIMDbID       = regexp.MustCompile(`(tt\d+)`)
)

// FetchMovieInfo 按 mediaType 获取海报或简介，并尽量补全 IMDb/豆瓣/TMDb 链接。
// 参数/返回：mediaType 支持 poster/intro；sourceInfo 为已有外链来源。
// 失败场景：所有来源都无法返回有效数据时返回错误文案。
// 副作用：会访问 PTGen、豆瓣、TMDb 等外部网络接口。
func FetchMovieInfo(mediaType, contentName, subtitle string, sourceInfo map[string]any, csptToken string) (MovieInfoResult, string) {
	result := MovieInfoResult{
		IMDb:   NormalizeExternalLink(toStringAny(sourceInfo["imdb_link"], ""), reIMDbLink),
		Douban: NormalizeExternalLink(toStringAny(sourceInfo["douban_link"], ""), reDoubanLink),
		TMDb:   NormalizeExternalLink(toStringAny(sourceInfo["tmdb_link"], ""), reTMDbLink),
	}

	if result.Douban == "" && result.IMDb != "" {
		if douban, imdb := queryByIMDb(result.IMDb); douban != "" || imdb != "" {
			if result.Douban == "" {
				result.Douban = douban
			}
			if result.IMDb == "" {
				result.IMDb = imdb
			}
		}
	}

	if result.IMDb == "" && result.Douban != "" {
		if douban, imdb := queryByDouban(result.Douban); douban != "" || imdb != "" {
			if result.Douban == "" {
				result.Douban = douban
			}
			if result.IMDb == "" {
				result.IMDb = imdb
			}
		}
	}

	if result.Douban == "" && subtitle != "" {
		if imdb, douban := searchByName(subtitle); imdb != "" || douban != "" {
			if result.IMDb == "" {
				result.IMDb = imdb
			}
			if result.Douban == "" {
				result.Douban = douban
			}
		}
	}

	format, formatIMDb, formatDouban, formatTMDb, errMsg := fetchPTGenFormat(result, csptToken)
	if errMsg == "" && strings.TrimSpace(format) != "" {
		poster, intro, imdb, douban, tmdb := parseFormatContent(format, formatIMDb, formatDouban, formatTMDb)
		result.IMDb = firstNonEmpty(imdb, result.IMDb)
		result.Douban = firstNonEmpty(douban, result.Douban)
		result.TMDb = firstNonEmpty(tmdb, result.TMDb)
		if mediaType == "poster" && strings.TrimSpace(poster) != "" {
			result.Poster = poster
			logFetchMovieInfoResult(mediaType, "ptgen", result, "PTGen格式返回海报")
			return result, ""
		}
		if mediaType == "intro" && strings.TrimSpace(intro) != "" {
			result.Intro = intro
			logFetchMovieInfoResult(mediaType, "ptgen", result, "PTGen格式返回简介")
			return result, ""
		}
		logx.Infof(movieInfoLogModule, "PTGen命中但未返回目标内容 media_type=%s poster_non_empty=%t intro_non_empty=%t", mediaType, strings.TrimSpace(poster) != "", strings.TrimSpace(intro) != "")
	}

	if result.Douban != "" {
		doubanHTML, err := FetchPageWithTimeout(result.Douban)
		if err == nil && doubanHTML != "" {
			if result.IMDb == "" {
				result.IMDb = NormalizeExternalLink(reIMDbInFormat.FindString(doubanHTML), reIMDbLink)
			}
			if result.TMDb == "" {
				result.TMDb = NormalizeExternalLink(reTMDbInFormat.FindString(doubanHTML), reTMDbLink)
			}
			posterURLs := extractPosterURLs(doubanHTML, result.Douban)
			summary := extractDoubanSummary(doubanHTML)
			if mediaType == "poster" && len(posterURLs) > 0 {
				result.Poster = ToBBCodeImages(posterURLs)
				logFetchMovieInfoResult(mediaType, "douban", result, "豆瓣页面提取海报")
				return result, ""
			}
			if mediaType == "intro" {
				result.Intro = BuildIntroText(contentName, subtitle, summary, BuildSourceLinks(result.IMDb, result.Douban, result.TMDb))
				if strings.TrimSpace(result.Intro) != "" {
					logFetchMovieInfoResult(mediaType, "douban", result, "豆瓣页面提取简介")
					return result, ""
				}
			}
		}
	}

	if needsTMDbFallback(mediaType, result) {
		if result.TMDb == "" && result.IMDb != "" {
			result.TMDb = imdbToTMDb(result.IMDb)
		}
		if result.TMDb != "" {
			tmdbPoster, tmdbOverview, tmdbIMDb := fetchTMDbDetails(result.TMDb)
			if result.IMDb == "" && tmdbIMDb != "" {
				result.IMDb = tmdbIMDb
			}
			if mediaType == "poster" && tmdbPoster != "" {
				result.Poster = "[img]" + tmdbPoster + "[/img]"
				logFetchMovieInfoResult(mediaType, "tmdb_fallback", result, "TMDb兜底返回海报")
				return result, ""
			}
			if mediaType == "intro" && strings.TrimSpace(tmdbOverview) != "" {
				result.Intro = BuildIntroText(contentName, subtitle, tmdbOverview, BuildSourceLinks(result.IMDb, result.Douban, result.TMDb))
				logFetchMovieInfoResult(mediaType, "tmdb_fallback", result, "TMDb兜底返回简介")
				return result, ""
			}
		}
	}

	if strings.TrimSpace(errMsg) != "" {
		logx.Warnf(
			movieInfoLogModule,
			"获取媒体信息失败 media_type=%s reason=%s imdb=%s douban=%s tmdb=%s",
			mediaType,
			CompactLogText(errMsg, 180),
			CompactLogText(result.IMDb, 120),
			CompactLogText(result.Douban, 120),
			CompactLogText(result.TMDb, 120),
		)
		return result, errMsg
	}
	if mediaType == "poster" {
		logx.Warnf(
			movieInfoLogModule,
			"获取媒体信息失败 media_type=%s reason=%s imdb=%s douban=%s tmdb=%s",
			mediaType,
			"未能获取有效海报",
			CompactLogText(result.IMDb, 120),
			CompactLogText(result.Douban, 120),
			CompactLogText(result.TMDb, 120),
		)
		return result, "未能获取有效海报，请检查豆瓣/IMDb/TMDb链接或网络连通性。"
	}
	logx.Warnf(
		movieInfoLogModule,
		"获取媒体信息失败 media_type=%s reason=%s imdb=%s douban=%s tmdb=%s",
		mediaType,
		"未能获取有效简介",
		CompactLogText(result.IMDb, 120),
		CompactLogText(result.Douban, 120),
		CompactLogText(result.TMDb, 120),
	)
	return result, "未能获取有效简介，请检查豆瓣/IMDb/TMDb链接或网络连通性。"
}

func logFetchMovieInfoResult(mediaType, source string, result MovieInfoResult, note string) {
	hasPayload := false
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "poster":
		hasPayload = strings.TrimSpace(result.Poster) != ""
	case "intro":
		hasPayload = strings.TrimSpace(result.Intro) != ""
	default:
		hasPayload = strings.TrimSpace(result.Poster) != "" || strings.TrimSpace(result.Intro) != ""
	}

	logx.Infof(
		movieInfoLogModule,
		"获取媒体信息命中 source=%s media_type=%s payload=%t note=%s imdb=%s douban=%s tmdb=%s",
		source,
		mediaType,
		hasPayload,
		note,
		CompactLogText(result.IMDb, 120),
		CompactLogText(result.Douban, 120),
		CompactLogText(result.TMDb, 120),
	)
}

func fetchPTGenFormat(links MovieInfoResult, csptToken string) (string, string, string, string, string) {
	resource := firstNonEmpty(links.Douban, links.TMDb, links.IMDb)
	traceID := fmt.Sprintf("ptgen-%d", time.Now().UnixNano())
	if resource == "" {
		logx.Debugf(
			ptgenLogModule,
			"跳过 PTGen 请求，缺少可用资源链接 流程ID=%s imdb=%s douban=%s tmdb=%s",
			traceID,
			CompactLogText(links.IMDb, 120),
			CompactLogText(links.Douban, 120),
			CompactLogText(links.TMDb, 120),
		)
		return "", "", "", "", ""
	}

	candidates := buildPTGenCandidates(resource, links.Douban, csptToken)
	logx.Infof(
		ptgenLogModule,
		"开始请求 PTGen 流程ID=%s 候选数量=%d 资源链接=%s",
		traceID,
		len(candidates),
		CompactLogText(resource, 160),
	)

	lastErr := ""
	for idx, candidate := range candidates {
		startTime := time.Now()
		logURL := sanitizePTGenCandidateForLog(candidate)
		logx.Infof(ptgenLogModule, "发起 PTGen 请求 流程ID=%s 候选=%d/%d 地址=%s", traceID, idx+1, len(candidates), logURL)

		format, imdb, douban, tmdb, err := fetchPTGenData(candidate)
		costMs := time.Since(startTime).Milliseconds()
		if err != nil {
			lastErr = err.Error()
			logx.Warnf(ptgenLogModule, "PTGen 请求失败 流程ID=%s 候选=%d/%d 耗时=%dms 地址=%s 错误=%v", traceID, idx+1, len(candidates), costMs, logURL, err)
			continue
		}
		format = strings.TrimSpace(format)
		if format == "" {
			logx.Warnf(ptgenLogModule, "PTGen 请求返回空格式 流程ID=%s 候选=%d/%d 耗时=%dms 地址=%s", traceID, idx+1, len(candidates), costMs, logURL)
			continue
		}
		logx.Infof(
			ptgenLogModule,
			"PTGen 请求命中 流程ID=%s 候选=%d/%d 耗时=%dms 地址=%s 格式长度=%d imdb=%s douban=%s tmdb=%s",
			traceID,
			idx+1,
			len(candidates),
			costMs,
			logURL,
			len([]rune(format)),
			CompactLogText(imdb, 120),
			CompactLogText(douban, 120),
			CompactLogText(tmdb, 120),
		)
		return format, imdb, douban, tmdb, ""
	}

	if strings.TrimSpace(lastErr) != "" {
		logx.Warnf(ptgenLogModule, "PTGen 候选全部失败，转入后续回退逻辑 流程ID=%s 最后错误=%s", traceID, CompactLogText(lastErr, 240))
	} else {
		logx.Warnf(ptgenLogModule, "PTGen 候选全部未返回有效格式，转入后续回退逻辑 流程ID=%s", traceID)
	}
	return "", "", "", "", lastErr
}

func buildPTGenCandidates(resourceURL, doubanURL, csptToken string) []string {
	encodedResource := neturl.QueryEscape(resourceURL)
	candidates := []string{}

	if csptToken != "" {
		candidates = append(candidates, fmt.Sprintf("https://cspt.top/api/ptgen/query/%s?url=%s", neturl.PathEscape(csptToken), encodedResource))
	}
	candidates = append(candidates,
		fmt.Sprintf("https://pt-nexus-ptgen.sqing33.dpdns.org/api?url=%s", encodedResource),
		fmt.Sprintf("https://pt-nexus-ptgen.1395251710.workers.dev/api?url=%s", encodedResource),
		fmt.Sprintf("https://ptgen.homeqian.top/?url=%s", encodedResource),
		fmt.Sprintf("https://api.iyuu.cn/App.Movie.Ptgen?url=%s", encodedResource),
	)

	if doubanID := extractDoubanID(doubanURL); doubanID != "" {
		candidates = append(candidates, fmt.Sprintf("https://ptgen.tju.pt/infogen?site=douban&sid=%s", neturl.QueryEscape(doubanID)))
	}

	seen := map[string]struct{}{}
	unique := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
}

func fetchPTGenData(url string) (string, string, string, string, error) {
	methods := preferredPTGenMethods(url)
	errMessages := make([]string, 0, len(methods))
	for _, method := range methods {
		body, err := FetchPageWithMethod(url, method)
		if err != nil {
			errMessages = append(errMessages, fmt.Sprintf("%s请求失败: %v", method, err))
			continue
		}

		format, imdb, douban, tmdb, parseErr := parsePTGenResponse(url, method, body)
		if parseErr != nil {
			errMessages = append(errMessages, fmt.Sprintf("%s响应无效: %v", method, parseErr))
			continue
		}
		return format, imdb, douban, tmdb, nil
	}

	if len(errMessages) > 0 {
		return "", "", "", "", fmt.Errorf(strings.Join(errMessages, " | "))
	}
	return "", "", "", "", fmt.Errorf("no request method available")
}

func preferredPTGenMethods(rawURL string) []string {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return []string{http.MethodGet}
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "pt-nexus-ptgen.sqing33.dpdns.org" || host == "pt-nexus-ptgen.1395251710.workers.dev" {
		return []string{http.MethodPost, http.MethodGet}
	}
	return []string{http.MethodGet}
}

func parsePTGenResponse(url, method, body string) (string, string, string, string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", "", "", "", fmt.Errorf("empty response")
	}

	lowerBody := strings.ToLower(body)
	if strings.Contains(lowerBody, "<!doctype html") && strings.Contains(lowerBody, "pt-gen - generate pt descriptions") {
		logx.Debugf(ptgenLogModule, "PTGen 返回首页 HTML，地址=%s 方法=%s 响应预览=%s", sanitizePTGenCandidateForLog(url), method, CompactLogText(body, 200))
		return "", "", "", "", fmt.Errorf("landing html response")
	}

	parsed := map[string]any{}
	if err := json.Unmarshal([]byte(body), &parsed); err == nil {
		if successRaw, exists := parsed["success"]; exists {
			if success, ok := successRaw.(bool); ok && !success {
				return "", "", "", "", fmt.Errorf("api failed: %s", pickFirstString(parsed, "message", "error"))
			}
		}
		if retRaw, exists := parsed["ret"]; exists {
			if retCode := numericToInt(retRaw); retCode != 0 && retCode != 200 {
				return "", "", "", "", fmt.Errorf("api ret=%d msg=%s", retCode, pickFirstString(parsed, "msg", "message", "error"))
			}
		}

		format := pickFirstString(parsed, "format", "content")
		dataMap, _ := parsed["data"].(map[string]any)
		if format == "" && dataMap != nil {
			format = pickFirstString(dataMap, "format", "content")
		}
		if format == "" {
			if strings.Contains(body, "[img]") || strings.Contains(body, "◎") || strings.Contains(body, "❁") {
				format = body
			}
		}
		if format == "" {
			logx.Debugf(ptgenLogModule, "PTGen 返回无可用格式，地址=%s 方法=%s 响应预览=%s", sanitizePTGenCandidateForLog(url), method, CompactLogText(body, 200))
			return "", "", "", "", fmt.Errorf("no format found")
		}

		imdb := NormalizeExternalLink(firstNonEmpty(pickFirstString(parsed, "imdb_link", "imdb"), pickFirstString(dataMap, "imdb_link", "imdb")), reIMDbLink)
		douban := NormalizeExternalLink(firstNonEmpty(pickFirstString(parsed, "douban_link", "douban"), pickFirstString(dataMap, "douban_link", "douban")), reDoubanLink)
		tmdb := NormalizeExternalLink(firstNonEmpty(pickFirstString(parsed, "tmdb_link", "tmdb"), pickFirstString(dataMap, "tmdb_link", "tmdb")), reTMDbLink)
		return format, imdb, douban, tmdb, nil
	}

	if strings.Contains(body, "[img]") || strings.Contains(body, "◎") || strings.Contains(body, "❁") {
		return body, "", "", "", nil
	}
	logx.Debugf(ptgenLogModule, "PTGen 返回内容无法识别，地址=%s 方法=%s 响应预览=%s", sanitizePTGenCandidateForLog(url), method, CompactLogText(body, 200))
	return "", "", "", "", fmt.Errorf("invalid response content")
}

func sanitizePTGenCandidateForLog(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := neturl.Parse(trimmed)
	if err != nil {
		return CompactLogText(trimmed, 260)
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "cspt.top" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) >= 4 && parts[0] == "api" && parts[1] == "ptgen" && parts[2] == "query" {
			parts[3] = maskSensitiveToken(parts[3])
			parsed.Path = "/" + strings.Join(parts, "/")
		}
	}
	return CompactLogText(parsed.String(), 260)
}

func maskSensitiveToken(token string) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return "***"
	}
	runes := []rune(trimmed)
	if len(runes) <= 6 {
		return "***"
	}
	return string(runes[:3]) + "***" + string(runes[len(runes)-3:])
}

func parseFormatContent(formatData, providedIMDb, providedDouban, providedTMDb string) (string, string, string, string, string) {
	imdb := NormalizeExternalLink(firstNonEmpty(providedIMDb, reIMDbInFormat.FindString(formatData)), reIMDbLink)
	douban := NormalizeExternalLink(firstNonEmpty(providedDouban, reDoubanInText.FindString(formatData)), reDoubanLink)
	tmdb := NormalizeExternalLink(firstNonEmpty(providedTMDb, reTMDbInFormat.FindString(formatData)), reTMDbLink)

	poster := ""
	if matches := reImgBBCode.FindAllStringSubmatch(formatData, -1); len(matches) > 0 {
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			imageURL := strings.TrimSpace(match[1])
			if imageURL != "" {
				poster = "[img]" + imageURL + "[/img]"
				break
			}
		}
	}

	intro := reImgBBCode.ReplaceAllString(formatData, "")
	intro = strings.TrimSpace(intro)
	intro = reMultiNewLine.ReplaceAllString(intro, "\n\n")
	return poster, intro, imdb, douban, tmdb
}

func extractDoubanID(doubanLink string) string {
	match := reDoubanID.FindStringSubmatch(strings.TrimSpace(doubanLink))
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

func extractIMDbID(imdbLink string) string {
	match := reIMDbID.FindStringSubmatch(strings.TrimSpace(imdbLink))
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

func queryByIMDb(imdbLink string) (string, string) {
	imdbID := extractIMDbID(imdbLink)
	if imdbID == "" {
		return "", NormalizeExternalLink(imdbLink, reIMDbLink)
	}
	return queryIDBridge(fmt.Sprintf("imdbid=%s", neturl.QueryEscape(imdbID)))
}

func queryByDouban(doubanLink string) (string, string) {
	doubanID := extractDoubanID(doubanLink)
	if doubanID == "" {
		return NormalizeExternalLink(doubanLink, reDoubanLink), ""
	}
	return queryIDBridge(fmt.Sprintf("doubanid=%s", neturl.QueryEscape(doubanID)))
}

func searchByName(name string) (string, string) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ""
	}
	query := fmt.Sprintf("name=%s", neturl.QueryEscape(trimmed))
	douban, imdb := queryIDBridge(query)
	return imdb, douban
}

func queryIDBridge(query string) (string, string) {
	if strings.TrimSpace(query) == "" {
		return "", ""
	}
	baseURLs := []string{
		"https://pt-nexus-imdb2douban.sqing33.dpdns.org/?" + query,
		"https://pt-nexus-imdb2douban.1395251710.workers.dev/?" + query,
	}
	for _, target := range baseURLs {
		body, err := FetchPageWithTimeout(target)
		if err != nil || strings.TrimSpace(body) == "" {
			continue
		}
		parsed := map[string]any{}
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			continue
		}
		list := []any{}
		if rawData, exists := parsed["data"]; exists {
			if arr, ok := rawData.([]any); ok {
				list = arr
			}
		}
		if len(list) == 0 {
			continue
		}
		first, _ := list[0].(map[string]any)
		if first == nil {
			continue
		}
		imdbID := strings.TrimSpace(toStringAny(first["imdbid"], ""))
		doubanID := strings.TrimSpace(toStringAny(first["doubanid"], ""))
		imdb := ""
		douban := ""
		if imdbID != "" {
			imdb = "https://www.imdb.com/title/" + imdbID + "/"
		}
		if doubanID != "" {
			douban = "https://movie.douban.com/subject/" + doubanID + "/"
		}
		return NormalizeExternalLink(douban, reDoubanLink), NormalizeExternalLink(imdb, reIMDbLink)
	}
	return "", ""
}

func needsTMDbFallback(mediaType string, result MovieInfoResult) bool {
	if mediaType == "poster" {
		return strings.TrimSpace(result.Poster) == ""
	}
	if mediaType == "intro" {
		return strings.TrimSpace(result.Intro) == ""
	}
	return false
}

func imdbToTMDb(imdbLink string) string {
	imdbID := extractIMDbID(imdbLink)
	if imdbID == "" {
		return ""
	}
	apiKey := "0f79586eb9d92afa2b7266f7928b055c"
	apiPath := fmt.Sprintf("/3/find/%s?external_source=imdb_id&api_key=%s", neturl.PathEscape(imdbID), neturl.QueryEscape(apiKey))
	baseURLs := []string{
		"https://api.tmdb.org",
		"https://api.themoviedb.org",
		"http://pt-nexus-proxy.sqing33.dpdns.org/https://api.themoviedb.org",
		"http://pt-nexus-proxy.1395251710.workers.dev/https://api.themoviedb.org",
	}
	for _, base := range baseURLs {
		body, err := FetchPageWithTimeout(base + apiPath)
		if err != nil || strings.TrimSpace(body) == "" {
			continue
		}
		parsed := map[string]any{}
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			continue
		}
		if tmdb := firstTMDbLinkFromFind(parsed); tmdb != "" {
			return tmdb
		}
	}
	return ""
}

func firstTMDbLinkFromFind(parsed map[string]any) string {
	for _, key := range []string{"movie_results", "tv_results"} {
		raw, exists := parsed[key]
		if !exists {
			continue
		}
		arr, ok := raw.([]any)
		if !ok || len(arr) == 0 {
			continue
		}
		first, ok := arr[0].(map[string]any)
		if !ok {
			continue
		}
		id := numericToInt(first["id"])
		if id <= 0 {
			continue
		}
		if key == "movie_results" {
			return fmt.Sprintf("https://www.themoviedb.org/movie/%d", id)
		}
		return fmt.Sprintf("https://www.themoviedb.org/tv/%d", id)
	}
	return ""
}

func fetchTMDbDetails(tmdbLink string) (string, string, string) {
	tmdbLink = strings.TrimSpace(tmdbLink)
	if tmdbLink == "" {
		return "", "", ""
	}
	parsed, err := neturl.Parse(tmdbLink)
	if err != nil {
		return "", "", ""
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 2 {
		return "", "", ""
	}
	mediaType := strings.TrimSpace(segments[0])
	id := strings.TrimSpace(segments[1])
	if mediaType == "" || id == "" {
		return "", "", ""
	}
	apiKey := "0f79586eb9d92afa2b7266f7928b055c"
	baseURLs := []string{
		"https://api.tmdb.org",
		"https://api.themoviedb.org",
		"http://pt-nexus-proxy.sqing33.dpdns.org/https://api.themoviedb.org",
		"http://pt-nexus-proxy.1395251710.workers.dev/https://api.themoviedb.org",
	}
	for _, base := range baseURLs {
		apiURL := fmt.Sprintf("%s/3/%s/%s?api_key=%s&language=zh-CN", base, neturl.PathEscape(mediaType), neturl.PathEscape(id), neturl.QueryEscape(apiKey))
		body, err := FetchPageWithTimeout(apiURL)
		if err != nil || strings.TrimSpace(body) == "" {
			continue
		}
		data := map[string]any{}
		if err := json.Unmarshal([]byte(body), &data); err != nil {
			continue
		}
		posterPath := strings.TrimSpace(pickFirstString(data, "poster_path"))
		overview := strings.TrimSpace(pickFirstString(data, "overview"))
		imdb := NormalizeExternalLink(strings.TrimSpace(pickFirstString(data, "imdb_id")), reIMDbLink)
		if imdb == "" {
			imdb = fetchTMDbExternalIMDb(base, mediaType, id, apiKey)
		}
		poster := ""
		if posterPath != "" {
			poster = "https://image.tmdb.org/t/p/original" + posterPath
		}
		if poster != "" || overview != "" || imdb != "" {
			return poster, overview, imdb
		}
	}
	return "", "", ""
}

func fetchTMDbExternalIMDb(baseURL, mediaType, id, apiKey string) string {
	apiURL := fmt.Sprintf("%s/3/%s/%s/external_ids?api_key=%s", baseURL, neturl.PathEscape(mediaType), neturl.PathEscape(id), neturl.QueryEscape(apiKey))
	body, err := FetchPageWithTimeout(apiURL)
	if err != nil || strings.TrimSpace(body) == "" {
		return ""
	}
	data := map[string]any{}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return ""
	}
	return NormalizeExternalLink(strings.TrimSpace(pickFirstString(data, "imdb_id")), reIMDbLink)
}

func extractPosterURLs(pageHTML string, baseURL string) []string {
	urls := make([]string, 0, 2)
	if strings.TrimSpace(pageHTML) == "" {
		return urls
	}
	for _, re := range []*regexp.Regexp{reOgImage, reNbgPoster} {
		match := re.FindStringSubmatch(pageHTML)
		if len(match) < 2 {
			continue
		}
		url := AbsolutizeURL(baseURL, strings.TrimSpace(match[1]))
		if url != "" {
			urls = appendUniqueStringLocal(urls, url)
		}
	}
	return urls
}

func extractDoubanSummary(pageHTML string) string {
	if strings.TrimSpace(pageHTML) == "" {
		return ""
	}
	if match := reSummarySpan.FindStringSubmatch(pageHTML); len(match) >= 2 {
		summary := sanitizeHTMLText(match[1], true)
		if summary != "" {
			return summary
		}
	}
	if match := reMetaDesc.FindStringSubmatch(pageHTML); len(match) >= 2 {
		return sanitizeHTMLText(match[1], false)
	}
	return ""
}

// ExtractDoubanSummary 从豆瓣详情页 HTML 中提取简介文本。
func ExtractDoubanSummary(pageHTML string) string {
	return extractDoubanSummary(pageHTML)
}

func sanitizeHTMLText(input string, keepLineBreak bool) string {
	text := input
	if keepLineBreak {
		text = reBreakLineTag.ReplaceAllString(text, "\n")
	}
	text = reAnyHTMLTag.ReplaceAllString(text, "")
	text = html.UnescapeString(text)
	lines := strings.Split(text, "\n")
	cleanLines := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(reMultiSpace.ReplaceAllString(line, " "))
		if trimmed == "" {
			continue
		}
		cleanLines = append(cleanLines, trimmed)
	}
	joined := strings.Join(cleanLines, "\n")
	joined = reMultiNewLine.ReplaceAllString(joined, "\n\n")
	return strings.TrimSpace(joined)
}

// BuildSourceLinks 构建简介末尾的来源链接列表。
func BuildSourceLinks(imdb, douban, tmdb string) []string {
	links := make([]string, 0, 3)
	if imdb != "" {
		links = append(links, "IMDb: "+imdb)
	}
	if douban != "" {
		links = append(links, "豆瓣: "+douban)
	}
	if tmdb != "" {
		links = append(links, "TMDb: "+tmdb)
	}
	return links
}

// BuildIntroText 组装统一格式的简介文案。
func BuildIntroText(contentName, subtitle, summary string, sourceLinks []string) string {
	title := firstNonEmpty(strings.TrimSpace(contentName), strings.TrimSpace(subtitle), "未知标题")
	sections := []string{"【简介】", title}

	if summary != "" {
		sections = append(sections, "", summary)
	} else {
		sections = append(sections, "", "暂无可用简介，已保留基础信息。")
	}
	if len(sourceLinks) > 0 {
		sections = append(sections, "", strings.Join(sourceLinks, "\n"))
	}
	return strings.TrimSpace(strings.Join(sections, "\n"))
}

func pickFirstString(source map[string]any, keys ...string) string {
	if source == nil {
		return ""
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		value, exists := source[key]
		if !exists {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case fmt.Stringer:
			text := strings.TrimSpace(typed.String())
			if text != "" {
				return text
			}
		}
	}
	return ""
}

func numericToInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case int32:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
