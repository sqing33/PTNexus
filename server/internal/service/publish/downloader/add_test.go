package downloader

import "testing"

func TestAddToDownloaderCannotBypassGuard(t *testing.T) {
	previous := checkDownloaderGate
	calledWith := ""
	checkDownloaderGate = func(downloaderID string) (bool, string) {
		calledWith = downloaderID
		return false, "达到数量限制"
	}
	t.Cleanup(func() { checkDownloaderGate = previous })

	result, status := AddToDownloader(map[string]any{
		"url":          "https://example.invalid/download.php?id=1",
		"savePath":     "/downloads",
		"downloaderId": "qb-main",
	}, map[string]any{}, nil)

	if status != 200 || result["limit_reached"] != true {
		t.Fatalf("unexpected result: status=%d result=%#v", status, result)
	}
	if calledWith != "qb-main" {
		t.Fatalf("guard called with %q", calledWith)
	}
}
