package extract

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/pt-nexus/server-go/internal/config"
	"github.com/pt-nexus/server-go/internal/platform/logx"
	"gopkg.in/yaml.v3"
)

const teamNormalizeLogModule = "制作组标准化"

type teamMappingEntry struct {
	SourceLower string
	SourceLen   int
	StandardKey string
	Index       int
}

type teamMappingTable struct {
	exact   map[string]string
	entries []teamMappingEntry
}

var teamMappingCache sync.Map

// NormalizeTeamKeyForSite 将任意制作组文本规范化为标准化 team.* 键。
// 参数/返回：raw 为原始制作组文本（如 FFans@leon、DYZ-WEB、team.frds）；siteCode 用于读取站点 standard_keys.team；
// 返回映射后的 team.*，映射失败返回 team.other。
// 失败场景：映射文件缺失或解析失败时会回退仅全局映射；仍无法映射则返回 team.other。
// 副作用：读取 YAML 配置并缓存映射表。
func NormalizeTeamKeyForSite(raw string, siteCode string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "team.other"
	}
	if direct := normalizeStandardTeamKey(trimmed); direct != "" {
		return direct
	}

	paths := config.ResolveRuntimePaths()
	globalTable := loadGlobalTeamMappingTable(paths.GlobalMapYML)
	siteTable := loadSiteTeamMappingTable(paths.BaseDir, siteCode)

	candidates := buildTeamCandidates(trimmed)
	for _, candidate := range candidates {
		label := normalizeTeamLabelWithoutAtChoice(candidate)
		if label == "" {
			continue
		}
		if standard, ok := resolveTeamMapping(label, siteTable, globalTable); ok {
			return standard
		}
	}
	return "team.other"
}

// NormalizeTeamKey 将制作组文本规范化为标准化 team.* 键（无站点上下文）。
// 参数/返回：raw 为原始制作组文本；返回映射后的 team.*，映射失败返回 team.other。
// 失败场景：全局映射不可读时直接返回 team.other。
// 副作用：读取全局 YAML 并缓存映射表。
func NormalizeTeamKey(raw string) string {
	return NormalizeTeamKeyForSite(raw, "")
}

func normalizeStandardTeamKey(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	lowered := strings.ToLower(trimmed)
	if !strings.HasPrefix(lowered, "team.") {
		return ""
	}
	rest := strings.TrimSpace(trimmed[5:])
	if rest == "" {
		return "team.other"
	}
	if strings.ContainsAny(rest, " \t\r\n") {
		return ""
	}
	return "team." + strings.ToLower(rest)
}

func buildTeamCandidates(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{}
	}
	if !strings.Contains(trimmed, "@") {
		return []string{trimmed}
	}
	parts := strings.Split(trimmed, "@")
	after := ""
	before := ""
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if part != "" {
			after = part
			break
		}
	}
	for i := 0; i < len(parts); i++ {
		part := strings.TrimSpace(parts[i])
		if part != "" {
			before = part
			break
		}
	}
	out := make([]string, 0, 2)
	if after != "" {
		out = append(out, after)
	}
	if before != "" && before != after {
		out = append(out, before)
	}
	return out
}

func normalizeTeamLabelWithoutAtChoice(raw string) string {
	team := strings.TrimSpace(raw)
	if team == "" {
		return ""
	}
	team = strings.Trim(team, "[](){}<> ")
	team = strings.TrimLeft(team, "-@")
	team = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(team, "team."), "TEAM."))
	team = strings.Trim(team, "._- ")
	return strings.TrimSpace(team)
}

func resolveTeamMapping(rawLabel string, siteTable teamMappingTable, globalTable teamMappingTable) (string, bool) {
	label := strings.TrimSpace(rawLabel)
	if label == "" {
		return "", false
	}
	if standard := normalizeStandardTeamKey(label); standard != "" {
		return standard, true
	}
	if standard, ok := siteTable.matchExact(label); ok {
		return standard, true
	}
	if standard, ok := globalTable.matchExact(label); ok {
		return standard, true
	}
	if standard, ok := siteTable.matchPartial(label); ok {
		return standard, true
	}
	if standard, ok := globalTable.matchPartial(label); ok {
		return standard, true
	}
	return "", false
}

func (t teamMappingTable) matchExact(raw string) (string, bool) {
	if len(t.exact) == 0 || strings.TrimSpace(raw) == "" {
		return "", false
	}
	standard, ok := t.exact[strings.ToLower(strings.TrimSpace(raw))]
	if !ok || strings.TrimSpace(standard) == "" {
		return "", false
	}
	return standard, true
}

func (t teamMappingTable) matchPartial(raw string) (string, bool) {
	if len(t.entries) == 0 || strings.TrimSpace(raw) == "" {
		return "", false
	}
	rawLower := strings.ToLower(strings.TrimSpace(raw))
	for _, entry := range t.entries {
		if entry.SourceLower == "" || strings.TrimSpace(entry.StandardKey) == "" {
			continue
		}
		// 对齐 Python：只做“映射 key 是原始值子串”的包含匹配。
		if strings.Contains(rawLower, entry.SourceLower) {
			return entry.StandardKey, true
		}
	}
	return "", false
}

func loadGlobalTeamMappingTable(mappingPath string) teamMappingTable {
	path := strings.TrimSpace(mappingPath)
	if path == "" || filepath.Clean(path) == "." {
		return teamMappingTable{}
	}
	cacheKey := "global:" + path
	if cached, ok := teamMappingCache.Load(cacheKey); ok {
		if table, ok := cached.(teamMappingTable); ok {
			return table
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		logx.Debugf(teamNormalizeLogModule, "读取全局制作组映射失败 path=%s err=%v", path, err)
		return teamMappingTable{}
	}
	table := buildTeamMappingTableFromYAML(data, []string{"global_standard_keys", "team"})
	teamMappingCache.Store(cacheKey, table)
	return table
}

func loadSiteTeamMappingTable(baseDir string, siteCode string) teamMappingTable {
	normalizedSite := normalizeSiteCodeForConfigLocal(siteCode)
	if normalizedSite == "" {
		return teamMappingTable{}
	}
	cfgPath := filepath.Join(strings.TrimSpace(baseDir), "configs", normalizedSite+".yaml")
	cacheKey := "site:" + cfgPath
	if cached, ok := teamMappingCache.Load(cacheKey); ok {
		if table, ok := cached.(teamMappingTable); ok {
			return table
		}
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return teamMappingTable{}
	}
	table := buildTeamMappingTableFromYAML(data, []string{"source_parsers", "standard_keys", "team"})
	teamMappingCache.Store(cacheKey, table)
	return table
}

func normalizeSiteCodeForConfigLocal(siteCode string) string {
	trimmed := strings.ToLower(strings.TrimSpace(siteCode))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, " ", "_")
	trimmed = strings.ReplaceAll(trimmed, "-", "_")
	return strings.TrimSpace(trimmed)
}

func buildTeamMappingTableFromYAML(data []byte, keyPath []string) teamMappingTable {
	node := findYAMLMappingNodeByPath(data, keyPath)
	if node == nil || node.Kind != yaml.MappingNode {
		return teamMappingTable{}
	}
	exact := map[string]string{}
	entries := make([]teamMappingEntry, 0, len(node.Content)/2)
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
		entries = append(entries, teamMappingEntry{
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
	return teamMappingTable{exact: exact, entries: entries}
}

func findYAMLMappingNodeByPath(data []byte, keyPath []string) *yaml.Node {
	if len(keyPath) == 0 {
		return nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil
	}
	if len(root.Content) == 0 {
		return nil
	}
	node := root.Content[0]
	for _, key := range keyPath {
		if node == nil || node.Kind != yaml.MappingNode {
			return nil
		}
		next := (*yaml.Node)(nil)
		for i := 0; i+1 < len(node.Content); i += 2 {
			k := node.Content[i]
			v := node.Content[i+1]
			if k == nil || v == nil {
				continue
			}
			if strings.TrimSpace(k.Value) == key {
				next = v
				break
			}
		}
		node = next
	}
	return node
}
