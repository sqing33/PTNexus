package checker

import (
	"html"
	"regexp"
	"strings"
)

var reCheckerHTMLTag = regexp.MustCompile(`(?is)<[^>]+>`)

func normalizeExtractedName(raw string) string {
	text := reCheckerHTMLTag.ReplaceAllString(strings.TrimSpace(raw), " ")
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}
