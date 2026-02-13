package repair

import (
	"regexp"
	"strings"
)

// introCompletenessResult 表示简介完整性检测结果。
type introCompletenessResult struct {
	IsComplete    bool
	MissingFields []string
	FoundFields   []string
}

var introRequiredPatterns = map[string][]*regexp.Regexp{
	"片名": {
		regexp.MustCompile(`(?i)[◎❁]\s*片\s*名`),
		regexp.MustCompile(`(?i)[◎❁]\s*译\s*名`),
		regexp.MustCompile(`(?i)[◎❁]\s*标\s*题`),
		regexp.MustCompile(`(?i)片名\s*[:：]`),
		regexp.MustCompile(`(?i)译名\s*[:：]`),
		regexp.MustCompile(`(?i)Title\s*[:：]`),
	},
	"产地": {
		regexp.MustCompile(`(?i)[◎❁]\s*产\s*地`),
		regexp.MustCompile(`(?i)[◎❁]\s*国\s*家`),
		regexp.MustCompile(`(?i)[◎❁]\s*地\s*区`),
		regexp.MustCompile(`(?i)制片国家/地区\s*[:：]`),
		regexp.MustCompile(`(?i)制片国家\s*[:：]`),
		regexp.MustCompile(`(?i)国家\s*[:：]`),
		regexp.MustCompile(`(?i)产地\s*[:：]`),
		regexp.MustCompile(`(?i)Country\s*[:：]`),
	},
	"简介": {
		regexp.MustCompile(`(?i)[◎❁]\s*简\s*介`),
		regexp.MustCompile(`(?i)[◎❁]\s*剧\s*情`),
		regexp.MustCompile(`(?i)[◎❁]\s*内\s*容`),
		regexp.MustCompile(`(?i)简介\s*[:：]`),
		regexp.MustCompile(`(?i)剧情\s*[:：]`),
		regexp.MustCompile(`(?i)内容简介\s*[:：]`),
		regexp.MustCompile(`(?i)Plot\s*[:：]`),
		regexp.MustCompile(`(?i)Synopsis\s*[:：]`),
	},
}

var introCriticalFields = []string{"片名", "产地", "简介"}

// checkIntroBodyCompleteness 按前端一致规则检查简介正文是否包含关键字段。
func checkIntroBodyCompleteness(bodyText string) introCompletenessResult {
	result := introCompletenessResult{
		IsComplete:    false,
		MissingFields: []string{},
		FoundFields:   []string{},
	}
	if bodyText == "" {
		result.MissingFields = []string{"所有字段"}
		return result
	}
	// 兼容“◎片　　名 / ◎产　　地 / ◎简　　介”中的全角空格（U+3000）。
	normalizedText := strings.ReplaceAll(bodyText, "\u3000", " ")

	for fieldName, patterns := range introRequiredPatterns {
		fieldFound := false
		for _, pattern := range patterns {
			if pattern.MatchString(normalizedText) {
				fieldFound = true
				break
			}
		}
		if fieldFound {
			result.FoundFields = append(result.FoundFields, fieldName)
		} else {
			result.MissingFields = append(result.MissingFields, fieldName)
		}
	}

	result.IsComplete = true
	for _, field := range introCriticalFields {
		matched := false
		for _, foundField := range result.FoundFields {
			if foundField == field {
				matched = true
				break
			}
		}
		if !matched {
			result.IsComplete = false
			break
		}
	}
	return result
}
