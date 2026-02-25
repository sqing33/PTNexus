package workflow

import (
	"regexp"
	"strings"
)

var rePTLGSImgTag = regexp.MustCompile(`(?i)\[/?img\]`)

func isPTLGSSite(siteCode string) bool {
	return strings.EqualFold(strings.TrimSpace(siteCode), "ptlgs")
}

func buildPTLGSDescription(uploadData map[string]any) string {
	return strings.TrimSpace(resolvePTLGSSection(uploadData, "statement"))
}

func buildPTLGSImageFields(uploadData map[string]any) (string, string) {
	cover := strings.TrimSpace(stripPTLGSImageBBCode(resolvePTLGSSection(uploadData, "poster")))
	screenshots := strings.TrimSpace(stripPTLGSImageBBCode(resolvePTLGSSection(uploadData, "screenshots")))
	return cover, screenshots
}

func resolvePTLGSSection(uploadData map[string]any, key string) string {
	if uploadData == nil {
		return ""
	}
	if fromTop := strings.TrimSpace(toStringAny(uploadData[key], "")); fromTop != "" {
		return fromTop
	}
	intro, _ := uploadData["intro"].(map[string]any)
	if intro == nil {
		return ""
	}
	return strings.TrimSpace(toStringAny(intro[key], ""))
}

func stripPTLGSImageBBCode(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	cleaned := rePTLGSImgTag.ReplaceAllString(text, "")
	return strings.TrimSpace(cleaned)
}
