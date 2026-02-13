package extract

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pt-nexus/server-go/internal/config"
	"github.com/pt-nexus/server-go/internal/platform/logx"
	"gopkg.in/yaml.v3"
)

const unwantedImageLogModule = "图片过滤"

type unwantedImageURLSet struct {
	Items map[string]struct{}
}

var unwantedImageURLCache sync.Map

func filterUnwantedImageURLs(urls []string) []string {
	if len(urls) == 0 {
		return urls
	}
	set := getUnwantedImageURLSet()
	if len(set) == 0 {
		return urls
	}
	out := make([]string, 0, len(urls))
	for _, raw := range urls {
		url := strings.TrimSpace(raw)
		if url == "" {
			continue
		}
		if _, blocked := set[url]; blocked {
			continue
		}
		out = append(out, url)
	}
	return out
}

func getUnwantedImageURLSet() map[string]struct{} {
	paths := config.ResolveRuntimePaths()
	mappingPath := strings.TrimSpace(paths.GlobalMapYML)
	if mappingPath == "" || filepath.Clean(mappingPath) == "." {
		return map[string]struct{}{}
	}

	if cached, ok := unwantedImageURLCache.Load(mappingPath); ok {
		if typed, ok := cached.(unwantedImageURLSet); ok {
			return typed.Items
		}
	}

	parsed := loadUnwantedImageURLSetFromYAML(mappingPath)
	unwantedImageURLCache.Store(mappingPath, unwantedImageURLSet{Items: parsed})
	return parsed
}

func loadUnwantedImageURLSetFromYAML(mappingPath string) map[string]struct{} {
	content, err := os.ReadFile(mappingPath)
	if err != nil {
		return map[string]struct{}{}
	}

	root := map[string]any{}
	if err := yaml.Unmarshal(content, &root); err != nil {
		logx.Debugf(unwantedImageLogModule, "解析 global_mappings 失败，跳过图片过滤 path=%s err=%v", mappingPath, err)
		return map[string]struct{}{}
	}

	contentFiltering, ok := root["content_filtering"].(map[string]any)
	if !ok || contentFiltering == nil {
		return map[string]struct{}{}
	}

	urls := parseStringSlice(contentFiltering["unwanted_image_urls"])
	if len(urls) == 0 {
		return map[string]struct{}{}
	}

	set := make(map[string]struct{}, len(urls))
	for _, raw := range urls {
		url := strings.TrimSpace(raw)
		if url == "" {
			continue
		}
		set[url] = struct{}{}
	}
	return set
}

func parseStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

