package tagging

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/platform/logx"
	"gopkg.in/yaml.v3"
)

const tagMappingLogModule = "迁移-标签映射"

type tagMappingEntry struct {
	Source      string
	SourceLower string
	SourceLen   int
	StandardKey string
	Index       int
}

type tagMappingTable struct {
	exact   map[string]string
	entries []tagMappingEntry
}

var tagMappingCache sync.Map

// MapTagsToStandard 将原始标签映射为标准化 tag.*，并过滤掉无法映射的条目。
func MapTagsToStandard(rawTags []string, siteCode string) ([]string, []string) {
	cleaned := make([]string, 0, len(rawTags))
	directStandard := make([]string, 0, 6)
	for _, raw := range rawTags {
		if standard := normalizeStandardTagKey(raw); standard != "" {
			directStandard = append(directStandard, standard)
			continue
		}
		tag := normalizeRawTagForMapping(raw)
		if tag == "" {
			continue
		}
		cleaned = append(cleaned, tag)
	}
	if len(cleaned) == 0 && len(directStandard) == 0 {
		return []string{}, []string{}
	}

	siteTable := loadSiteTagMappingTable(siteCode)
	globalTable := loadGlobalTagMappingTable()

	mapped := make([]string, 0, len(cleaned))
	unmapped := make([]string, 0, 8)
	seen := map[string]struct{}{}
	seenUnmapped := map[string]struct{}{}

	for _, standard := range directStandard {
		if standard == "" {
			continue
		}
		if _, exists := seen[standard]; exists {
			continue
		}
		seen[standard] = struct{}{}
		mapped = append(mapped, standard)
	}

	for _, raw := range cleaned {
		standard, ok := resolveTagMapping(raw, siteTable, globalTable)
		if !ok {
			if _, exists := seenUnmapped[raw]; !exists {
				seenUnmapped[raw] = struct{}{}
				unmapped = append(unmapped, raw)
			}
			continue
		}
		if _, exists := seen[standard]; exists {
			continue
		}
		seen[standard] = struct{}{}
		mapped = append(mapped, standard)
	}

	mapped = deduplicateHDRTags(mapped)
	sort.SliceStable(mapped, func(i, j int) bool {
		leftLower := strings.ToLower(mapped[i])
		rightLower := strings.ToLower(mapped[j])
		if leftLower != rightLower {
			return leftLower < rightLower
		}
		return mapped[i] < mapped[j]
	})
	return mapped, unmapped
}

func normalizeStandardTagKey(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) < 5 || !strings.EqualFold(trimmed[:4], "tag.") {
		return ""
	}
	rest := strings.TrimSpace(trimmed[4:])
	if rest == "" {
		return ""
	}
	if strings.ContainsAny(rest, " \t\r\n") {
		return ""
	}
	return "tag." + rest
}

func normalizeRawTagForMapping(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.Trim(trimmed, "[](){}<> ")
	trimmed = strings.TrimSpace(trimmed)
	if len(trimmed) >= 4 && strings.EqualFold(trimmed[:4], "tag.") {
		trimmed = strings.TrimSpace(trimmed[4:])
	}
	return strings.TrimSpace(trimmed)
}

func resolveTagMapping(raw string, siteTable tagMappingTable, globalTable tagMappingTable) (string, bool) {
	if standard, ok := siteTable.matchExact(raw); ok {
		return standard, true
	}
	if standard, ok := globalTable.matchExact(raw); ok {
		return standard, true
	}
	if standard, ok := siteTable.matchPartial(raw); ok {
		return standard, true
	}
	if standard, ok := globalTable.matchPartial(raw); ok {
		return standard, true
	}
	return "", false
}

func (t tagMappingTable) matchExact(raw string) (string, bool) {
	if len(t.exact) == 0 || strings.TrimSpace(raw) == "" {
		return "", false
	}
	standard, ok := t.exact[strings.ToLower(strings.TrimSpace(raw))]
	if !ok || strings.TrimSpace(standard) == "" {
		return "", false
	}
	return standard, true
}

func (t tagMappingTable) matchPartial(raw string) (string, bool) {
	if len(t.entries) == 0 || strings.TrimSpace(raw) == "" {
		return "", false
	}
	rawLower := strings.ToLower(strings.TrimSpace(raw))
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

func loadGlobalTagMappingTable() tagMappingTable {
	paths := config.ResolveRuntimePaths()
	mappingPath := strings.TrimSpace(paths.GlobalMapYML)
	if mappingPath == "" || filepath.Clean(mappingPath) == "." {
		return tagMappingTable{}
	}
	cacheKey := "global:" + mappingPath
	if cached, ok := tagMappingCache.Load(cacheKey); ok {
		if table, ok := cached.(tagMappingTable); ok {
			return table
		}
	}

	data, err := os.ReadFile(mappingPath)
	if err != nil {
		logx.Debugf(tagMappingLogModule, "读取全局tag映射失败 path=%s err=%v", mappingPath, err)
		return tagMappingTable{}
	}

	table := buildTagMappingTableFromYAML(data, []string{"global_standard_keys", "tag"})
	tagMappingCache.Store(cacheKey, table)
	return table
}

func loadSiteTagMappingTable(siteCode string) tagMappingTable {
	normalizedSite := normalizeSiteCodeForConfig(siteCode)
	if normalizedSite == "" {
		return tagMappingTable{}
	}

	paths := config.ResolveRuntimePaths()
	cfgPath := filepath.Join(paths.BaseDir, "configs", normalizedSite+".yaml")
	cacheKey := "site:" + cfgPath
	if cached, ok := tagMappingCache.Load(cacheKey); ok {
		if table, ok := cached.(tagMappingTable); ok {
			return table
		}
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return tagMappingTable{}
	}

	table := buildTagMappingTableFromYAML(data, []string{"source_parsers", "standard_keys", "tag"})
	tagMappingCache.Store(cacheKey, table)
	return table
}

func normalizeSiteCodeForConfig(siteCode string) string {
	trimmed := strings.ToLower(strings.TrimSpace(siteCode))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, " ", "_")
	trimmed = strings.ReplaceAll(trimmed, "-", "_")
	return strings.TrimSpace(trimmed)
}

func buildTagMappingTableFromYAML(data []byte, keyPath []string) tagMappingTable {
	node := findYAMLMappingNodeByPath(data, keyPath)
	if node == nil || node.Kind != yaml.MappingNode {
		return tagMappingTable{}
	}

	entries := make([]tagMappingEntry, 0, len(node.Content)/2)
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
		entries = append(entries, tagMappingEntry{
			Source:      sourceText,
			SourceLower: strings.ToLower(sourceText),
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

	exact := map[string]string{}
	for _, entry := range entries {
		if entry.SourceLower == "" || strings.TrimSpace(entry.StandardKey) == "" {
			continue
		}
		exact[entry.SourceLower] = entry.StandardKey
	}

	return tagMappingTable{
		exact:   exact,
		entries: entries,
	}
}

func findYAMLMappingNodeByPath(data []byte, path []string) *yaml.Node {
	if len(data) == 0 || len(path) == 0 {
		return nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil
	}
	if len(root.Content) == 0 {
		return nil
	}
	current := root.Content[0]
	for _, key := range path {
		if current == nil || current.Kind != yaml.MappingNode {
			return nil
		}
		next := mappingValueNode(current, key)
		if next == nil {
			return nil
		}
		current = next
	}
	return current
}

func mappingValueNode(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for idx := 0; idx+1 < len(mapping.Content); idx += 2 {
		k := mapping.Content[idx]
		v := mapping.Content[idx+1]
		if k == nil || v == nil {
			continue
		}
		if strings.TrimSpace(k.Value) == key {
			return v
		}
	}
	return nil
}

func deduplicateHDRTags(tags []string) []string {
	hasHDR := false
	hasHDR10Plus := false
	for _, tag := range tags {
		switch strings.TrimSpace(tag) {
		case "tag.HDR":
			hasHDR = true
		case "tag.HDR10+":
			hasHDR10Plus = true
		}
	}
	if !(hasHDR && hasHDR10Plus) {
		return tags
	}
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		if strings.TrimSpace(tag) == "tag.HDR" {
			continue
		}
		result = append(result, tag)
	}
	return result
}
