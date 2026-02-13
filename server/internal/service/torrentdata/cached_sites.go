package torrentdata

import "strings"

func (s *TorrentDataService) GetCachedSites(name string, size int64) (map[string]any, int) {
	if strings.TrimSpace(name) == "" {
		return map[string]any{"error": "缺少必要参数：name"}, 400
	}
	if size <= 0 {
		return map[string]any{"error": "size参数必须是整数"}, 400
	}
	cachedSites, matchedHashes, err := s.repo.CachedSitesByNameAndSize(name, size)
	if err != nil {
		return map[string]any{"error": "查询缓存站点失败", "success": false}, 500
	}
	return map[string]any{"success": true, "cached_sites": cachedSites, "matched_hashes": matchedHashes}, 200
}
