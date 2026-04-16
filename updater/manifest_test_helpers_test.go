package main

import (
	"net/http"
	"net/http/httptest"
)

type manifestTestResponse struct {
	status int
	body   string
}

func newManifestTestServer(t interface{ Cleanup(func()) }, routes map[string]manifestTestResponse) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if resp, ok := routes[r.URL.Path]; ok {
			status := resp.status
			if status == 0 {
				status = http.StatusOK
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(resp.body))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}
