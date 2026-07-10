package localquery

func (s *Service) AnalyzeDuplicates(videoOnly bool) (map[string]any, error) {
	names, err := s.repo.ListDuplicateNames(videoOnly)
	if err != nil {
		return nil, err
	}
	downloaders := s.downloaderMap()
	duplicates := make([]map[string]any, 0, len(names))
	totalWasted := int64(0)

	for _, row := range names {
		instances, err := s.repo.ListTorrentsByName(row.Name, videoOnly)
		if err != nil {
			continue
		}
		locations := make([]map[string]any, 0, len(instances))
		sizes := make([]int64, 0, len(instances))
		totalSize := int64(0)

		for _, item := range instances {
			dlName := "未知"
			if meta, ok := downloaders[item.DownloaderID]; ok {
				dlName = meta.Name
			}
			locations = append(locations, map[string]any{
				"hash":            item.Hash,
				"downloader_name": dlName,
				"path":            item.SavePath,
			})
			totalSize += item.Size
			sizes = append(sizes, item.Size)
		}
		maxSize := int64(0)
		for _, size := range sizes {
			if size > maxSize {
				maxSize = size
			}
		}
		wasted := totalSize - maxSize
		if wasted < 0 {
			wasted = 0
		}
		totalWasted += wasted

		duplicates = append(duplicates, map[string]any{
			"name":        row.Name,
			"count":       row.Count,
			"locations":   locations,
			"total_size":  totalSize,
			"wasted_size": wasted,
		})
	}

	return map[string]any{
		"duplicates":       duplicates,
		"total_duplicates": len(duplicates),
		"wasted_space":     totalWasted,
	}, nil
}
