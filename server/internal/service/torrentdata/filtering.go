package torrentdata

import (
	"sort"
	"strconv"
	"strings"

	"github.com/pt-nexus/server/internal/repository"
)

// applyFilters 按维护种子列表的查询参数过滤聚合后的种子数据。
// 参数/返回：data 为已聚合的种子列表，params 为筛选条件，siteConfigMap 为站点配置索引；返回过滤后的列表。
// 失败场景：不返回错误，seed_parameters 查询失败时跳过“已存在数据”排除。
// 副作用：开启 ExcludeExisting 时会读取 seed_parameters 中已存在的 name+size 分组。
func (s *TorrentDataService) applyFilters(data []map[string]any, params TorrentsDataParams, siteConfigMap map[string]repository.SiteConfig) []map[string]any {
	filtered := data
	nameSearch := strings.ToLower(strings.TrimSpace(params.NameSearch))
	if nameSearch != "" {
		filtered = filterData(filtered, func(item map[string]any) bool {
			return strings.Contains(strings.ToLower(stringValue(item["name"], "")), nameSearch)
		})
	}

	if len(params.PathFilters) > 0 {
		allowed := toStringSet(params.PathFilters)
		filtered = filterData(filtered, func(item map[string]any) bool {
			_, ok := allowed[stringValue(item["save_path"], "")]
			return ok
		})
	}

	if len(params.StateFilters) > 0 {
		allowed := toStringSet(params.StateFilters)
		filtered = filterData(filtered, func(item map[string]any) bool {
			states := strings.Split(stringValue(item["state"], ""), ",")
			for _, state := range states {
				if _, ok := allowed[strings.TrimSpace(state)]; ok {
					return true
				}
			}
			return false
		})
	}

	if len(params.DownloaderFilters) > 0 {
		allowed := toStringSet(params.DownloaderFilters)
		filtered = filterData(filtered, func(item map[string]any) bool {
			ids := toStringSlice(item["downloaderIds"])
			for _, id := range ids {
				if _, ok := allowed[id]; ok {
					return true
				}
			}
			return false
		})
	}

	if len(params.SourceAvailabilityFilters) > 0 {
		hasAvailableFilter := containsString(params.SourceAvailabilityFilters, "存在源站点")
		hasUnavailableFilter := containsString(params.SourceAvailabilityFilters, "无可用源站点")
		if hasAvailableFilter != hasUnavailableFilter {
			filtered = filterData(filtered, func(item map[string]any) bool {
				sitesMap, _ := item["sites"].(map[string]any)
				hasSource := false
				for siteName := range sitesMap {
					cfg, ok := siteConfigMap[siteName]
					if !ok {
						continue
					}
					if isAvailableBatchFetchSourceSite(siteName, cfg) {
						hasSource = true
						break
					}
				}
				if hasAvailableFilter {
					return hasSource
				}
				return !hasSource
			})
		}
	}

	if len(params.ExistSiteNames) > 0 {
		required := toStringSet(params.ExistSiteNames)
		filtered = filterData(filtered, func(item map[string]any) bool {
			sitesMap, _ := item["sites"].(map[string]any)
			for site := range required {
				if _, exists := sitesMap[site]; !exists {
					return false
				}
			}
			return true
		})
	}

	if len(params.NotExistSiteNames) > 0 {
		forbidden := toStringSet(params.NotExistSiteNames)
		filtered = filterData(filtered, func(item map[string]any) bool {
			sitesMap, _ := item["sites"].(map[string]any)
			for site := range forbidden {
				if _, exists := sitesMap[site]; exists {
					return false
				}
			}
			return true
		})
	}

	if params.ExcludeExisting {
		groups, err := s.repo.ExistingSeedParameterGroups()
		if err == nil {
			existing := map[string]struct{}{}
			for _, group := range groups {
				key := torrentNameSizeKey(group.Name, group.Size)
				if key == "" {
					continue
				}
				existing[key] = struct{}{}
			}
			filtered = filterData(filtered, func(item map[string]any) bool {
				key := torrentNameSizeKey(stringValue(item["name"], ""), int64Value(item["size"], 0))
				_, found := existing[key]
				return !found
			})
		}
	}

	return filtered
}

func torrentNameSizeKey(name string, size int64) string {
	name = strings.TrimSpace(name)
	if name == "" || size <= 0 {
		return ""
	}
	return name + "\x00" + strconv.FormatInt(size, 10)
}

// isAvailableBatchFetchSourceSite 判断站点是否可作为维护种子批量获取的源站点。
// 参数/返回：siteName 为站点昵称，cfg 为 sites 表配置；返回 true 表示列表会展示为可用源站点。
// 失败场景：无。
// 副作用：无。
func isAvailableBatchFetchSourceSite(siteName string, cfg repository.SiteConfig) bool {
	return (cfg.Migration == 1 || cfg.Migration == 3) && strings.TrimSpace(cfg.Cookie) != ""
}

func (s *TorrentDataService) sortData(data []map[string]any, sortProp, sortOrder string) {
	if len(data) <= 1 {
		return
	}

	// Python baseline sorts by a custom "name" comparator by default, and also uses it for
	// all non-numeric sorts (even when sortProp is provided).
	hasExplicit := strings.TrimSpace(sortProp) != "" && strings.TrimSpace(sortOrder) != ""
	descending := hasExplicit && strings.EqualFold(sortOrder, "descending")
	if !hasExplicit {
		sort.SliceStable(data, func(i, j int) bool {
			return customNameLess(stringValue(data[i]["name"], ""), stringValue(data[j]["name"], ""))
		})
		return
	}
	mapped := map[string]string{
		"size_formatted":           "size",
		"total_uploaded_formatted": "total_uploaded",
	}
	if value, ok := mapped[sortProp]; ok {
		sortProp = value
	}

	numericFields := map[string]struct{}{
		"size":               {},
		"progress":           {},
		"total_uploaded":     {},
		"site_count":         {},
		"target_sites_count": {},
		"seeders":            {},
	}

	if _, ok := numericFields[sortProp]; ok {
		sort.SliceStable(data, func(i, j int) bool {
			left := numberValue(data[i][sortProp])
			right := numberValue(data[j][sortProp])
			if descending {
				return left > right
			}
			return left < right
		})
		return
	}

	sort.SliceStable(data, func(i, j int) bool {
		left := stringValue(data[i]["name"], "")
		right := stringValue(data[j]["name"], "")
		if descending {
			return customNameLess(right, left)
		}
		return customNameLess(left, right)
	})
}

func (s *TorrentDataService) selectBestDownloader(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	cfg := s.cfg.Get()
	downloaders := toSlice(cfg["downloaders"])
	enabledOrder := make([]string, 0)
	for _, raw := range downloaders {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if !toBool(item["enabled"], true) {
			continue
		}
		enabledOrder = append(enabledOrder, toString(item["id"], ""))
	}
	for _, candidate := range enabledOrder {
		if containsString(ids, candidate) {
			return candidate
		}
	}
	return ids[0]
}
