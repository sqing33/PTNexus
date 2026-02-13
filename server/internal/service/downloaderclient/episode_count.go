package downloaderclient

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// EpisodeCountInput 定义基于下载器上下文统计集数所需参数。
type EpisodeCountInput struct {
	RootConfig   map[string]any
	DownloaderID string
	SavePath     string
	TorrentName  string
	ContentName  string
}

// EpisodeCountResult 定义集数统计结果。
type EpisodeCountResult struct {
	Available    bool
	EpisodeCount int
	SeasonNumber int
	Source       string
	Reason       string
	ResolvedPath string
}

// CountEpisodesWithDownloaderContext 按“代理优先，本地映射兜底”策略统计剧集数量。
// 参数/返回：input 包含 downloader_id 与保存路径信息；返回可用性、来源与统计结果。
// 失败场景：路径不可达、代理不可用或无法识别集数时返回 Available=false。
// 副作用：可能访问盒子代理接口与本地文件系统。
func CountEpisodesWithDownloaderContext(input EpisodeCountInput) EpisodeCountResult {
	savePath := strings.TrimSpace(input.SavePath)
	if savePath == "" {
		return EpisodeCountResult{
			Available: false,
			Source:    "none",
			Reason:    "缺少保存路径",
		}
	}

	torrentName := strings.TrimSpace(input.TorrentName)
	contentName := strings.TrimSpace(input.ContentName)
	if contentName == "" {
		contentName = torrentName
	}
	candidates := buildPathCandidates(savePath, torrentName, contentName)

	trimmedDownloaderID := strings.TrimSpace(input.DownloaderID)
	if trimmedDownloaderID != "" {
		downloader, decision, err := DecideProxy(input.RootConfig, trimmedDownloaderID)
		if decision.Enabled {
			for _, candidate := range candidates {
				count, season, proxyErr := downloader.FetchEpisodeCountByProxy(candidate)
				if proxyErr == nil {
					return EpisodeCountResult{
						Available:    true,
						EpisodeCount: count,
						SeasonNumber: season,
						Source:       "proxy",
						Reason:       "通过盒子代理统计集数",
						ResolvedPath: candidate,
					}
				}
				if apiErr, ok := proxyErr.(*ProxyAPIError); ok && apiErr != nil && apiErr.StatusCode == 400 {
					continue
				}
				break
			}
		} else if err != nil && strings.TrimSpace(decision.Reason) == "config_error" {
			return EpisodeCountResult{
				Available: false,
				Source:    "none",
				Reason:    "读取下载器配置失败: " + strings.TrimSpace(err.Error()),
			}
		}
	}

	translatedSavePath := TranslateDownloaderPath(input.RootConfig, trimmedDownloaderID, savePath)
	localCandidates := buildPathCandidates(translatedSavePath, torrentName, contentName)
	count, season, resolvedPath, ok := countLocalEpisodesFromCandidates(localCandidates)
	if !ok {
		return EpisodeCountResult{
			Available:    false,
			Source:       "none",
			Reason:       "本地路径不可用或未识别到剧集文件",
			ResolvedPath: translatedSavePath,
		}
	}

	return EpisodeCountResult{
		Available:    true,
		EpisodeCount: count,
		SeasonNumber: season,
		Source:       "local",
		Reason:       "通过本地路径统计集数",
		ResolvedPath: resolvedPath,
	}
}

func buildPathCandidates(savePath, torrentName, contentName string) []string {
	trimmedSavePath := strings.TrimSpace(savePath)
	trimmedTorrentName := strings.TrimSpace(torrentName)
	trimmedContentName := strings.TrimSpace(contentName)

	candidates := make([]string, 0, 3)
	if trimmedSavePath != "" && trimmedTorrentName != "" {
		candidates = append(candidates, filepath.Join(trimmedSavePath, trimmedTorrentName))
	}
	if trimmedSavePath != "" && trimmedContentName != "" && !strings.EqualFold(trimmedContentName, trimmedTorrentName) {
		candidates = append(candidates, filepath.Join(trimmedSavePath, trimmedContentName))
	}
	if trimmedSavePath != "" {
		candidates = append(candidates, trimmedSavePath)
	}
	return compactStrings(candidates)
}

func countLocalEpisodesFromCandidates(candidates []string) (int, int, string, bool) {
	root := ""
	for _, candidate := range candidates {
		clean := filepath.Clean(strings.TrimSpace(candidate))
		if clean == "." || clean == "" {
			continue
		}
		if stat, err := os.Stat(clean); err == nil && stat != nil && stat.IsDir() {
			root = clean
			break
		}
	}
	if root == "" {
		return 0, 0, "", false
	}

	count, season, ok := countEpisodesFromLocalPath(root)
	if !ok {
		return 0, 0, root, false
	}
	return count, season, root, true
}

func countEpisodesFromLocalPath(root string) (int, int, bool) {
	videoExt := map[string]struct{}{
		".mkv":  {},
		".mp4":  {},
		".ts":   {},
		".avi":  {},
		".wmv":  {},
		".mov":  {},
		".flv":  {},
		".m2ts": {},
	}

	reEpisode := regexp.MustCompile(`(?i)[Ss](\d{1,2})[Ee](\d{1,3})`)
	episodesBySeason := map[int]map[int]struct{}{}

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if _, ok := videoExt[ext]; !ok {
			return nil
		}
		matches := reEpisode.FindStringSubmatch(d.Name())
		if len(matches) < 3 {
			return nil
		}
		season, err1 := strconv.Atoi(matches[1])
		episode, err2 := strconv.Atoi(matches[2])
		if err1 != nil || err2 != nil || season <= 0 || episode <= 0 {
			return nil
		}
		if _, ok := episodesBySeason[season]; !ok {
			episodesBySeason[season] = map[int]struct{}{}
		}
		episodesBySeason[season][episode] = struct{}{}
		return nil
	})

	if len(episodesBySeason) == 0 {
		return 0, 0, false
	}
	if season1, ok := episodesBySeason[1]; ok && len(season1) > 0 {
		return len(season1), 1, true
	}

	mainSeason := 0
	total := 0
	for season, eps := range episodesBySeason {
		if len(eps) == 0 {
			continue
		}
		total += len(eps)
		if mainSeason == 0 || season < mainSeason {
			mainSeason = season
		}
	}
	if total <= 0 {
		return 0, 0, false
	}
	if mainSeason <= 0 {
		mainSeason = 1
	}
	return total, mainSeason, true
}
