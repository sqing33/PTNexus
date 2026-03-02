package stats

import (
	"sort"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/repository"
)

func (s *Service) GetSpeedData() (map[string]any, error) {
	rows, err := s.repo.QueryLatestSpeeds()
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	for _, row := range rows {
		result[row.DownloaderID] = map[string]any{
			"upload_speed":   row.UploadSpeed,
			"download_speed": row.DownloadSpeed,
			"ul_speed":       row.UploadSpeed,
			"dl_speed":       row.DownloadSpeed,
		}
	}
	return result, nil
}

func (s *Service) GetRecentSpeedData(seconds int) (map[string]any, error) {
	if seconds <= 0 {
		seconds = 60
	}
	end := time.Now()
	start := end.Add(-time.Duration(seconds) * time.Second)
	rows, err := s.repo.QuerySpeedAverages(start, end, "%H:%M:%S")
	if err != nil {
		return nil, err
	}
	return s.buildSpeedResponse(rows), nil
}

func (s *Service) GetSpeedChartData(rangeKey string) (map[string]any, error) {
	start, end, format := resolveTimeRange(rangeKey, true)
	now := time.Now()
	longPeriodThreshold := now.Add(-48 * time.Hour)

	var rows []repository.SpeedRow
	if start.Before(longPeriodThreshold) {
		coarseFormat := format
		useCoarseGrouping := true
		if (rangeKey == "this_week" || rangeKey == "last_week") && strings.Contains(format, "%H") {
			useCoarseGrouping = false
		}
		if useCoarseGrouping && strings.Contains(format, "%H") {
			coarseFormat = strings.Split(format, " %H")[0]
		}

		rowsHourly, err := s.repo.QuerySpeedAveragesHourly(start, end, coarseFormat)
		if err != nil {
			return nil, err
		}

		recentThreshold := now.Add(-3 * 24 * time.Hour)
		recentStart := recentThreshold
		if start.After(recentThreshold) {
			recentStart = start
		}

		if recentStart.Before(end) {
			rowsFine, err := s.repo.QuerySpeedAverages(recentStart, end, format)
			if err != nil {
				return nil, err
			}
			rows = append(rowsHourly, rowsFine...)
		} else {
			rows = rowsHourly
		}
	} else {
		var err error
		rows, err = s.repo.QuerySpeedAverages(start, end, format)
		if err != nil {
			return nil, err
		}
	}

	return s.buildSpeedChartResponse(rows), nil
}

func (s *Service) buildSpeedResponse(rows []repository.SpeedRow) map[string]any {
	downloaders := s.enabledDownloaders()
	labelSet := map[string]struct{}{}
	for _, row := range rows {
		if row.TimeGroup == "" {
			continue
		}
		labelSet[row.TimeGroup] = struct{}{}
	}
	labels := make([]string, 0, len(labelSet))
	for label := range labelSet {
		labels = append(labels, label)
	}
	sort.Strings(labels)

	byTime := map[string]map[string]map[string]float64{}
	for _, label := range labels {
		byTime[label] = map[string]map[string]float64{}
	}
	for _, row := range rows {
		if _, ok := byTime[row.TimeGroup]; !ok {
			continue
		}
		byTime[row.TimeGroup][row.DownloaderID] = map[string]float64{
			"ul_speed": row.ULSpeed,
			"dl_speed": row.DLSpeed,
		}
	}

	datasets := make([]map[string]any, 0, len(labels))
	for _, label := range labels {
		speeds := map[string]any{}
		for _, downloader := range downloaders {
			id := toString(downloader["id"], "")
			entry, exists := byTime[label][id]
			if !exists {
				speeds[id] = map[string]any{"ul_speed": 0, "dl_speed": 0}
				continue
			}
			speeds[id] = map[string]any{"ul_speed": entry["ul_speed"], "dl_speed": entry["dl_speed"]}
		}
		datasets = append(datasets, map[string]any{"time": label, "speeds": speeds})
	}

	return map[string]any{
		"labels":      labels,
		"datasets":    datasets,
		"downloaders": downloaders,
	}
}

func (s *Service) buildSpeedChartResponse(rows []repository.SpeedRow) map[string]any {
	downloaders := s.enabledDownloaders()

	byTime := map[string]map[string]any{}
	for _, row := range rows {
		if strings.TrimSpace(row.TimeGroup) == "" {
			continue
		}
		entry, ok := byTime[row.TimeGroup]
		if !ok {
			entry = map[string]any{
				"time":   row.TimeGroup,
				"speeds": map[string]any{},
			}
			byTime[row.TimeGroup] = entry
		}
		speeds, ok := entry["speeds"].(map[string]any)
		if !ok {
			speeds = map[string]any{}
			entry["speeds"] = speeds
		}
		speeds[row.DownloaderID] = map[string]any{
			"ul_speed": row.ULSpeed,
			"dl_speed": row.DLSpeed,
		}
	}

	times := make([]string, 0, len(byTime))
	for t := range byTime {
		times = append(times, t)
	}
	sort.Strings(times)

	datasets := make([]map[string]any, 0, len(times))
	labels := make([]string, 0, len(times))
	for _, t := range times {
		labels = append(labels, t)
		if item, ok := byTime[t]; ok {
			datasets = append(datasets, item)
		}
	}

	return map[string]any{
		"labels":      labels,
		"datasets":    datasets,
		"downloaders": downloaders,
	}
}

func resolveTimeRange(rangeKey string, forSpeed bool) (time.Time, time.Time, string) {
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	start := todayStart.AddDate(0, 0, -7)
	end := now
	format := "%Y-%m-%d"

	switch rangeKey {
	case "last_1_hour":
		start = now.Add(-1 * time.Hour)
		format = "%Y-%m-%d %H:%M"
	case "last_6_hours":
		start = now.Add(-6 * time.Hour)
		format = "%Y-%m-%d %H:%M"
	case "last_12_hours":
		start = now.Add(-12 * time.Hour)
		format = "%Y-%m-%d %H:%M"
	case "last_24_hours":
		start = now.Add(-24 * time.Hour)
		format = "%Y-%m-%d %H:00"
	case "today":
		start = todayStart
		if forSpeed {
			format = "%Y-%m-%d %H:%M"
		} else {
			format = "%Y-%m-%d %H:00"
		}
	case "yesterday":
		start = todayStart.AddDate(0, 0, -1)
		end = todayStart
		if forSpeed {
			format = "%Y-%m-%d %H:%M"
		} else {
			format = "%Y-%m-%d %H:00"
		}
	case "this_week":
		start = todayStart.AddDate(0, 0, -int(now.Weekday()+6)%7)
		format = "%Y-%m-%d"
		if forSpeed {
			format = "%Y-%m-%d %H:00"
		}
	case "last_week":
		thisWeekStart := todayStart.AddDate(0, 0, -int(now.Weekday()+6)%7)
		start = thisWeekStart.AddDate(0, 0, -7)
		end = thisWeekStart
		format = "%Y-%m-%d"
		if forSpeed {
			format = "%Y-%m-%d %H:00"
		}
	case "this_month":
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		format = "%Y-%m-%d"
	case "last_month":
		thisMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		start = thisMonthStart.AddDate(0, -1, 0)
		end = thisMonthStart
		format = "%Y-%m-%d"
	case "this_year":
		start = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
		format = "%Y-%m"
	case "all":
		start = time.Date(1970, 1, 1, 0, 0, 0, 0, now.Location())
		format = "%Y-%m"
	default:
		if forSpeed {
			start = now.Add(-12 * time.Hour)
			format = "%Y-%m-%d %H:%M"
		} else {
			start = todayStart.AddDate(0, 0, -7)
			format = "%Y-%m-%d"
		}
	}

	if forSpeed {
		duration := end.Sub(start)
		if duration > 60*24*time.Hour {
			format = "%Y-%m-%d"
		}
		if duration > 365*24*time.Hour {
			format = "%Y-%m"
		}
	}
	return start, end, format
}
