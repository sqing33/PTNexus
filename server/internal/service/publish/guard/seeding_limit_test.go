package guard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newQBGuardTestServer(t *testing.T, seeding []qbTorrent, paused []qbTorrent, failFilter string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v2/auth/login":
			_, _ = writer.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			filter := request.URL.Query().Get("filter")
			if filter == failFilter {
				http.Error(writer, "query failed", http.StatusBadGateway)
				return
			}
			rows := seeding
			if filter == "paused" {
				rows = paused
			}
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(rows)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func recentQBTorrent(hash string, state string, uploadSpeed int64) qbTorrent {
	return qbTorrent{
		Hash:        hash,
		State:       state,
		NumComplete: 1,
		UpSpeed:     uploadSpeed,
		AddedOn:     time.Now().Unix(),
		Ratio:       0.5,
	}
}

func TestQBGuardCountsUploadingAndPausedBoundaries(t *testing.T) {
	previousLimit := maxRecentAdditions
	maxRecentAdditions = 15
	t.Cleanup(func() { maxRecentAdditions = previousLimit })

	for _, testCase := range []struct {
		name        string
		activeCount int
		pausedCount int
		wantAllowed bool
	}{
		{name: "fourteen", activeCount: 7, pausedCount: 7, wantAllowed: true},
		{name: "fifteen", activeCount: 8, pausedCount: 7, wantAllowed: false},
		{name: "sixteen", activeCount: 8, pausedCount: 8, wantAllowed: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			seeding := make([]qbTorrent, 0, testCase.activeCount)
			paused := make([]qbTorrent, 0, testCase.pausedCount)
			for index := 0; index < testCase.activeCount; index++ {
				seeding = append(seeding, recentQBTorrent("active-"+string(rune('a'+index)), "uploading", 1024))
			}
			for index := 0; index < testCase.pausedCount; index++ {
				paused = append(paused, recentQBTorrent("paused-"+string(rune('a'+index)), "pausedUP", 0))
			}

			server := newQBGuardTestServer(t, seeding, paused, "")
			stats, err := collectSeedingLimitGroupStats("127.0.0", []seedingLimitDownloader{{
				ID: "qb", Name: "QB", Type: "qbittorrent", Host: server.URL, Enabled: true,
			}})
			if err != nil {
				t.Fatalf("collect stats returned error: %v", err)
			}
			if stats.MonitoredCount != testCase.activeCount+testCase.pausedCount {
				t.Fatalf("monitored count = %d", stats.MonitoredCount)
			}
			if stats.RecentCount != testCase.activeCount+testCase.pausedCount {
				t.Fatalf("recent count = %d", stats.RecentCount)
			}
			allowed, _ := evaluateSeedingLimit(stats)
			if allowed != testCase.wantAllowed {
				t.Fatalf("allowed = %v, want %v", allowed, testCase.wantAllowed)
			}
		})
	}
}

func TestQBGuardAppliesAgeSeederAndRatioExemptions(t *testing.T) {
	old := recentQBTorrent("old", "uploading", 1024)
	old.AddedOn = time.Now().Add(-25 * time.Hour).Unix()
	highSeeders := recentQBTorrent("seeders", "uploading", 1024)
	highSeeders.NumComplete = minSeedsThreshold
	highRatio := recentQBTorrent("ratio", "pausedUP", 0)
	highRatio.Ratio = ratioIgnoreThreshold + 0.01
	counted := recentQBTorrent("counted", "pausedUP", 0)

	server := newQBGuardTestServer(t, []qbTorrent{old, highSeeders}, []qbTorrent{highRatio, counted}, "")
	stats, err := collectSeedingLimitGroupStats("127.0.0", []seedingLimitDownloader{{
		ID: "qb", Name: "QB", Type: "qbittorrent", Host: server.URL, Enabled: true,
	}})
	if err != nil {
		t.Fatalf("collect stats returned error: %v", err)
	}
	if stats.MonitoredCount != 2 {
		t.Fatalf("monitored count = %d, want 2", stats.MonitoredCount)
	}
	if stats.RecentCount != 1 {
		t.Fatalf("recent count = %d, want 1", stats.RecentCount)
	}
}

func TestGuardAggregatesSameSubnetDownloaders(t *testing.T) {
	first := newQBGuardTestServer(t, []qbTorrent{recentQBTorrent("one", "uploading", 1)}, nil, "")
	second := newQBGuardTestServer(t, nil, []qbTorrent{recentQBTorrent("two", "pausedUP", 0)}, "")

	stats, err := collectSeedingLimitGroupStats("127.0.0", []seedingLimitDownloader{
		{ID: "one", Name: "One", Type: "qbittorrent", Host: first.URL, Enabled: true},
		{ID: "two", Name: "Two", Type: "qbittorrent", Host: second.URL, Enabled: true},
	})
	if err != nil {
		t.Fatalf("collect stats returned error: %v", err)
	}
	if stats.MonitoredCount != 2 || stats.RecentCount != 2 {
		t.Fatalf("unexpected aggregate: %#v", stats)
	}
	if strings.Join(stats.DownloaderNames, ",") != "One,Two" {
		t.Fatalf("unexpected downloader names: %#v", stats.DownloaderNames)
	}
}

func TestGuardFailsClosedWhenDownloaderQueryFails(t *testing.T) {
	server := newQBGuardTestServer(t, nil, nil, "paused")
	allowed, message := checkSeedingLimitForGroup("127.0.0", []seedingLimitDownloader{{
		ID: "qb", Name: "QB", Type: "qbittorrent", Host: server.URL, Enabled: true,
	}})
	if allowed {
		t.Fatal("expected query failure to block publishing")
	}
	if !strings.Contains(message, "统计失败") {
		t.Fatalf("unexpected message: %s", message)
	}
}

func TestResolveDownloaderSubnetGroupsIPv4By24(t *testing.T) {
	first, err := resolveDownloaderSubnet(seedingLimitDownloader{Host: "http://192.168.10.2:8080"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolveDownloaderSubnet(seedingLimitDownloader{Host: "https://192.168.10.99"})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first != "192.168.10" {
		t.Fatalf("subnets = %q and %q", first, second)
	}
}
