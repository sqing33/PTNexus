package stats

import (
	"sort"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/repository"
)

func (s *Service) GetChartData(rangeKey string) (map[string]any, error) {
	start, end, format := resolveTimeRange(rangeKey, false)
	now := time.Now()
	longPeriodThreshold := now.Add(-48 * time.Hour)

	var rows []repository.TrafficDeltaRow
	if start.Before(longPeriodThreshold) {
		coarseFormat := format
		if strings.Contains(format, "%H") {
			coarseFormat = strings.Split(format, " %H")[0]
		}

		rowsHourly, err := s.repo.QueryTrafficDeltasHourly(start, end, coarseFormat)
		if err != nil {
			return nil, err
		}

		recentThreshold := now.Add(-3 * 24 * time.Hour)
		recentStart := recentThreshold
		if start.After(recentThreshold) {
			recentStart = start
		}

		if recentStart.Before(end) {
			rowsFine, err := s.repo.QueryTrafficDeltas(recentStart, end, format)
			if err != nil {
				return nil, err
			}
			rows = append(rowsHourly, rowsFine...)
		} else {
			rows = rowsHourly
		}
	} else {
		var err error
		rows, err = s.repo.QueryTrafficDeltas(start, end, format)
		if err != nil {
			return nil, err
		}
	}

	downloaders := s.enabledDownloaders()
	enabledSet := map[string]struct{}{}
	for _, item := range downloaders {
		id := toString(item["id"], "")
		if id != "" {
			enabledSet[id] = struct{}{}
		}
	}

	labels := collectLabelsFromTraffic(rows)
	indexMap := map[string]int{}
	for idx, label := range labels {
		indexMap[label] = idx
	}

	datasets := map[string]map[string][]int64{}
	for _, downloader := range downloaders {
		id := toString(downloader["id"], "")
		datasets[id] = map[string][]int64{
			"uploaded":   make([]int64, len(labels)),
			"downloaded": make([]int64, len(labels)),
		}
	}

	for _, row := range rows {
		if _, ok := enabledSet[row.DownloaderID]; !ok {
			continue
		}
		index, ok := indexMap[row.TimeGroup]
		if !ok {
			continue
		}
		if _, ok := datasets[row.DownloaderID]; !ok {
			continue
		}
		datasets[row.DownloaderID]["uploaded"][index] += int64(row.TotalUL)
		datasets[row.DownloaderID]["downloaded"][index] += int64(row.TotalDL)
	}

	resultDatasets := map[string]any{}
	for downloaderID, value := range datasets {
		resultDatasets[downloaderID] = value
	}

	return map[string]any{
		"labels":      labels,
		"datasets":    resultDatasets,
		"downloaders": downloaders,
	}, nil
}

func collectLabelsFromTraffic(rows []repository.TrafficDeltaRow) []string {
	set := map[string]struct{}{}
	for _, row := range rows {
		if row.TimeGroup != "" {
			set[row.TimeGroup] = struct{}{}
		}
	}
	labels := make([]string, 0, len(set))
	for value := range set {
		labels = append(labels, value)
	}
	sort.Strings(labels)
	return labels
}
