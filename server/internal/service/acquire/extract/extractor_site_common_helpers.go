package extract

import (
	"bytes"
	"fmt"
	neturl "net/url"
	"regexp"
	"strings"
	"time"

	acquirefetch "github.com/pt-nexus/server-go/internal/service/acquire/fetch"
	xhtml "golang.org/x/net/html"
)

var (
	reListRowByTorrentID  = regexp.MustCompile(`(?is)<tr[^>]*>.*?details\.php\?[^"'>]*id=(\d+)[^"'>]*.*?</tr>`)
	reListTagSpan         = regexp.MustCompile(`(?is)<span[^>]*class=["'][^"']*(?:optiontag|details-tag|chs_tag|tags)[^"']*["'][^>]*>(.*?)</span>`)
	reListTagAnchor       = regexp.MustCompile(`(?is)<a[^>]*class=["'][^"']*(?:optiontag|details-tag|chs_tag|tags)[^"']*["'][^>]*>(.*?)</a>`)
	reListTagImageAlt     = regexp.MustCompile(`(?is)<img[^>]*alt=["']([^"']+)["'][^>]*>`)
	reListOptionTagSpan   = regexp.MustCompile(`(?is)<span[^>]*class=["'][^"']*optiontag[^"']*["'][^>]*>(.*?)</span>`)
	reListOptionTagAnchor = regexp.MustCompile(`(?is)<a[^>]*class=["'][^"']*optiontag[^"']*["'][^>]*>(.*?)</a>`)
	reTorrentIDInHref     = regexp.MustCompile(`(?i)\bid=(\d+)`)
	reMediaInfoCodeMain   = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*codemain[^"']*["'][^>]*>(.*?)</div>`)
	reHHClubMediaInfo     = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*nexus-media-info-raw[^"']*["'][^>]*>.*?<pre[^>]*>.*?<code[^>]*>(.*?)</code>.*?</pre>.*?</div>`)
	reKeepfrdsMediaInfo   = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*mediainfo[^"']*["'][^>]*>.*?<div[^>]*class=["'][^"']*codemain[^"']*["'][^>]*>.*?<pre[^>]*>(.*?)</pre>.*?</div>.*?</div>`)
	reHTMLBreakTag        = regexp.MustCompile(`(?i)<br\s*/?>`)
)

func extractKeepfrdsTitles(pageHTML string, fallbackTitle string) (string, string) {
	mainTitle := strings.TrimSpace(extractTopTitle(pageHTML))
	subtitle := strings.TrimSpace(extractRowValueByLabels(pageHTML, []string{"副标题"}))
	if subtitle != "" {
		return subtitle, mainTitle
	}
	return strings.TrimSpace(firstNonEmpty(mainTitle, fallbackTitle)), ""
}

func extractMediaInfoByRegexes(pageHTML string, patterns []*regexp.Regexp) string {
	candidates := make([]string, 0, 8)
	for _, pattern := range patterns {
		for _, raw := range extractRegexCandidates(pageHTML, pattern) {
			clean := sanitizeHTMLPreText(raw, true)
			if clean != "" {
				candidates = append(candidates, clean)
			}
		}
	}
	if picked := pickMediaInfoCandidate(candidates); picked != "" {
		return picked
	}
	if picked := pickBDInfoCandidate(candidates); picked != "" {
		return picked
	}
	return ""
}

func extractRegexCandidates(pageHTML string, pattern *regexp.Regexp) []string {
	if pattern == nil {
		return []string{}
	}
	matches := pattern.FindAllStringSubmatch(strings.TrimSpace(pageHTML), -1)
	results := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		results = append(results, match[1])
	}
	return results
}

func extractRegexCandidatesAsText(pageHTML string, pattern *regexp.Regexp) []string {
	raws := extractRegexCandidates(pageHTML, pattern)
	results := make([]string, 0, len(raws))
	for _, raw := range raws {
		text := strings.TrimSpace(normalizeHTMLBlockText(raw))
		if text == "" {
			continue
		}
		results = append(results, text)
	}
	return results
}

func normalizeHTMLBlockText(raw string) string {
	text := reHTMLBreakTag.ReplaceAllString(strings.TrimSpace(raw), "\n")
	return sanitizeHTMLText(text, true)
}

func extractRowValueByLabels(pageHTML string, labels []string) string {
	for _, label := range labels {
		escaped := regexp.QuoteMeta(strings.TrimSpace(label))
		pattern := regexp.MustCompile(`(?is)<td[^>]*>\s*` + escaped + `\s*</td>\s*<td[^>]*>(.*?)</td>`)
		match := pattern.FindStringSubmatch(pageHTML)
		if len(match) >= 2 {
			value := strings.TrimSpace(sanitizeHTMLText(match[1], true))
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func fetchTagsFromTorrentList(baseURL string, cookie string, mainTitle string, torrentID string) ([]string, error) {
	normalizedBase := acquirefetch.NormalizeSiteBaseURL(baseURL)
	if strings.TrimSpace(normalizedBase) == "" {
		return nil, fmt.Errorf("缺少 base_url")
	}
	if strings.TrimSpace(cookie) == "" {
		return nil, fmt.Errorf("缺少 cookie")
	}
	if strings.TrimSpace(mainTitle) == "" {
		return nil, fmt.Errorf("缺少标题")
	}
	if strings.TrimSpace(torrentID) == "" {
		return nil, fmt.Errorf("缺少 torrent_id")
	}

	query := neturl.Values{}
	query.Set("search", strings.TrimSpace(mainTitle))
	query.Set("search_area", "0")
	query.Set("search_mode", "0")
	query.Set("incldead", "1")
	query.Set("spstate", "0")
	url := strings.TrimRight(normalizedBase, "/") + "/torrents.php?" + query.Encode()

	html, err := acquirefetch.FetchPageWithCookie(url, cookie, 45*time.Second)
	if err != nil {
		return nil, err
	}

	rowHTML, found := findTorrentRowHTMLFromListPage(html, torrentID)
	if !found {
		rowHTML = ""
		for _, match := range reListRowByTorrentID.FindAllStringSubmatch(html, -1) {
			if len(match) < 2 {
				continue
			}
			if strings.TrimSpace(match[1]) == strings.TrimSpace(torrentID) {
				rowHTML = match[0]
				break
			}
		}
	}
	if rowHTML == "" {
		return nil, fmt.Errorf("搜索结果中未命中 torrent_id=%s", strings.TrimSpace(torrentID))
	}

	tags := extractTagsFromListRowHTML(rowHTML)
	if len(tags) == 0 {
		return nil, fmt.Errorf("未提取到有效标签")
	}
	return tags, nil
}

func extractTagsFromListRowHTML(rowHTML string) []string {
	if strings.TrimSpace(rowHTML) == "" {
		return nil
	}

	tags := make([]string, 0, 8)
	collectTag := func(raw string) {
		tag := normalizeSourceTagText(raw)
		if tag == "" || shouldIgnoreSourceTag(tag) {
			return
		}
		tags = appendUniqueString(tags, tag)
	}

	optionTags := make([]string, 0, 4)
	for _, match := range reListOptionTagSpan.FindAllStringSubmatch(rowHTML, -1) {
		if len(match) >= 2 {
			tag := normalizeSourceTagText(match[1])
			if tag == "" || shouldIgnoreSourceTag(tag) {
				continue
			}
			optionTags = appendUniqueString(optionTags, tag)
		}
	}
	for _, match := range reListOptionTagAnchor.FindAllStringSubmatch(rowHTML, -1) {
		if len(match) >= 2 {
			tag := normalizeSourceTagText(match[1])
			if tag == "" || shouldIgnoreSourceTag(tag) {
				continue
			}
			optionTags = appendUniqueString(optionTags, tag)
		}
	}
	if len(optionTags) > 0 {
		return optionTags
	}

	for _, match := range reListTagSpan.FindAllStringSubmatch(rowHTML, -1) {
		if len(match) >= 2 {
			collectTag(match[1])
		}
	}
	for _, match := range reListTagAnchor.FindAllStringSubmatch(rowHTML, -1) {
		if len(match) >= 2 {
			collectTag(match[1])
		}
	}
	for _, match := range reListTagImageAlt.FindAllStringSubmatch(rowHTML, -1) {
		if len(match) >= 2 {
			collectTag(match[1])
		}
	}
	return tags
}

func findTorrentRowHTMLFromListPage(pageHTML string, torrentID string) (string, bool) {
	page := strings.TrimSpace(pageHTML)
	id := strings.TrimSpace(torrentID)
	if page == "" || id == "" {
		return "", false
	}

	root, err := xhtml.Parse(strings.NewReader(page))
	if err != nil || root == nil {
		return "", false
	}

	row := findTorrentRowNode(root, id)
	if row == nil {
		return "", false
	}

	var builder bytes.Buffer
	if err := xhtml.Render(&builder, row); err != nil {
		return "", false
	}
	return builder.String(), true
}

func findTorrentRowNode(root *xhtml.Node, torrentID string) *xhtml.Node {
	if root == nil {
		return nil
	}

	table := findFirstNodeByTagAndClassToken(root, "table", "torrents")
	if table == nil {
		return nil
	}

	tbody := findFirstChildByTag(table, "tbody")
	parent := table
	if tbody != nil {
		parent = tbody
	}

	for child := parent.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != xhtml.ElementNode || !strings.EqualFold(child.Data, "tr") {
			continue
		}
		if isHeaderRow(child) {
			continue
		}
		id := extractTorrentIDFromRow(child)
		if id == "" {
			continue
		}
		if strings.TrimSpace(id) == strings.TrimSpace(torrentID) {
			return child
		}
	}
	return nil
}

func findFirstChildByTag(node *xhtml.Node, tag string) *xhtml.Node {
	if node == nil {
		return nil
	}
	trimmed := strings.ToLower(strings.TrimSpace(tag))
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode && strings.ToLower(child.Data) == trimmed {
			return child
		}
	}
	return nil
}

func findFirstNodeByTagAndClassToken(root *xhtml.Node, tag string, classToken string) *xhtml.Node {
	if root == nil {
		return nil
	}
	trimmedTag := strings.ToLower(strings.TrimSpace(tag))
	trimmedToken := strings.TrimSpace(classToken)

	var walk func(*xhtml.Node) *xhtml.Node
	walk = func(node *xhtml.Node) *xhtml.Node {
		if node == nil {
			return nil
		}
		if node.Type == xhtml.ElementNode && strings.ToLower(node.Data) == trimmedTag {
			if trimmedToken == "" || hasClassToken(node, trimmedToken) {
				return node
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if found := walk(child); found != nil {
				return found
			}
		}
		return nil
	}
	return walk(root)
}

func hasClassToken(node *xhtml.Node, token string) bool {
	if node == nil {
		return false
	}
	raw := strings.TrimSpace(getAttr(node, "class"))
	if raw == "" || strings.TrimSpace(token) == "" {
		return false
	}
	for _, item := range strings.Fields(raw) {
		if item == token {
			return true
		}
	}
	return false
}

func isHeaderRow(row *xhtml.Node) bool {
	if row == nil {
		return false
	}
	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != xhtml.ElementNode || !strings.EqualFold(child.Data, "td") {
			continue
		}
		if hasClassToken(child, "colhead") {
			return true
		}
	}
	return false
}

func extractTorrentIDFromRow(row *xhtml.Node) string {
	if row == nil {
		return ""
	}
	var found string
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node == nil || found != "" {
			return
		}
		if node.Type == xhtml.ElementNode && strings.EqualFold(node.Data, "a") {
			href := strings.TrimSpace(getAttr(node, "href"))
			if href != "" && strings.Contains(href, "details.php") && strings.Contains(href, "id=") {
				if match := reTorrentIDInHref.FindStringSubmatch(href); len(match) >= 2 {
					found = strings.TrimSpace(match[1])
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(row)
	return found
}
