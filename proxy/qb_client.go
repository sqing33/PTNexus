package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

func newQBHTTPClient(baseURL string) (*qbHTTPClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return nil, fmt.Errorf("downloader host is empty")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "http://" + trimmed
	}
	trimmed = strings.TrimRight(trimmed, "/")

	return &qbHTTPClient{
		Client:  &http.Client{Jar: jar, Timeout: 180 * time.Second},
		BaseURL: trimmed,
	}, nil
}

func (c *qbHTTPClient) apiURL(endpoint string, params url.Values) string {
	endpoint = strings.TrimLeft(strings.TrimSpace(endpoint), "/")
	fullURL := c.BaseURL + "/api/v2/" + endpoint
	if params != nil && len(params) > 0 {
		fullURL += "?" + params.Encode()
	}
	return fullURL
}

func (c *qbHTTPClient) Login(username, password string) error {
	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)

	req, err := http.NewRequest(http.MethodPost, c.apiURL("auth/login", nil), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("qB login failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if !strings.Contains(strings.ToLower(string(body)), "ok") {
		return fmt.Errorf("qB login failed: %s", strings.TrimSpace(string(body)))
	}

	c.IsLoggedIn = true
	return nil
}

func (c *qbHTTPClient) Get(endpoint string, params url.Values) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.apiURL(endpoint, params), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("qB GET %s failed: HTTP %d %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (c *qbHTTPClient) PostForm(endpoint string, data url.Values) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, c.apiURL(endpoint, nil), strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("qB POST %s failed: HTTP %d %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func formatTrackersForRaw(trackers []FlexibleTracker) []map[string]string {
	result := make([]map[string]string, 0, len(trackers))
	for _, tracker := range trackers {
		result = append(result, map[string]string{
			"url":         strings.TrimSpace(tracker.URL),
			"status":      fmt.Sprintf("%d", tracker.Status),
			"tier":        strings.TrimSpace(fmt.Sprint(tracker.Tier)),
			"num_peers":   fmt.Sprintf("%d", tracker.NumPeers),
			"num_seeds":   fmt.Sprintf("%d", tracker.NumSeeds),
			"num_leeches": fmt.Sprintf("%d", tracker.NumLeeches),
			"msg":         strings.TrimSpace(tracker.Msg),
		})
	}
	return result
}

func fetchTorrentsForDownloader(wg *sync.WaitGroup, config DownloaderConfig, includeComment, includeTrackers bool, resultsChan chan<- []NormalizedTorrent, errChan chan<- error) {
	defer wg.Done()
	if config.Type != "qbittorrent" {
		resultsChan <- []NormalizedTorrent{}
		return
	}

	client, err := newQBHTTPClient(config.Host)
	if err != nil {
		errChan <- fmt.Errorf("[%s] create client failed: %v", config.Host, err)
		return
	}
	if err := client.Login(config.Username, config.Password); err != nil {
		errChan <- fmt.Errorf("[%s] login failed: %v", config.Host, err)
		return
	}

	body, err := client.Get("torrents/info", nil)
	if err != nil {
		errChan <- fmt.Errorf("[%s] fetch torrents failed: %v", config.Host, err)
		return
	}

	var torrents []QBTorrentInfo
	if err := json.Unmarshal(body, &torrents); err != nil {
		errChan <- fmt.Errorf("[%s] parse torrents failed: %v", config.Host, err)
		return
	}

	normalizedList := make([]NormalizedTorrent, 0, len(torrents))
	var totalUploaded int64
	var totalDownloaded int64
	for _, torrent := range torrents {
		downloaded := torrent.Downloaded
		if downloaded <= 0 && torrent.Size > 0 && torrent.Progress > 0 {
			downloaded = int64(float64(torrent.Size) * torrent.Progress)
		}
		totalUploaded += torrent.Uploaded
		totalDownloaded += downloaded

		ratio := torrent.Ratio
		if ratio <= 0 && downloaded > 0 {
			ratio = float64(torrent.Uploaded) / float64(downloaded)
		}

		normalizedList = append(normalizedList, NormalizedTorrent{
			Hash:         torrent.Hash,
			Name:         torrent.Name,
			Size:         torrent.Size,
			Progress:     torrent.Progress,
			State:        torrent.State,
			SavePath:     torrent.SavePath,
			Uploaded:     torrent.Uploaded,
			Downloaded:   downloaded,
			Ratio:        ratio,
			DownloaderID: config.ID,
		})
	}

	if includeComment || includeTrackers {
		for i := range normalizedList {
			item := &normalizedList[i]
			params := url.Values{}
			params.Set("hash", item.Hash)

			if includeComment {
				body, err := client.Get("torrents/properties", params)
				if err == nil {
					var props struct {
						Comment string `json:"comment"`
					}
					if json.Unmarshal(body, &props) == nil {
						item.Comment = props.Comment
					}
				}
			}

			if includeTrackers {
				body, err := client.Get("torrents/trackers", params)
				if err == nil {
					var trackers []FlexibleTracker
					if json.Unmarshal(body, &trackers) == nil {
						item.Trackers = formatTrackersForRaw(trackers)
					}
				}
			}
		}
	}

	log.Printf("fetched %d torrents from %s (uploaded=%.2f GB downloaded=%.2f GB)", len(normalizedList), config.Host, float64(totalUploaded)/1024/1024/1024, float64(totalDownloaded)/1024/1024/1024)
	resultsChan <- normalizedList
}

func fetchServerStatsForDownloader(wg *sync.WaitGroup, config DownloaderConfig, resultsChan chan<- ServerStats, errChan chan<- error) {
	defer wg.Done()
	if config.Type != "qbittorrent" {
		resultsChan <- ServerStats{DownloaderID: config.ID}
		return
	}

	client, err := newQBHTTPClient(config.Host)
	if err != nil {
		errChan <- fmt.Errorf("[%s] create client failed: %v", config.Host, err)
		return
	}
	if err := client.Login(config.Username, config.Password); err != nil {
		errChan <- fmt.Errorf("[%s] login failed: %v", config.Host, err)
		return
	}

	body, err := client.Get("sync/maindata", nil)
	if err != nil {
		errChan <- fmt.Errorf("[%s] fetch stats failed: %v", config.Host, err)
		return
	}

	var mainData struct {
		ServerState struct {
			DlInfoSpeed int64 `json:"dl_info_speed"`
			UpInfoSpeed int64 `json:"up_info_speed"`
			AlltimeDL   int64 `json:"alltime_dl"`
			AlltimeUL   int64 `json:"alltime_ul"`
		} `json:"server_state"`
	}
	if err := json.Unmarshal(body, &mainData); err != nil {
		errChan <- fmt.Errorf("[%s] parse stats failed: %v", config.Host, err)
		return
	}

	version := ""
	if versionBody, err := client.Get("app/version", nil); err == nil {
		version = strings.TrimSpace(string(versionBody))
	}

	resultsChan <- ServerStats{
		DownloaderID:  config.ID,
		DownloadSpeed: mainData.ServerState.DlInfoSpeed,
		UploadSpeed:   mainData.ServerState.UpInfoSpeed,
		TotalDownload: mainData.ServerState.AlltimeDL,
		TotalUpload:   mainData.ServerState.AlltimeUL,
		Version:       version,
	}
}

func allTorrentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, map[string]any{"success": false, "message": "only POST is supported"})
		return
	}

	var req TorrentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, map[string]any{"success": false, "message": "invalid JSON: " + err.Error()})
		return
	}

	var wg sync.WaitGroup
	resultsChan := make(chan []NormalizedTorrent, len(req.Downloaders))
	errChan := make(chan error, len(req.Downloaders))

	for _, downloader := range req.Downloaders {
		wg.Add(1)
		go fetchTorrentsForDownloader(&wg, downloader, req.IncludeComment, req.IncludeTrackers, resultsChan, errChan)
	}
	wg.Wait()
	close(resultsChan)
	close(errChan)

	for err := range errChan {
		if err != nil {
			writeJSONResponse(w, r, http.StatusBadGateway, map[string]any{"success": false, "message": err.Error()})
			return
		}
	}

	items := make([]NormalizedInfo, 0)
	for list := range resultsChan {
		for _, item := range list {
			items = append(items, NormalizedInfo{
				Hash:         item.Hash,
				Name:         item.Name,
				Size:         item.Size,
				Progress:     item.Progress,
				State:        item.State,
				SavePath:     item.SavePath,
				Comment:      item.Comment,
				Trackers:     item.Trackers,
				Uploaded:     item.Uploaded,
				Downloaded:   item.Downloaded,
				Ratio:        item.Ratio,
				DownloaderID: item.DownloaderID,
			})
		}
	}

	writeJSONResponse(w, r, http.StatusOK, items)
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, map[string]any{"success": false, "message": "only POST is supported"})
		return
	}

	var req struct {
		Downloaders []DownloaderConfig `json:"downloaders"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, map[string]any{"success": false, "message": "failed to read request body: " + err.Error()})
		return
	}

	trimmedBody := bytes.TrimSpace(body)
	if len(trimmedBody) == 0 {
		writeJSONResponse(w, r, http.StatusBadRequest, map[string]any{"success": false, "message": "invalid JSON: empty request body"})
		return
	}

	if trimmedBody[0] == '[' {
		if err := json.Unmarshal(trimmedBody, &req.Downloaders); err != nil {
			writeJSONResponse(w, r, http.StatusBadRequest, map[string]any{"success": false, "message": "invalid JSON: " + err.Error()})
			return
		}
	} else if err := json.Unmarshal(trimmedBody, &req); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, map[string]any{"success": false, "message": "invalid JSON: " + err.Error()})
		return
	}

	var wg sync.WaitGroup
	resultsChan := make(chan ServerStats, len(req.Downloaders))
	errChan := make(chan error, len(req.Downloaders))

	for _, downloader := range req.Downloaders {
		wg.Add(1)
		go fetchServerStatsForDownloader(&wg, downloader, resultsChan, errChan)
	}
	wg.Wait()
	close(resultsChan)
	close(errChan)

	for err := range errChan {
		if err != nil {
			writeJSONResponse(w, r, http.StatusBadGateway, map[string]any{"success": false, "message": err.Error()})
			return
		}
	}

	stats := make([]ServerStats, 0, len(req.Downloaders))
	for item := range resultsChan {
		stats = append(stats, item)
	}
	writeJSONResponse(w, r, http.StatusOK, stats)
}

func uploadLimitBatchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, UploadLimitBatchResponse{Success: false})
		return
	}

	var req UploadLimitBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, map[string]any{"success": false, "message": "invalid JSON: " + err.Error()})
		return
	}

	results := make([]UploadLimitResult, 0, len(req.Downloaders))
	for _, downloader := range req.Downloaders {
		result := UploadLimitResult{DownloaderID: downloader.ID}
		if downloader.Type != "qbittorrent" {
			result.Errors = append(result.Errors, fmt.Sprintf("unsupported downloader type: %s", downloader.Type))
			results = append(results, result)
			continue
		}

		client, err := newQBHTTPClient(downloader.Host)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			results = append(results, result)
			continue
		}
		if err := client.Login(downloader.Username, downloader.Password); err != nil {
			result.Errors = append(result.Errors, err.Error())
			results = append(results, result)
			continue
		}

		for _, action := range downloader.Actions {
			hashes := make([]string, 0, len(action.TorrentIDs))
			for _, hash := range action.TorrentIDs {
				hash = strings.TrimSpace(hash)
				if hash != "" {
					hashes = append(hashes, hash)
				}
			}
			if len(hashes) == 0 {
				continue
			}

			form := url.Values{}
			form.Set("hashes", strings.Join(hashes, "|"))
			form.Set("limit", fmt.Sprintf("%d", action.LimitMBps*1024*1024))
			if _, err := client.PostForm("torrents/setUploadLimit", form); err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}

			result.AppliedGroups++
			result.AppliedTorrents += len(hashes)
		}

		results = append(results, result)
	}

	writeJSONResponse(w, r, http.StatusOK, UploadLimitBatchResponse{
		Success: true,
		Results: results,
	})
}
