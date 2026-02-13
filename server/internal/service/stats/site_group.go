package stats

import "strings"

func (s *Service) GetSiteStats() ([]map[string]any, error) {
	rows, err := s.repo.QuerySiteStats()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]any{
			"site_name":     row.SiteName,
			"total_size":    row.TotalSize,
			"torrent_count": row.TorrentCount,
		})
	}
	return result, nil
}

func (s *Service) GetGroupStats(siteName string) ([]map[string]any, error) {
	rows, err := s.repo.QueryGroupStats(siteName)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]any{
			"site_name":     row.SiteName,
			"group_suffix":  strings.ReplaceAll(row.GroupSuffix, "-", ""),
			"torrent_count": row.TorrentCount,
			"total_size":    row.TotalSize,
		})
	}
	return result, nil
}
