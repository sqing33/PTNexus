package sites

import (
	"errors"
	"regexp"
	"strings"
)

var (
	reDStudioKeyValue = regexp.MustCompile(`(?is)^\s*[❁✦★•\-*]*\s*([A-Za-z][A-Za-z0-9 /&()._-]*?)\s*[:：]\s*(.*?)\s*$`)
	reDStudioHeading  = regexp.MustCompile(`(?is)^\s*[❁✦★•\-*]*\s*([A-Za-z][A-Za-z0-9 /&()._-]*?)\s*$`)
	reDStudioCastLine = regexp.MustCompile(`(?is)^\s*(.+?)\s+(?:as|饰)\s+(.+?)\s*$`)
)

var dstudioFieldLabels = map[string]string{
	"title":                "片名",
	"original title":       "原名",
	"genres":               "类别",
	"genre":                "类别",
	"language":             "语言",
	"languages":            "语言",
	"first air date":       "首播",
	"number of episodes":   "集数",
	"number of seasons":    "季数",
	"episode runtime":      "单集片长",
	"production countries": "产地",
	"production country":   "产地",
	"rating":               "评分",
	"tmdb link":            "TMDB链接",
	"imdb link":            "IMDb链接",
	"directors":            "导演",
	"director":             "导演",
	"cast":                 "主演",
	"overview":             "简介",
	"plot":                 "简介",
	"synopsis":             "简介",
	"description":          "简介",
}

var dstudioCountryValues = map[string]string{
	"south korea":       "韩国",
	"korea":             "韩国",
	"republic of korea": "韩国",
	"united states":     "美国",
	"usa":               "美国",
	"u.s.a.":            "美国",
	"japan":             "日本",
	"china":             "中国",
	"hong kong":         "中国香港",
	"taiwan":            "中国台湾",
}

// ExtractDStudio 针对屌丝站的详情页简介进行特殊转换。
// 参数/返回：输入为屌丝站详情页上下文与公共提取器依赖，返回统一 SeedData。
// 失败场景：公共提取器未注入时返回错误，供上层回退公共提取器。
// 副作用：不写库，仅将 TMDB 英文字段重写为中文简介正文。
func ExtractDStudio(input Input, runtime Runtime) (SeedData, error) {
	if runtime.ExtractWithPublic == nil {
		return SeedData{}, errors.New("缺少公共提取器依赖")
	}

	data, _ := runtime.ExtractWithPublic(input)
	combined := strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(data.Intro.Statement),
		strings.TrimSpace(data.Intro.Body),
	}, "\n"))

	if looksLikeDStudioIntro(combined) {
		translated := normalizeDStudioIntroBody(combined)
		if strings.TrimSpace(translated) != "" {
			data.Intro.Statement = ""
			data.Intro.Body = translated
		}
	}

	if strings.TrimSpace(data.Intro.Body) == "" && combined != "" {
		data.Intro.Body = combined
	}
	if strings.TrimSpace(data.Intro.Body) == "" {
		data.Intro.Body = "◎简　　介　暂无简介"
	}
	if runtime.BuildSourceParams != nil {
		data.SourceParams = runtime.BuildSourceParams(data)
	}
	return data.Normalize(input.FallbackTitle), nil
}

func looksLikeDStudioIntro(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "❁") {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, marker := range []string{"title:", "original title:", "tmdb link:", "imdb link:", "overview:", "number of episodes:", "production countries:", "cast"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func normalizeDStudioIntroBody(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	sections := make([]string, 0, len(lines)+4)
	castMode := false
	castStarted := false
	hasSummary := false

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			if len(sections) > 0 && sections[len(sections)-1] != "" {
				sections = append(sections, "")
			}
			continue
		}

		key, value, ok := parseDStudioLabelLine(line)
		if ok {
			label := normalizeDStudioFieldLabel(key)
			if label == "" {
				castMode = false
				continue
			}
			if label == "主演" {
				castMode = true
				if value == "" {
					sections = append(sections, "◎主　　演")
					castStarted = true
					continue
				}
				castLine := normalizeDStudioCastEntry(value)
				sections = append(sections, formatDStudioIntroLine(label, castLine))
				castStarted = true
				continue
			}
			castMode = false
			if label == "简介" {
				hasSummary = true
			}
			sections = append(sections, formatDStudioIntroLine(label, normalizeDStudioFieldValue(key, value)))
			continue
		}

		if castMode {
			if castLine := normalizeDStudioCastEntry(line); castLine != "" {
				if !castStarted {
					sections = append(sections, "◎主　　演")
					castStarted = true
				}
				sections = append(sections, "　　　　　　"+castLine)
				continue
			}
		}
		sections = append(sections, line)
	}

	if !hasSummary {
		if len(sections) > 0 && sections[len(sections)-1] != "" {
			sections = append(sections, "")
		}
		sections = append(sections, "◎简　　介　暂无简介")
	}
	return joinDStudioLines(sections)
}

func parseDStudioLabelLine(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(strings.TrimLeft(line, "❁✦★•-* "))
	if trimmed == "" {
		return "", "", false
	}
	if matches := reDStudioKeyValue.FindStringSubmatch(trimmed); len(matches) >= 3 {
		return strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2]), true
	}
	if matches := reDStudioHeading.FindStringSubmatch(trimmed); len(matches) >= 2 {
		key := strings.TrimSpace(matches[1])
		if normalizeDStudioFieldLabel(key) != "" {
			return key, "", true
		}
	}
	return "", "", false
}

func normalizeDStudioFieldLabel(label string) string {
	return dstudioFieldLabels[strings.ToLower(strings.TrimSpace(label))]
}

func normalizeDStudioFieldValue(label, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.EqualFold(trimmed, "N/A") {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "production countries", "production country":
		if mapped := normalizeDStudioCountryValue(trimmed); mapped != "" {
			return mapped
		}
	case "cast":
		if castLine := normalizeDStudioCastEntry(trimmed); castLine != "" {
			return castLine
		}
	}
	return trimmed
}

func normalizeDStudioCountryValue(value string) string {
	parts := splitDStudioValues(value)
	if len(parts) == 0 {
		return strings.TrimSpace(value)
	}
	mapped := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if chinese, ok := dstudioCountryValues[strings.ToLower(trimmed)]; ok {
			mapped = append(mapped, chinese)
			continue
		}
		mapped = append(mapped, trimmed)
	}
	if len(mapped) == 0 {
		return strings.TrimSpace(value)
	}
	return strings.Join(mapped, " / ")
}

func normalizeDStudioCastEntry(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	matches := reDStudioCastLine.FindStringSubmatch(trimmed)
	if len(matches) >= 3 {
		left := strings.TrimSpace(matches[1])
		right := strings.TrimSpace(matches[2])
		if left != "" && right != "" {
			return left + " 饰 " + right
		}
	}
	return trimmed
}

func formatDStudioIntroLine(label string, value string) string {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		trimmedValue = "暂无"
	}
	switch label {
	case "片名":
		return "◎片　　名　" + trimmedValue
	case "原名":
		return "◎原　　名　" + trimmedValue
	case "类别":
		return "◎类　　别　" + trimmedValue
	case "语言":
		return "◎语　　言　" + trimmedValue
	case "首播":
		return "◎首　　播　" + trimmedValue
	case "集数":
		return "◎集　　数　" + trimmedValue
	case "季数":
		return "◎季　　数　" + trimmedValue
	case "单集片长":
		return "◎单集片长　" + trimmedValue
	case "产地":
		return "◎产　　地　" + trimmedValue
	case "评分":
		return "◎评　　分　" + trimmedValue
	case "TMDB链接":
		return "◎TMDB链接　" + trimmedValue
	case "IMDb链接":
		return "◎IMDb链接　" + trimmedValue
	case "导演":
		return "◎导　　演　" + trimmedValue
	case "主演":
		return "◎主　　演　" + trimmedValue
	case "简介":
		return "◎简　　介　" + trimmedValue
	default:
		return "◎" + label + "　" + trimmedValue
	}
}

func joinDStudioLines(lines []string) string {
	cleaned := make([]string, 0, len(lines))
	lastBlank := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(cleaned) == 0 || lastBlank {
				continue
			}
			cleaned = append(cleaned, "")
			lastBlank = true
			continue
		}
		cleaned = append(cleaned, trimmed)
		lastBlank = false
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func splitDStudioValues(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case '/', ',', '、', '|', ';', '／':
			return true
		}
		return false
	})
	if len(fields) == 0 {
		return []string{value}
	}
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		trimmed := strings.TrimSpace(field)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
