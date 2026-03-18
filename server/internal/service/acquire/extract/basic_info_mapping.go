package extract

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/pt-nexus/server/internal/config"
	"gopkg.in/yaml.v3"
)

type pageBasicInfo struct {
	Raw      map[string]string
	Standard map[string]string
}

type standardFieldMappingEntry struct {
	SourceLower string
	SourceLen   int
	StandardKey string
	Index       int
}

type standardFieldMappingTable struct {
	exact   map[string]string
	entries []standardFieldMappingEntry
}

var (
	standardFieldMappingCache sync.Map
	sourceFieldKeyCache       sync.Map
)

var supportedBasicInfoFields = []string{
	"type",
	"medium",
	"video_codec",
	"audio_codec",
	"resolution",
	"team",
	"source",
}

var defaultBasicInfoLabels = map[string][]string{
	"type":        {"类型", "類型", "category", "type"},
	"medium":      {"媒介", "媒体", "媒體", "medium"},
	"video_codec": {"视频编码", "視頻編碼", "编码", "編碼", "video codec", "codec"},
	"audio_codec": {"音频编码", "音頻編碼", "音频", "音頻", "audio codec", "audio"},
	"resolution":  {"分辨率", "解析度", "resolution"},
	"team":        {"制作组", "製作組", "团队", "團隊", "team"},
	"source":      {"产地", "產地", "地区", "地區", "来源", "來源", "处理", "處理", "source"},
}

var basicInfoSectionLabels = []string{"基本信息", "基本資料", "basic info", "basicinfo"}

func extractMappedBasicInfoFromPage(pageHTML string, siteCode string) pageBasicInfo {
	result := pageBasicInfo{
		Raw:      map[string]string{},
		Standard: map[string]string{},
	}

	inlineValues := extractBasicInfoInlineValues(pageHTML, siteCode)
	for _, field := range supportedBasicInfoFields {
		labels := resolveBasicInfoLabels(siteCode, field)
		raw := pickBasicInfoValueByLabels(inlineValues, labels)
		if raw == "" {
			raw = extractBasicInfoValueFromRows(pageHTML, labels)
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		result.Raw[field] = raw
		if mapped := mapBasicInfoFieldToStandard(field, raw, siteCode); mapped != "" {
			result.Standard[field] = mapped
		}
	}

	return result
}

func extractBasicInfoInlineValues(pageHTML string, siteCode string) map[string]string {
	values := map[string]string{}
	cell := findDetailValueCellByLabels(pageHTML, basicInfoSectionLabels)
	if cell == nil {
		return values
	}

	text := strings.TrimSpace(extractVisibleText(cell))
	if text == "" {
		return values
	}

	labels := collectBasicInfoInlineLabels(siteCode)
	if len(labels) == 0 {
		return values
	}

	pattern := buildBasicInfoInlinePattern(labels)
	if pattern == nil {
		return values
	}

	matchIndexes := pattern.FindAllStringSubmatchIndex(text, -1)
	for i, idx := range matchIndexes {
		if len(idx) < 4 {
			continue
		}

		label := normalizeDetailFieldLabel(text[idx[2]:idx[3]])
		valueStart := idx[1]
		valueEnd := len(text)
		if i+1 < len(matchIndexes) && len(matchIndexes[i+1]) >= 1 {
			valueEnd = matchIndexes[i+1][0]
		}
		value := strings.TrimSpace(text[valueStart:valueEnd])
		if label == "" || value == "" {
			continue
		}
		if _, exists := values[label]; exists {
			continue
		}
		values[label] = value
	}

	return values
}

func buildBasicInfoInlinePattern(labels []string) *regexp.Regexp {
	if len(labels) == 0 {
		return nil
	}
	sorted := append([]string{}, labels...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left := utf8.RuneCountInString(sorted[i])
		right := utf8.RuneCountInString(sorted[j])
		if left != right {
			return left > right
		}
		return strings.ToLower(sorted[i]) < strings.ToLower(sorted[j])
	})

	parts := make([]string, 0, len(sorted))
	for _, label := range sorted {
		trimmed := strings.TrimSpace(label)
		if trimmed == "" {
			continue
		}
		parts = append(parts, regexp.QuoteMeta(trimmed))
	}
	if len(parts) == 0 {
		return nil
	}

	joined := strings.Join(parts, "|")
	return regexp.MustCompile(`(?is)(` + joined + `)\s*[:：]`)
}

func collectBasicInfoInlineLabels(siteCode string) []string {
	labels := make([]string, 0, len(supportedBasicInfoFields)*4)
	for _, field := range supportedBasicInfoFields {
		for _, label := range resolveBasicInfoLabels(siteCode, field) {
			labels = appendUniqueString(labels, label)
		}
	}
	return labels
}

func pickBasicInfoValueByLabels(values map[string]string, labels []string) string {
	if len(values) == 0 || len(labels) == 0 {
		return ""
	}
	for _, label := range labels {
		normalized := normalizeDetailFieldLabel(label)
		if normalized == "" {
			continue
		}
		if value := strings.TrimSpace(values[normalized]); value != "" {
			return value
		}
	}
	return ""
}

func extractBasicInfoValueFromRows(pageHTML string, labels []string) string {
	cell := findDetailValueCellByLabels(pageHTML, labels)
	if cell == nil {
		return ""
	}
	return strings.TrimSpace(extractVisibleText(cell))
}

func resolveBasicInfoLabels(siteCode string, field string) []string {
	labels := make([]string, 0, 6)
	if sourceKey := loadSiteSourceFieldKey(siteCode, field); sourceKey != "" {
		labels = append(labels, sourceKey)
	}
	for _, label := range defaultBasicInfoLabels[field] {
		labels = append(labels, label)
	}
	result := make([]string, 0, len(labels))
	for _, label := range labels {
		result = appendUniqueString(result, label)
	}
	return result
}

func loadSiteSourceFieldKey(siteCode string, field string) string {
	normalizedSite := normalizeSiteCodeForConfigLocal(siteCode)
	if normalizedSite == "" || strings.TrimSpace(field) == "" {
		return ""
	}

	paths := config.ResolveRuntimePaths()
	cfgPath := filepath.Join(strings.TrimSpace(paths.BaseDir), "configs", normalizedSite+".yaml")
	cacheKey := "site-source-key:" + cfgPath + ":" + field
	if cached, ok := sourceFieldKeyCache.Load(cacheKey); ok {
		if value, ok := cached.(string); ok {
			return value
		}
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return ""
	}
	node := findYAMLMappingNodeByPath(data, []string{"source_parsers", "source_params", field, "source_key"})
	if node == nil {
		sourceFieldKeyCache.Store(cacheKey, "")
		return ""
	}
	value := strings.TrimSpace(node.Value)
	sourceFieldKeyCache.Store(cacheKey, value)
	return value
}

func mapBasicInfoFieldToStandard(field string, raw string, siteCode string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if field == "team" {
		teamKey := NormalizeTeamKeyForSite(trimmed, siteCode)
		if teamKey == "" || teamKey == "team.other" {
			return ""
		}
		return teamKey
	}
	if direct := normalizeDirectStandardFieldKey(field, trimmed); direct != "" {
		return direct
	}

	paths := config.ResolveRuntimePaths()
	siteTable := loadSiteStandardFieldMappingTable(paths.BaseDir, siteCode, field)
	globalTable := loadGlobalStandardFieldMappingTable(paths.GlobalMapYML, field)

	if mapped, ok := resolveStandardFieldMapping(trimmed, siteTable, globalTable); ok {
		return mapped
	}
	return ""
}

func resolveStandardFieldMapping(raw string, siteTable standardFieldMappingTable, globalTable standardFieldMappingTable) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	if mapped, ok := siteTable.matchExact(trimmed); ok {
		return mapped, true
	}
	if mapped, ok := globalTable.matchExact(trimmed); ok {
		return mapped, true
	}
	if mapped, ok := siteTable.matchPartial(trimmed); ok {
		return mapped, true
	}
	if mapped, ok := globalTable.matchPartial(trimmed); ok {
		return mapped, true
	}
	return "", false
}

func (t standardFieldMappingTable) matchExact(raw string) (string, bool) {
	if len(t.exact) == 0 {
		return "", false
	}
	standard, ok := t.exact[strings.ToLower(strings.TrimSpace(raw))]
	if !ok || strings.TrimSpace(standard) == "" {
		return "", false
	}
	return standard, true
}

func (t standardFieldMappingTable) matchPartial(raw string) (string, bool) {
	if len(t.entries) == 0 {
		return "", false
	}
	rawLower := strings.ToLower(strings.TrimSpace(raw))
	if rawLower == "" {
		return "", false
	}
	for _, entry := range t.entries {
		if entry.SourceLower == "" || strings.TrimSpace(entry.StandardKey) == "" {
			continue
		}
		if strings.Contains(rawLower, entry.SourceLower) || strings.Contains(entry.SourceLower, rawLower) {
			return entry.StandardKey, true
		}
	}
	return "", false
}

func loadGlobalStandardFieldMappingTable(mappingPath string, field string) standardFieldMappingTable {
	path := strings.TrimSpace(mappingPath)
	if path == "" || filepath.Clean(path) == "." || strings.TrimSpace(field) == "" {
		return standardFieldMappingTable{}
	}
	cacheKey := "global-standard-field:" + path + ":" + field
	if cached, ok := standardFieldMappingCache.Load(cacheKey); ok {
		if table, ok := cached.(standardFieldMappingTable); ok {
			return table
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return standardFieldMappingTable{}
	}
	table := buildStandardFieldMappingTableFromYAML(data, []string{"global_standard_keys", field})
	standardFieldMappingCache.Store(cacheKey, table)
	return table
}

func loadSiteStandardFieldMappingTable(baseDir string, siteCode string, field string) standardFieldMappingTable {
	normalizedSite := normalizeSiteCodeForConfigLocal(siteCode)
	if normalizedSite == "" || strings.TrimSpace(field) == "" {
		return standardFieldMappingTable{}
	}
	cfgPath := filepath.Join(strings.TrimSpace(baseDir), "configs", normalizedSite+".yaml")
	cacheKey := "site-standard-field:" + cfgPath + ":" + field
	if cached, ok := standardFieldMappingCache.Load(cacheKey); ok {
		if table, ok := cached.(standardFieldMappingTable); ok {
			return table
		}
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return standardFieldMappingTable{}
	}
	table := buildStandardFieldMappingTableFromYAML(data, []string{"source_parsers", "standard_keys", field})
	standardFieldMappingCache.Store(cacheKey, table)
	return table
}

func buildStandardFieldMappingTableFromYAML(data []byte, keyPath []string) standardFieldMappingTable {
	node := findYAMLMappingNodeByPath(data, keyPath)
	if node == nil || node.Kind != yaml.MappingNode {
		return standardFieldMappingTable{}
	}

	exact := map[string]string{}
	entries := make([]standardFieldMappingEntry, 0, len(node.Content)/2)
	index := 0
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		if keyNode == nil || valueNode == nil {
			continue
		}
		sourceText := strings.TrimSpace(keyNode.Value)
		if sourceText == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(valueNode.Tag), "!!null") {
			continue
		}
		standard := strings.TrimSpace(valueNode.Value)
		if standard == "" {
			continue
		}
		lowered := strings.ToLower(sourceText)
		exact[lowered] = standard
		entries = append(entries, standardFieldMappingEntry{
			SourceLower: lowered,
			SourceLen:   utf8.RuneCountInString(sourceText),
			StandardKey: standard,
			Index:       index,
		})
		index++
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].SourceLen != entries[j].SourceLen {
			return entries[i].SourceLen > entries[j].SourceLen
		}
		return entries[i].Index < entries[j].Index
	})
	return standardFieldMappingTable{exact: exact, entries: entries}
}

func normalizeDirectStandardFieldKey(field string, raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	prefix := standardFieldPrefix(field)
	if prefix == "" {
		return ""
	}
	lowered := strings.ToLower(trimmed)
	if !strings.HasPrefix(lowered, prefix) {
		return ""
	}
	rest := strings.TrimSpace(trimmed[len(prefix):])
	if rest == "" || strings.ContainsAny(rest, " \t\r\n") {
		return ""
	}
	return prefix + strings.ToLower(rest)
}

func standardFieldPrefix(field string) string {
	switch strings.TrimSpace(field) {
	case "type":
		return "category."
	case "medium":
		return "medium."
	case "video_codec":
		return "video."
	case "audio_codec":
		return "audio."
	case "resolution":
		return "resolution."
	case "source":
		return "source."
	default:
		return ""
	}
}
