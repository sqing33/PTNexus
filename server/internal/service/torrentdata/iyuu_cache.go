package torrentdata

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/config"
)

type iyuuSiteCacheFile struct {
	Timestamp      string     `json:"timestamp"`
	SIDSHA1        string     `json:"sid_sha1"`
	SupportedSites []iyuuSite `json:"supported_sites"`
	SitesList      []string   `json:"sites_list"`
}

const iyuuCacheExpiryDays = 7

func iyuuCacheFilePath() string {
	paths := config.ResolveRuntimePaths()
	return filepath.Join(paths.DataDir, "iyuu_site_cache.json")
}

// loadIYUUSiteCache 加载 IYUU 缓存文件并判断是否有效。
// 参数/返回：currentSites 为 torrents 表中当前出现过的站点昵称列表；返回 (sid_sha1, sites, valid)。
// 失败场景：文件不存在/损坏/过期/站点列表变化时返回 valid=false。
// 副作用：读取本地文件。
func loadIYUUSiteCache(currentSites []string) (string, []iyuuSite, bool) {
	path := iyuuCacheFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, false
	}
	var cache iyuuSiteCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return "", nil, false
	}
	if cache.SIDSHA1 == "" || cache.Timestamp == "" {
		return "", nil, false
	}
	ts, err := time.Parse(time.RFC3339, cache.Timestamp)
	if err != nil {
		// 兼容历史/手写格式
		ts, err = time.Parse("2006-01-02 15:04:05", cache.Timestamp)
		if err != nil {
			return "", nil, false
		}
	}
	if time.Since(ts) > (iyuuCacheExpiryDays * 24 * time.Hour) {
		return "", nil, false
	}
	if !sameStringSet(cache.SitesList, currentSites) {
		return "", nil, false
	}
	return cache.SIDSHA1, cache.SupportedSites, true
}

// saveIYUUSiteCache 保存 IYUU 缓存文件。
// 参数/返回：sid_sha1 与 sites 为 IYUU 过滤后的可辅种站点列表；sitesList 为 torrents 表当前站点列表；返回错误。
// 失败场景：写入失败。
// 副作用：写入本地文件。
func saveIYUUSiteCache(sidSha1 string, sites []iyuuSite, sitesList []string) error {
	if sidSha1 == "" {
		return errors.New("sid_sha1 为空，无法写入缓存")
	}
	cache := iyuuSiteCacheFile{
		Timestamp:      time.Now().Format(time.RFC3339),
		SIDSHA1:        sidSha1,
		SupportedSites: sites,
		SitesList:      sitesList,
	}
	content, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	path := iyuuCacheFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func sameStringSet(a []string, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	setA := map[string]struct{}{}
	for _, v := range a {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		setA[v] = struct{}{}
	}
	setB := map[string]struct{}{}
	for _, v := range b {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		setB[v] = struct{}{}
	}
	if len(setA) != len(setB) {
		return false
	}
	for k := range setA {
		if _, ok := setB[k]; !ok {
			return false
		}
	}
	return true
}
