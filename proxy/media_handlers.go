package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func screenshotHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, ScreenshotResponse{Success: false, Message: "only POST is supported"})
		return
	}

	var reqData ScreenshotRequest
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, ScreenshotResponse{Success: false, Message: "invalid JSON request body: " + err.Error()})
		return
	}

	initialPath := normalizePath(reqData.RemotePath)
	if initialPath == "" {
		writeJSONResponse(w, r, http.StatusBadRequest, ScreenshotResponse{Success: false, Message: "remote_path cannot be empty"})
		return
	}

	statusCode := http.StatusOK
	response := ScreenshotResponse{}
	mode := strings.ToLower(strings.TrimSpace(reqData.Mode))
	selectedSubtitleSID := 0
	if reqData.SelectedSubtitleSID != nil {
		selectedSubtitleSID = *reqData.SelectedSubtitleSID
	}
	log.Printf("screenshot request received: mode=%s remote_path=%s content_name=%q preview_count=%d selected_times=%d selected_subtitle_sid=%d", mode, initialPath, reqData.ContentName, reqData.PreviewCount, len(reqData.SelectedTimes), selectedSubtitleSID)

	var minfoFallbackErr error
	if mode == "finalize" && isMInfoConfigured() {
		screenshotPoints := sanitizeMInfoScreenshotTimes(reqData.SelectedTimes)
		if len(screenshotPoints) == 0 {
			writeJSONResponse(w, r, http.StatusBadRequest, ScreenshotResponse{Success: false, Message: "selected_times must contain at least one valid timestamp"})
			return
		}

		startedAt := time.Now()
		log.Printf("MInfo screenshot started: remote_path=%s timestamps=%d variant=jpg hdr_processor=libplacebo subtitle_mode=auto", initialPath, len(screenshotPoints))
		if selectedSubtitleSID > 0 {
			log.Printf("MInfo controls subtitle selection automatically; selected_subtitle_sid=%d is reserved for local fallback", selectedSubtitleSID)
		}
		links, minfoErr := requestScreenshotsFromMInfo(r.Context(), initialPath, screenshotPoints)
		if minfoErr == nil {
			urls := make([]string, 0, len(links))
			for index, item := range links {
				log.Printf("MInfo screenshot item: index=%d filename=%q size=%d width=%d height=%d", index+1, item.Filename, item.Size, item.Width, item.Height)
				urls = append(urls, item.URL)
			}
			log.Printf("MInfo screenshot succeeded: remote_path=%s uploaded=%d requested=%d elapsed_ms=%d", initialPath, len(urls), len(screenshotPoints), time.Since(startedAt).Milliseconds())
			writeJSONResponse(w, r, http.StatusOK, ScreenshotResponse{
				Success: true,
				Message: fmt.Sprintf("MInfo uploaded %d/%d screenshots", len(urls), len(screenshotPoints)),
				BBCode:  buildScreenshotBBCode(urls),
			})
			return
		}

		minfoFallbackErr = minfoErr
		log.Printf("MInfo screenshot failed; falling back to local pipeline: remote_path=%s elapsed_ms=%d err=%v", initialPath, time.Since(startedAt).Milliseconds(), minfoErr)
	}

	err := withMountedISOIfNeeded(initialPath, "screenshot request", func(resolvedPath string) error {
		videoPath, err := findTargetVideoFile(resolvedPath, reqData.ContentName)
		if err != nil {
			statusCode = http.StatusBadRequest
			response = ScreenshotResponse{Success: false, Message: err.Error()}
			return err
		}
		duration, err := getVideoDuration(videoPath)
		if err != nil {
			statusCode = http.StatusInternalServerError
			response = ScreenshotResponse{Success: false, Message: "failed to get video duration: " + err.Error()}
			return err
		}
		log.Printf("screenshot target resolved: mode=%s video=%s duration=%.3fs", mode, videoPath, duration)

		if mode == "preview" {
			inspection, selectedCandidate, hasSelectedCandidate, currentSubtitleSID, inspectErr := resolveSubtitleCandidate(videoPath, reqData.SelectedSubtitleSID)
			if inspectErr != nil {
				statusCode = http.StatusInternalServerError
				response = ScreenshotResponse{Success: false, Message: inspectErr.Error()}
				return inspectErr
			}

			candidates, previewErr := generatePreviewCandidates(videoPath, duration, reqData.PreviewCount, currentSubtitleSID, selectedCandidate, hasSelectedCandidate)
			if previewErr != nil {
				statusCode = http.StatusInternalServerError
				response = ScreenshotResponse{Success: false, Message: previewErr.Error()}
				return previewErr
			}

			response = ScreenshotResponse{
				Success:            true,
				Message:            fmt.Sprintf("generated %d preview candidates", len(candidates)),
				SubtitleState:      string(inspection.State),
				SubtitleStreams:    inspection.Streams,
				CurrentSubtitleSID: currentSubtitleSID,
				PreviewCandidates:  candidates,
			}
			return nil
		}

		if mode == "inspect" {
			inspection, inspectErr := inspectSubtitleStreams(videoPath)
			if inspectErr != nil {
				statusCode = http.StatusInternalServerError
				response = ScreenshotResponse{Success: false, Message: inspectErr.Error()}
				return inspectErr
			}

			message := "no usable subtitle stream detected"
			switch inspection.State {
			case ScreenshotSubtitleStateConfirmedChinese:
				message = "confirmed Chinese subtitle stream detected"
			case ScreenshotSubtitleStateUsableButUnconfirmed:
				message = "usable subtitle stream detected, but language is not confirmed"
			}

			response = ScreenshotResponse{
				Success:            true,
				Message:            message,
				SubtitleState:      string(inspection.State),
				SubtitleStreams:    inspection.Streams,
				CurrentSubtitleSID: inspection.CurrentSubtitleSID,
			}
			return nil
		}

		inspection, selectedCandidate, hasSelectedCandidate, subtitleSID, inspectErr := resolveSubtitleCandidate(videoPath, reqData.SelectedSubtitleSID)
		if inspectErr != nil {
			statusCode = http.StatusInternalServerError
			response = ScreenshotResponse{Success: false, Message: inspectErr.Error()}
			return inspectErr
		}

		switch {
		case subtitleSID <= 0:
			log.Printf("capturing screenshots without subtitles")
		case reqData.SelectedSubtitleSID != nil && hasSelectedCandidate:
			log.Printf("capturing screenshots with user-selected subtitle stream sid=%d title=%s", subtitleSID, selectedCandidate.Title)
		case inspection.State == ScreenshotSubtitleStateConfirmedChinese:
			log.Printf("capturing screenshots with confirmed Chinese subtitle stream sid=%d", subtitleSID)
		default:
			log.Printf("capturing screenshots with usable subtitle stream sid=%d", subtitleSID)
		}

		screenshotPoints := make([]float64, 0, 5)
		const numScreenshots = 5
		if mode == "finalize" {
			screenshotPoints = sanitizeSelectedScreenshotTimes(reqData.SelectedTimes, duration)
			if len(screenshotPoints) == 0 {
				statusCode = http.StatusBadRequest
				response = ScreenshotResponse{Success: false, Message: "selected_times must contain at least one valid timestamp"}
				return fmt.Errorf("selected_times cannot be empty")
			}
		} else {
			screenshotPoints = buildSmartScreenshotPointsForPreview(videoPath, duration, numScreenshots, subtitleSID, selectedCandidate, hasSelectedCandidate)
			if len(screenshotPoints) < numScreenshots {
				log.Printf("smart screenshot points were insufficient; falling back to uniform percentages")
				percentages := []float64{0.15, 0.30, 0.50, 0.70, 0.85}
				screenshotPoints = make([]float64, 0, len(percentages))
				for _, p := range percentages {
					screenshotPoints = append(screenshotPoints, duration*p)
				}
			}
		}

		if mode != "finalize" && isMInfoConfigured() {
			startedAt := time.Now()
			log.Printf("MInfo automatic screenshot started: remote_path=%s timestamps=%d variant=jpg hdr_processor=libplacebo subtitle_mode=auto", initialPath, len(screenshotPoints))
			if selectedSubtitleSID > 0 {
				log.Printf("MInfo controls subtitle selection automatically; selected_subtitle_sid=%d is reserved for local fallback", selectedSubtitleSID)
			}
			links, minfoErr := requestScreenshotsFromMInfo(r.Context(), initialPath, screenshotPoints)
			if minfoErr == nil {
				urls := make([]string, 0, len(links))
				for index, item := range links {
					log.Printf("MInfo screenshot item: index=%d filename=%q size=%d width=%d height=%d", index+1, item.Filename, item.Size, item.Width, item.Height)
					urls = append(urls, item.URL)
				}
				log.Printf("MInfo automatic screenshot succeeded: remote_path=%s uploaded=%d requested=%d elapsed_ms=%d", initialPath, len(urls), len(screenshotPoints), time.Since(startedAt).Milliseconds())
				response = ScreenshotResponse{
					Success: true,
					Message: fmt.Sprintf("MInfo uploaded %d/%d screenshots", len(urls), len(screenshotPoints)),
					BBCode:  buildScreenshotBBCode(urls),
				}
				return nil
			}

			minfoFallbackErr = minfoErr
			log.Printf("MInfo automatic screenshot failed; falling back to local pipeline: remote_path=%s elapsed_ms=%d err=%v", initialPath, time.Since(startedAt).Milliseconds(), minfoErr)
		}

		tempDir, err := os.MkdirTemp("", "screenshots-*")
		if err != nil {
			statusCode = http.StatusInternalServerError
			response = ScreenshotResponse{Success: false, Message: "failed to create temp directory: " + err.Error()}
			return err
		}
		defer os.RemoveAll(tempDir)

		videoHDR := detectHDRFromVideo(videoPath)
		contentHDR := hasHDRKeyword(reqData.ContentName)
		pathHDR := hasHDRKeyword(reqData.RemotePath)
		videoIsHDR := videoHDR || contentHDR || pathHDR
		log.Printf(
			"screenshot source HDR detection: video=%s hdr=%t video_metadata=%t content_keyword=%t path_keyword=%t content_name=%q remote_path=%q",
			filepath.Base(videoPath),
			videoIsHDR,
			videoHDR,
			contentHDR,
			pathHDR,
			reqData.ContentName,
			reqData.RemotePath,
		)

		uploadedURLs := make([]string, 0, len(screenshotPoints))
		for i, point := range screenshotPoints {
			totalSeconds := int(point)
			hours, minutes, seconds := totalSeconds/3600, (totalSeconds%3600)/60, totalSeconds%60
			timeStr := fmt.Sprintf("%02dh%02dm%02ds", hours, minutes, seconds)
			fileName := fmt.Sprintf("s%d_%s.png", i+1, timeStr)
			intermediatePngPath := filepath.Join(tempDir, "raw_"+fileName)
			finalPngPath := filepath.Join(tempDir, fileName)

			if err := takeScreenshot(videoPath, intermediatePngPath, point, subtitleSID); err != nil {
				log.Printf("screenshot %d failed during capture: %v", i+1, err)
				continue
			}
			finalImagePath, err := convertPngToOptimizedImage(intermediatePngPath, finalPngPath, videoIsHDR)
			if err != nil {
				log.Printf("screenshot %d failed during image optimization: %v", i+1, err)
				continue
			}
			log.Printf("screenshot image optimized: index=%d hdr=%t output=%s size_mb=%.2f", i+1, videoIsHDR, finalImagePath, fileSizeMB(finalImagePath))

			uploadPath, err := preparePixhostUploadImage(finalImagePath)
			if err != nil {
				log.Printf("screenshot %d failed during upload image preparation: %v", i+1, err)
				continue
			}
			log.Printf("screenshot upload prepared: index=%d path=%s size_mb=%.2f", i+1, uploadPath, fileSizeMB(uploadPath))
			showURL, err := uploadToPixhost(uploadPath)
			if err != nil {
				log.Printf("screenshot %d failed during upload: %v", i+1, err)
				continue
			}

			directURL := normalizePixhostShowURL(showURL)
			log.Printf("screenshot upload succeeded: index=%d source=%s direct_url=%s", i+1, uploadPath, directURL)
			uploadedURLs = append(uploadedURLs, directURL)
		}

		if len(uploadedURLs) == 0 {
			msg := "all screenshots failed to process"
			statusCode = http.StatusInternalServerError
			response = ScreenshotResponse{Success: false, Message: msg}
			return fmt.Errorf("%s", msg)
		}

		response = ScreenshotResponse{
			Success: true,
			Message: fmt.Sprintf("uploaded %d/%d screenshots", len(uploadedURLs), len(screenshotPoints)),
			BBCode:  buildScreenshotBBCode(uploadedURLs),
		}
		return nil
	})
	if err != nil {
		if statusCode == http.StatusOK || response.Success {
			statusCode = http.StatusInternalServerError
			response = ScreenshotResponse{Success: false, Message: err.Error()}
		}
		if minfoFallbackErr != nil {
			response.Message = fmt.Sprintf("MInfo screenshot failed (%v); local fallback failed (%s)", minfoFallbackErr, response.Message)
		}
		writeJSONResponse(w, r, statusCode, response)
		return
	}

	writeJSONResponse(w, r, statusCode, response)
}

func normalizePixhostShowURL(showURL string) string {
	directURL := strings.TrimSpace(showURL)
	for _, from := range []string{
		"https://pixhost.to/show/",
		"https://pixhost.to/th/",
		"http://pixhost.to/show/",
		"http://pixhost.to/th/",
		"https://pixhost.cc/show/",
		"https://pixhost.cc/th/",
		"http://pixhost.cc/show/",
		"http://pixhost.cc/th/",
	} {
		directURL = strings.Replace(directURL, from, "https://img2.pixhost.cc/images/", 1)
	}
	return directURL
}

func mediainfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, MediaInfoResponse{Success: false, Message: "only POST is supported"})
		return
	}

	var reqData MediaInfoRequest
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, MediaInfoResponse{Success: false, Message: "invalid JSON request body: " + err.Error()})
		return
	}

	initialPath := normalizePath(reqData.RemotePath)
	if initialPath == "" {
		writeJSONResponse(w, r, http.StatusBadRequest, MediaInfoResponse{Success: false, Message: "remote_path cannot be empty"})
		return
	}

	statusCode := http.StatusOK
	response := MediaInfoResponse{}
	err := withMountedISOIfNeeded(initialPath, "mediainfo request", func(resolvedPath string) error {
		if isBlurayDisc(resolvedPath) {
			response = MediaInfoResponse{
				Success: true,
				Message: "bluray directory detected",
				IsBDMV:  true,
			}
			return nil
		}

		videoPath, innerErr := findTargetVideoFile(resolvedPath, reqData.ContentName)
		if innerErr != nil {
			statusCode = http.StatusBadRequest
			response = MediaInfoResponse{Success: false, Message: innerErr.Error()}
			return innerErr
		}

		mediaInfoText, innerErr := extractMediaInfo(videoPath)
		if innerErr != nil {
			statusCode = http.StatusInternalServerError
			response = MediaInfoResponse{Success: false, Message: "failed to extract media info: " + innerErr.Error()}
			return innerErr
		}

		response = MediaInfoResponse{
			Success:   true,
			Message:   "media info extracted",
			MediaInfo: strings.TrimSpace(mediaInfoText),
		}
		return nil
	})
	if err != nil {
		if statusCode == http.StatusOK || response.Success {
			statusCode = http.StatusInternalServerError
			response = MediaInfoResponse{Success: false, Message: err.Error()}
		}
		writeJSONResponse(w, r, statusCode, response)
		return
	}

	writeJSONResponse(w, r, statusCode, response)
}

func fileCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, FileCheckResponse{Success: false, Message: "only POST is supported"})
		return
	}

	var reqData FileCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, FileCheckResponse{Success: false, Message: "invalid JSON request body: " + err.Error()})
		return
	}

	remotePath := normalizePath(reqData.RemotePath)
	if remotePath == "" {
		writeJSONResponse(w, r, http.StatusBadRequest, FileCheckResponse{Success: false, Message: "remote_path cannot be empty"})
		return
	}

	fileInfo, err := os.Stat(remotePath)
	if os.IsNotExist(err) {
		writeJSONResponse(w, r, http.StatusOK, FileCheckResponse{
			Success: true,
			Message: "path checked",
			Exists:  false,
		})
		return
	}
	if err != nil {
		writeJSONResponse(w, r, http.StatusInternalServerError, FileCheckResponse{
			Success: false,
			Message: "failed to stat path: " + err.Error(),
		})
		return
	}

	writeJSONResponse(w, r, http.StatusOK, FileCheckResponse{
		Success: true,
		Message: "path checked",
		Exists:  true,
		IsFile:  !fileInfo.IsDir(),
		Size:    fileInfo.Size(),
	})
}

func batchFileCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, BatchFileCheckResponse{Success: false, Message: "only POST is supported"})
		return
	}

	var reqData BatchFileCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, BatchFileCheckResponse{Success: false, Message: "invalid JSON request body: " + err.Error()})
		return
	}
	if len(reqData.RemotePaths) == 0 {
		writeJSONResponse(w, r, http.StatusBadRequest, BatchFileCheckResponse{Success: false, Message: "remote_paths cannot be empty"})
		return
	}

	results := make([]FileCheckResult, 0, len(reqData.RemotePaths))
	for _, rawPath := range reqData.RemotePaths {
		remotePath := normalizePath(rawPath)
		result := FileCheckResult{Path: remotePath}

		fileInfo, err := os.Stat(remotePath)
		if os.IsNotExist(err) {
			results = append(results, result)
			continue
		}
		if err != nil {
			results = append(results, result)
			continue
		}

		result.Exists = true
		result.IsFile = !fileInfo.IsDir()
		result.Size = fileInfo.Size()
		results = append(results, result)
	}

	writeJSONResponse(w, r, http.StatusOK, BatchFileCheckResponse{
		Success: true,
		Message: "batch check complete",
		Results: results,
	})
}

func episodeCountHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONResponse(w, r, http.StatusMethodNotAllowed, EpisodeCountResponse{Success: false, Message: "only POST is supported"})
		return
	}

	var reqData EpisodeCountRequest
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		writeJSONResponse(w, r, http.StatusBadRequest, EpisodeCountResponse{Success: false, Message: "invalid JSON request body: " + err.Error()})
		return
	}

	remotePath := normalizePath(reqData.RemotePath)
	if remotePath == "" {
		writeJSONResponse(w, r, http.StatusBadRequest, EpisodeCountResponse{Success: false, Message: "remote_path cannot be empty"})
		return
	}
	if _, err := os.Stat(remotePath); os.IsNotExist(err) {
		writeJSONResponse(w, r, http.StatusOK, EpisodeCountResponse{Success: false, Message: "path does not exist"})
		return
	}

	videoExtensions := map[string]bool{
		".mkv": true, ".mp4": true, ".ts": true, ".avi": true,
		".wmv": true, ".mov": true, ".flv": true, ".m2ts": true,
	}
	episodePattern := regexp.MustCompile(`[Ss](\d{1,2})[Ee](\d{1,3})`)
	episodeSet := make(map[string]bool)
	seasonNumbers := make(map[int]bool)

	err := filepath.Walk(remotePath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !videoExtensions[strings.ToLower(filepath.Ext(info.Name()))] {
			return nil
		}

		matches := episodePattern.FindStringSubmatch(info.Name())
		if len(matches) >= 3 {
			season, _ := strconv.Atoi(matches[1])
			episode, _ := strconv.Atoi(matches[2])
			key := fmt.Sprintf("S%dE%d", season, episode)
			episodeSet[key] = true
			seasonNumbers[season] = true
		}
		return nil
	})
	if err != nil {
		writeJSONResponse(w, r, http.StatusInternalServerError, EpisodeCountResponse{Success: false, Message: "failed to scan directory: " + err.Error()})
		return
	}

	if len(episodeSet) == 0 {
		writeJSONResponse(w, r, http.StatusOK, EpisodeCountResponse{
			Success:      true,
			Message:      "no episodic files found",
			EpisodeCount: 0,
		})
		return
	}

	mainSeason := 0
	for season := range seasonNumbers {
		if mainSeason == 0 || season < mainSeason {
			mainSeason = season
		}
	}

	seasonEpisodeCount := 0
	for key := range episodeSet {
		if strings.HasPrefix(key, fmt.Sprintf("S%d", mainSeason)) {
			seasonEpisodeCount++
		}
	}

	writeJSONResponse(w, r, http.StatusOK, EpisodeCountResponse{
		Success:      true,
		Message:      "episode count complete",
		EpisodeCount: seasonEpisodeCount,
		SeasonNumber: mainSeason,
	})
}
