package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type DownloaderConfig struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type NormalizedTorrent struct {
	Hash         string
	Name         string
	Size         int64
	Progress     float64
	State        string
	SavePath     string
	Comment      string
	Trackers     []map[string]string
	Uploaded     int64
	Downloaded   int64
	Ratio        float64
	DownloaderID string
}

type NormalizedInfo struct {
	Hash         string              `json:"hash"`
	Name         string              `json:"name"`
	Size         int64               `json:"size"`
	Progress     float64             `json:"progress"`
	State        string              `json:"state"`
	SavePath     string              `json:"save_path"`
	Comment      string              `json:"comment,omitempty"`
	Trackers     []map[string]string `json:"trackers"`
	Uploaded     int64               `json:"uploaded"`
	Downloaded   int64               `json:"downloaded"`
	Ratio        float64             `json:"ratio"`
	DownloaderID string              `json:"downloader_id"`
}

type TorrentsRequest struct {
	Downloaders     []DownloaderConfig `json:"downloaders"`
	IncludeComment  bool               `json:"include_comment,omitempty"`
	IncludeTrackers bool               `json:"include_trackers,omitempty"`
}

type DeleteTorrentsRequest struct {
	Downloader  DownloaderConfig `json:"downloader"`
	Hashes      []string         `json:"hashes"`
	DeleteFiles bool             `json:"delete_files"`
}

type ServerStats struct {
	DownloaderID  string `json:"downloader_id"`
	DownloadSpeed int64  `json:"download_speed"`
	UploadSpeed   int64  `json:"upload_speed"`
	TotalDownload int64  `json:"total_download"`
	TotalUpload   int64  `json:"total_upload"`
	Version       string `json:"version,omitempty"`
}

type FlexibleTracker struct {
	URL        string      `json:"url"`
	Status     int         `json:"status"`
	Tier       interface{} `json:"tier"`
	NumPeers   int         `json:"num_peers"`
	NumSeeds   int         `json:"num_seeds"`
	NumLeeches int         `json:"num_leeches"`
	Msg        string      `json:"msg"`
}

type qbHTTPClient struct {
	Client     *http.Client
	BaseURL    string
	IsLoggedIn bool
}

type ScreenshotRequest struct {
	RemotePath          string    `json:"remote_path"`
	ContentName         string    `json:"content_name,omitempty"`
	Mode                string    `json:"mode,omitempty"`
	PreviewCount        int       `json:"preview_count,omitempty"`
	SelectedTimes       []float64 `json:"selected_times,omitempty"`
	SelectedSubtitleSID *int      `json:"selected_subtitle_sid,omitempty"`
	PixhostDomain       string    `json:"pixhost_domain,omitempty"`
}

type ScreenshotPreviewCandidate struct {
	ID          string  `json:"id"`
	TimeSeconds float64 `json:"time_seconds"`
	TimeLabel   string  `json:"time_label"`
	PreviewData string  `json:"preview_data"`
	Recommended bool    `json:"recommended"`
}

type ScreenshotSubtitleState string

const (
	ScreenshotSubtitleStateConfirmedChinese     ScreenshotSubtitleState = "confirmed_chinese"
	ScreenshotSubtitleStateUsableButUnconfirmed ScreenshotSubtitleState = "usable_but_unconfirmed"
	ScreenshotSubtitleStateNoUsableSubtitle     ScreenshotSubtitleState = "no_usable_subtitle"
)

type ScreenshotSubtitleStream struct {
	SubtitleSID        int    `json:"subtitle_sid"`
	StreamIndex        int    `json:"stream_index"`
	CodecName          string `json:"codec_name"`
	Language           string `json:"language,omitempty"`
	Title              string `json:"title,omitempty"`
	DisplayName        string `json:"display_name"`
	IsConfidentChinese bool   `json:"is_confident_chinese"`
	IsDefault          bool   `json:"is_default"`
}

type ScreenshotResponse struct {
	Success            bool                         `json:"success"`
	Message            string                       `json:"message"`
	BBCode             string                       `json:"bbcode,omitempty"`
	SubtitleState      string                       `json:"subtitle_state,omitempty"`
	SubtitleStreams    []ScreenshotSubtitleStream   `json:"subtitle_streams,omitempty"`
	CurrentSubtitleSID int                          `json:"current_subtitle_sid,omitempty"`
	PreviewCandidates  []ScreenshotPreviewCandidate `json:"preview_candidates,omitempty"`
}

type MediaInfoRequest struct {
	RemotePath  string `json:"remote_path"`
	ContentName string `json:"content_name,omitempty"`
}

type MediaInfoResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	MediaInfo string `json:"mediainfo,omitempty"`
	IsBDMV    bool   `json:"is_bdmv,omitempty"`
}

type FileCheckRequest struct {
	RemotePath string `json:"remote_path"`
}

type FileCheckResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Exists  bool   `json:"exists"`
	IsFile  bool   `json:"is_file,omitempty"`
	Size    int64  `json:"size,omitempty"`
}

type BatchFileCheckRequest struct {
	RemotePaths []string `json:"remote_paths"`
}

type FileCheckResult struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	IsFile bool   `json:"is_file"`
	Size   int64  `json:"size"`
}

type BatchFileCheckResponse struct {
	Success bool              `json:"success"`
	Message string            `json:"message"`
	Results []FileCheckResult `json:"results"`
}

type EpisodeCountRequest struct {
	RemotePath string `json:"remote_path"`
}

type EpisodeCountResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	EpisodeCount int    `json:"episode_count,omitempty"`
	SeasonNumber int    `json:"season_number,omitempty"`
}

type UploadLimitGroup struct {
	LimitMBps  int      `json:"limit_mbps"`
	TorrentIDs []string `json:"torrent_ids"`
}

type UploadLimitDownloader struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Host     string             `json:"host"`
	Username string             `json:"username"`
	Password string             `json:"password"`
	Actions  []UploadLimitGroup `json:"actions"`
}

type UploadLimitBatchRequest struct {
	Downloaders []UploadLimitDownloader `json:"downloaders"`
}

type UploadLimitResult struct {
	DownloaderID    string   `json:"downloader_id"`
	AppliedGroups   int      `json:"applied_groups"`
	AppliedTorrents int      `json:"applied_torrents"`
	Errors          []string `json:"errors"`
}

type UploadLimitBatchResponse struct {
	Success bool                `json:"success"`
	Results []UploadLimitResult `json:"results"`
}

type subtitleStreamCandidate struct {
	SubtitleSID        int
	StreamIndex        int
	StreamOrdinal      int
	CodecName          string
	Language           string
	Title              string
	DisplayName        string
	ConfidenceScore    int
	IsConfidentChinese bool
	IsDefault          bool
	IsSupported        bool
}

type subtitleInspectionResult struct {
	State              ScreenshotSubtitleState
	Streams            []ScreenshotSubtitleStream
	Candidates         []subtitleStreamCandidate
	CurrentSubtitleSID int
}

type QBTorrentInfo struct {
	Hash       string  `json:"hash"`
	Name       string  `json:"name"`
	Size       int64   `json:"size"`
	Progress   float64 `json:"progress"`
	State      string  `json:"state"`
	SavePath   string  `json:"save_path"`
	Uploaded   int64   `json:"uploaded"`
	Downloaded int64   `json:"downloaded"`
	Ratio      float64 `json:"ratio"`
}

func writeJSONResponse(w http.ResponseWriter, _ *http.Request, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	body, err := json.Marshal(payload)
	if err != nil {
		statusCode = http.StatusInternalServerError
		body = []byte(`{"success":false,"message":"failed to encode response"}`)
	}
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

func toStringAny(value any, fallback string) string {
	switch typed := value.(type) {
	case nil:
		return fallback
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func toInt64Any(value any) int64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case int:
		return int64(typed)
	case int8:
		return int64(typed)
	case int16:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case uint:
		return int64(typed)
	case uint8:
		return int64(typed)
	case uint16:
		return int64(typed)
	case uint32:
		return int64(typed)
	case uint64:
		if typed > uint64(^uint64(0)>>1) {
			return 0
		}
		return int64(typed)
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		if v, err := typed.Int64(); err == nil {
			return v
		}
		if v, err := typed.Float64(); err == nil {
			return int64(v)
		}
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0
		}
		if v, err := strconv.ParseInt(text, 10, 64); err == nil {
			return v
		}
		if v, err := strconv.ParseFloat(text, 64); err == nil {
			return int64(v)
		}
	}
	return 0
}

func toFloat64Any(value any) float64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case float32:
		return float64(typed)
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		if v, err := typed.Float64(); err == nil {
			return v
		}
	case string:
		if v, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return v
		}
	}
	return 0
}

func toBoolAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case string:
		text := strings.ToLower(strings.TrimSpace(typed))
		return text == "1" || text == "true" || text == "yes"
	default:
		return false
	}
}
