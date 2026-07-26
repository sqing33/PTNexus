package main

import (
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	port := ":9090"
	if len(os.Args) > 1 {
		port = strings.TrimSpace(os.Args[1])
		if port == "" {
			port = ":9090"
		}
		if !strings.HasPrefix(port, ":") {
			port = ":" + port
		}
	}

	http.HandleFunc("/api/torrents/all", allTorrentsHandler)
	http.HandleFunc("/api/stats/server", statsHandler)
	http.HandleFunc("/api/torrents/upload-limit/batch", uploadLimitBatchHandler)
	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, r, http.StatusOK, map[string]string{
			"status":  "ok",
			"message": "pt-nexus-box-proxy is healthy",
		})
	})
	http.HandleFunc("/api/media/screenshot", screenshotHandler)
	http.HandleFunc("/api/media/mediainfo", mediainfoHandler)
	RegisterBDInfoRoutes()
	http.HandleFunc("/api/file/check", fileCheckHandler)
	http.HandleFunc("/api/file/batch-check", batchFileCheckHandler)
	http.HandleFunc("/api/media/episode-count", episodeCountHandler)

	if isMInfoConfigured() {
		endpoint, err := resolveMInfoScreenshotEndpoint()
		if err != nil {
			log.Printf("MInfo screenshot integration is configured but invalid: err=%v", err)
		} else {
			log.Printf("MInfo screenshot integration enabled: endpoint=%s modes=automatic,finalize variant=jpg hdr_processor=libplacebo", endpoint)
		}
	} else {
		log.Printf("MInfo screenshot integration disabled: set %s to enable HDR/DV automatic and finalize screenshots", minfoBaseURLEnv)
	}

	log.Printf("pt-nexus-box-proxy listening on %s", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
