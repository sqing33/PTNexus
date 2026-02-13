package sites

import (
	"errors"
	"regexp"
	"strings"
)

var (
	reAudiencesShowCodeMain = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*show[^"']*["'][^>]*>\s*<div[^>]*class=["'][^"']*codemain[^"']*["'][^>]*>(.*?)</div>\s*</div>`)
	reAudiencesCodeMain     = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*codemain[^"']*["'][^>]*>(.*?)</div>`)
)

// ExtractAudiences 提取人人站点的详情页参数。
// 参数/返回：输入详情页上下文与运行期依赖，返回统一 SeedData。
// 失败场景：公共提取器缺失或公共提取失败。
// 副作用：无。
func ExtractAudiences(input Input, runtime Runtime) (SeedData, error) {
	if runtime.ExtractWithPublic == nil {
		return SeedData{}, errors.New("缺少公共提取器依赖")
	}
	data, err := runtime.ExtractWithPublic(input)
	if err != nil {
		return data.Normalize(input.FallbackTitle), err
	}
	if media := extractAudiencesMediaInfo(input.PageHTML, runtime); media != "" {
		data.MediaInfo = media
	}
	if runtime.BuildSourceParams != nil {
		data.SourceParams = runtime.BuildSourceParams(data)
	}
	return data.Normalize(input.FallbackTitle), nil
}

func extractAudiencesMediaInfo(pageHTML string, runtime Runtime) string {
	if runtime.ExtractRegexCandidates == nil || runtime.PickMediaInfoCandidate == nil || runtime.PickBDInfoCandidate == nil {
		return ""
	}

	blocks := runtime.ExtractRegexCandidates(pageHTML, reAudiencesShowCodeMain)
	if len(blocks) == 0 {
		blocks = runtime.ExtractRegexCandidates(pageHTML, reAudiencesCodeMain)
	}

	candidates := make([]string, 0, len(blocks))
	for _, raw := range blocks {
		clean := cleanAudiencesBDInfoContent(raw, runtime)
		if clean == "" {
			continue
		}
		allowByMediaRule := runtime.IsLikelyMediaInfoText == nil || runtime.IsLikelyMediaInfoText(clean)
		allowByBDRule := runtime.IsLikelyBDInfoText == nil || runtime.IsLikelyBDInfoText(clean)
		if allowByMediaRule || allowByBDRule {
			candidates = append(candidates, clean)
		}
	}

	if picked := runtime.PickMediaInfoCandidate(candidates); picked != "" {
		return picked
	}
	if picked := runtime.PickBDInfoCandidate(candidates); picked != "" {
		return picked
	}
	return ""
}

func cleanAudiencesBDInfoContent(content string, runtime Runtime) string {
	clean := strings.TrimSpace(content)
	if runtime.SanitizeHTMLPreText != nil {
		clean = strings.TrimSpace(runtime.SanitizeHTMLPreText(content, true))
	} else if runtime.SanitizeHTMLText != nil {
		clean = strings.TrimSpace(runtime.SanitizeHTMLText(content, true))
	}
	if clean == "" {
		return ""
	}
	if !strings.Contains(clean, "DISC INFO") || !strings.Contains(clean, "PLAYLIST REPORT") {
		return clean
	}

	subtitlesIdx := strings.Index(clean, "SUBTITLES:")
	if subtitlesIdx < 0 {
		return clean
	}

	prefix := strings.TrimSpace(clean[:subtitlesIdx])
	subtitlesPart := clean[subtitlesIdx:]
	if idx := strings.Index(subtitlesPart, "\n\nFILES:"); idx >= 0 {
		subtitlesPart = subtitlesPart[:idx]
	} else {
		lines := strings.Split(subtitlesPart, "\n")
		trimmed := make([]string, 0, len(lines))
		for _, line := range lines {
			lineTrim := strings.TrimSpace(line)
			if strings.HasPrefix(lineTrim, "FILES:") || strings.Contains(lineTrim, `:\`) {
				break
			}
			trimmed = append(trimmed, line)
		}
		subtitlesPart = strings.Join(trimmed, "\n")
	}
	return strings.TrimSpace(prefix + "\n" + strings.TrimSpace(subtitlesPart))
}
