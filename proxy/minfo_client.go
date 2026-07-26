package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	minfoBaseURLEnv = "PTNEXUS_MINFO_URL"
	minfoMaxBody    = 16 << 20
)

type minfoScreenshotLink struct {
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnail_url"`
	Filename     string `json:"filename"`
	Size         int64  `json:"size"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
}

type minfoScreenshotResponse struct {
	OK        bool                  `json:"ok"`
	LinkItems []minfoScreenshotLink `json:"link_items"`
	Logs      string                `json:"logs"`
	Error     string                `json:"error"`
}

func isMInfoConfigured() bool {
	return strings.TrimSpace(os.Getenv(minfoBaseURLEnv)) != ""
}

func resolveMInfoScreenshotEndpoint() (string, error) {
	configured := strings.TrimSpace(os.Getenv(minfoBaseURLEnv))
	if configured == "" {
		return "", fmt.Errorf("%s is not configured", minfoBaseURLEnv)
	}

	parsed, err := url.Parse(configured)
	if err != nil {
		return "", fmt.Errorf("invalid %s: %w", minfoBaseURLEnv, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("invalid %s scheme %q: only http and https are supported", minfoBaseURLEnv, parsed.Scheme)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("invalid %s: host is empty", minfoBaseURLEnv)
	}

	parsed.RawQuery = ""
	parsed.Fragment = ""
	if !strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/api/screenshots") {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/screenshots"
	}
	return parsed.String(), nil
}

func sanitizeMInfoScreenshotTimes(values []float64) []float64 {
	clean := make([]float64, 0, len(values))
	for _, value := range values {
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		duplicate := false
		for _, existing := range clean {
			if math.Abs(existing-value) < 0.8 {
				duplicate = true
				break
			}
		}
		if !duplicate {
			clean = append(clean, value)
		}
	}
	sort.Float64s(clean)
	return clean
}

func requestScreenshotsFromMInfo(ctx context.Context, remotePath string, timestamps []float64) ([]minfoScreenshotLink, error) {
	endpoint, err := resolveMInfoScreenshotEndpoint()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(remotePath) == "" {
		return nil, fmt.Errorf("MInfo media path is empty")
	}
	if len(timestamps) == 0 {
		return nil, fmt.Errorf("MInfo screenshot timestamps are empty")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := map[string]string{
		"path":          remotePath,
		"mode":          "links",
		"variant":       "jpg",
		"subtitle_mode": "auto",
		"hdr_processor": "libplacebo",
		"count":         strconv.Itoa(len(timestamps)),
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, fmt.Errorf("failed to build MInfo field %s: %w", key, err)
		}
	}
	for _, timestamp := range timestamps {
		if err := writer.WriteField("timestamp", strconv.FormatFloat(timestamp, 'f', 3, 64)); err != nil {
			return nil, fmt.Errorf("failed to build MInfo timestamp: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to finish MInfo request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, fmt.Errorf("failed to create MInfo request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "pt-nexus-box-proxy")

	client := &http.Client{Timeout: 20 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MInfo request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, minfoMaxBody))
	if err != nil {
		return nil, fmt.Errorf("failed to read MInfo response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("MInfo HTTP %d: %s", resp.StatusCode, compactResponseBody(string(responseBody)))
	}

	parsed := minfoScreenshotResponse{}
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse MInfo response: %w body=%s", err, compactResponseBody(string(responseBody)))
	}
	if !parsed.OK {
		detail := strings.TrimSpace(parsed.Error)
		if detail == "" {
			detail = compactResponseBody(parsed.Logs)
		}
		if detail == "" {
			detail = "MInfo returned ok=false"
		}
		return nil, fmt.Errorf("%s", detail)
	}

	links := make([]minfoScreenshotLink, 0, len(parsed.LinkItems))
	seen := make(map[string]struct{}, len(parsed.LinkItems))
	for _, item := range parsed.LinkItems {
		item.URL = strings.TrimSpace(item.URL)
		if item.URL == "" {
			continue
		}
		if _, exists := seen[item.URL]; exists {
			continue
		}
		seen[item.URL] = struct{}{}
		links = append(links, item)
	}
	if len(links) == 0 {
		return nil, fmt.Errorf("MInfo response did not include usable image links")
	}
	return links, nil
}

func buildScreenshotBBCode(urls []string) string {
	var builder strings.Builder
	for _, imageURL := range urls {
		trimmedURL := strings.TrimSpace(imageURL)
		if trimmedURL == "" {
			continue
		}
		builder.WriteString(fmt.Sprintf("[img]%s[/img]\n", trimmedURL))
	}
	return strings.TrimSpace(builder.String())
}
