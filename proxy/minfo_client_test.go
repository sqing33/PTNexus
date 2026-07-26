package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestRequestScreenshotsFromMInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/screenshots" {
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.Error(w, "unexpected request path", http.StatusNotFound)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart form: %v", err)
			http.Error(w, "invalid multipart form", http.StatusBadRequest)
			return
		}

		expectedFields := map[string]string{
			"path":          "/downloads/movie.iso",
			"mode":          "links",
			"variant":       "jpg",
			"subtitle_mode": "auto",
			"hdr_processor": "libplacebo",
			"count":         "2",
		}
		for key, expected := range expectedFields {
			if actual := r.FormValue(key); actual != expected {
				t.Errorf("field %s: got %q, want %q", key, actual, expected)
			}
		}
		if actual := r.MultipartForm.Value["timestamp"]; !reflect.DeepEqual(actual, []string{"00:02:00", "00:08:00"}) {
			t.Errorf("timestamps: got %#v", actual)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"link_items":[{"url":"https://img.example/1.jpg","width":3840,"height":2160},{"url":"https://img.example/2.jpg"}]}`))
	}))
	defer server.Close()
	t.Setenv(minfoBaseURLEnv, server.URL)

	links, err := requestScreenshotsFromMInfo(context.Background(), "/downloads/movie.iso", []float64{120.25, 480.5})
	if err != nil {
		t.Fatalf("request screenshots: %v", err)
	}
	if len(links) != 2 || links[0].URL != "https://img.example/1.jpg" || links[0].Width != 3840 {
		t.Fatalf("unexpected links: %#v", links)
	}
}

func TestRequestScreenshotsFromMInfoRejectsEmptyLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"link_items":[]}`))
	}))
	defer server.Close()
	t.Setenv(minfoBaseURLEnv, server.URL+"/")

	if _, err := requestScreenshotsFromMInfo(context.Background(), "/downloads/movie.mkv", []float64{60}); err == nil {
		t.Fatal("expected an error for a response without image links")
	}
}

func TestSanitizeMInfoScreenshotTimes(t *testing.T) {
	actual := sanitizeMInfoScreenshotTimes([]float64{300, -1, 100, 100.4, 200})
	expected := []float64{100, 200, 300}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("got %#v, want %#v", actual, expected)
	}
}
