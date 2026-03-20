package sites

import (
	"fmt"
	"regexp"
	"strings"

	processingrepair "github.com/pt-nexus/server/internal/service/processing/repair"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

const hddolbyPublishLogModule = "发布-HDDolby"

var reHDDolbyTMDbLink = regexp.MustCompile(`https?://(?:www\.)?themoviedb\.org/[a-zA-Z]+/\d+/?`)

// 定义 HDDolby 站点在公共表单发布流程上的差异步骤。
type hddolbyPublisher struct {
	publicSiteDefaults
}

// PublishHDDolby 执行 HDDolby 站点特殊发布流程（TMDb/MediaInfo/截图独立字段）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回公共发布器结果。
// 失败场景：TMDb 链接无法解析或公共发布器失败时返回 error。
// 副作用：可能请求 TMDb 接口兜底解析链接，并调用公共发布器上传。
func PublishHDDolby(input publisher.PublishInput) (publisher.PublishResult, error) {
	return publishWithPublicSite(input, hddolbyPublisher{})
}

func (hddolbyPublisher) LogModule() string {
	return hddolbyPublishLogModule
}

func (hddolbyPublisher) AttemptPrefix(input publisher.PublishInput) string {
	return "检测到 HDDolby 站点：启用 TMDb/MediaInfo/截图独立字段"
}

func (hddolbyPublisher) BuildDescription(input publisher.PublishInput) string {
	return buildHDDolbyDescription(input.UploadData)
}

func (hddolbyPublisher) BuildExtraFormFields(input publisher.PublishInput) (map[string]string, error) {
	return buildHDDolbyExtraFormFields(input)
}

// 构造 HDDolby 的简介内容，仅保留声明、海报与正文。
func buildHDDolbyDescription(uploadData map[string]any) string {
	intro, _ := uploadData["intro"].(map[string]any)
	parts := make([]string, 0, 3)
	for _, key := range []string{"statement", "poster", "body"} {
		section := strings.TrimSpace(firstNonEmpty(
			toStringAny(uploadData[key], ""),
			toStringAny(intro[key], ""),
		))
		if section != "" {
			parts = append(parts, section)
		}
	}
	return strings.Join(parts, "\n\n")
}

// 构造 HDDolby 独立字段，包括 TMDb、MediaInfo 与截图。
func buildHDDolbyExtraFormFields(input publisher.PublishInput) (map[string]string, error) {
	mediaInfo := strings.TrimSpace(firstNonEmpty(
		toStringAny(input.UploadData["media_info"], ""),
		toStringAny(input.UploadData["mediainfo"], ""),
		strings.TrimSpace(input.MediaInfo),
	))
	screenshots := strings.Join(extractImageURLsFromText(resolveUploadSection(input.UploadData, "screenshots")), "\n")
	tmdbURL := strings.TrimSpace(resolveHDDolbyTMDbURL(input))
	if tmdbURL == "" {
		return nil, fmt.Errorf("HDDolby 缺少 TMDb 链接")
	}
	return map[string]string{
		"tmdb_url":    tmdbURL,
		"media_info":  mediaInfo,
		"screenshots": strings.TrimSpace(screenshots),
	}, nil
}

// 解析 HDDolby 必填的 TMDb 链接，必要时通过 IMDb 兜底转换。
func resolveHDDolbyTMDbURL(input publisher.PublishInput) string {
	uploadData := input.UploadData
	standardized, _ := uploadData["standardized_params"].(map[string]any)
	intro, _ := uploadData["intro"].(map[string]any)

	tmdbLink := strings.TrimSpace(firstNonEmpty(
		toStringAny(uploadData["tmdb_link"], ""),
		toStringAny(uploadData["tmdbLink"], ""),
		toStringAny(standardized["tmdb_link"], ""),
		toStringAny(standardized["tmdbLink"], ""),
		toStringAny(intro["tmdb_link"], ""),
	))
	if tmdbLink != "" {
		return processingrepair.NormalizeExternalLink(tmdbLink, reHDDolbyTMDbLink)
	}

	imdbLink := strings.TrimSpace(firstNonEmpty(
		toStringAny(uploadData["imdb_link"], ""),
		toStringAny(uploadData["imdbLink"], ""),
		toStringAny(standardized["imdb_link"], ""),
		toStringAny(standardized["imdbLink"], ""),
		strings.TrimSpace(input.IMDbLink),
	))
	if imdbLink == "" {
		return ""
	}
	return processingrepair.ResolveTMDbLinkWithIMDbFallback("", imdbLink, hddolbyPublishLogModule, "HDDolby发布")
}
