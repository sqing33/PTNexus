package sites

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

const longptPublishLogModule = "发布-LongPT"

// 定义 LongPT 站点在公共表单发布流程上的差异步骤。
type longptPublisher struct {
	publicSiteDefaults
}

// PublishLongPT 执行 LongPT 站点特殊发布流程（UHD Remux 媒介选择、高分/高码标签自动添加）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回发布详情页 URL、是否疑似"种子已存在"、用于自动编辑的表单字段，以及发布过程日志。
// 失败场景：同 Public 发布器。
// 副作用：读取本地种子文件并向目标站点发起上传请求；可选写入 data/tmp/torrents 参数落盘。
func PublishLongPT(input publisher.PublishInput) (publisher.PublishResult, error) {
	return publishWithPublicSite(input, longptPublisher{})
}

func (longptPublisher) LogModule() string {
	return longptPublishLogModule
}

func (longptPublisher) AdjustFormFields(input publisher.PublishInput, formFields map[string]string) {
	if formFields == nil {
		return
	}

	standardized := extractLongPTStandardizedParams(input.UploadData)
	medium := strings.TrimSpace(toStringAny(standardized["medium"], ""))
	resolution := strings.TrimSpace(toStringAny(standardized["resolution"], ""))
	title := strings.TrimSpace(input.Title)

	// 1. UHD BluRay Remux 媒介：标题含 UHD + Remux 或标准化参数为 uhd_bluray_remux 时选择值 11
	if isLongPTUHDBluRayRemux(medium, title) {
		formFields["medium_sel[4]"] = "11"
	}

	// 2. 高分/高码标签增强
	applyLongPTTagOverrides(formFields, input, standardized, resolution)
}

// --- UHD BluRay Remux 检测 ---

func isLongPTUHDBluRayRemux(medium, title string) bool {
	if medium == "medium.uhd_bluray_remux" {
		return true
	}
	upperTitle := strings.ToUpper(title)
	return strings.Contains(upperTitle, "UHD") && strings.Contains(upperTitle, "REMUX")
}

// --- 标签增强（高分 + 高码） ---

func applyLongPTTagOverrides(formFields map[string]string, input publisher.PublishInput, standardized map[string]any, resolution string) {
	tags := resolveSiteCombinedTags(input.UploadData)

	// 高分：豆瓣评分 >= 8.0
	if rating := extractLongPTDoubanRating(input.UploadData, input.Description); rating >= 8.0 {
		tags["tag.高分"] = struct{}{}
	}

	// 高码：4K > 15Mb/s 或 1080p > 9Mb/s
	if bitrate := extractLongPTBitrateMbps(input.MediaInfo); bitrate > 0 {
		if (resolution == "resolution.r2160p" && bitrate > 15) ||
			(resolution == "resolution.r1080p" && bitrate > 9) {
			tags["tag.高码"] = struct{}{}
		}
	}

	rebuildLongPTTagFields(formFields, tags)
}

// 重建 LongPT 标签表单字段（基于 tags[4] 索引格式）。
func rebuildLongPTTagFields(formFields map[string]string, tags map[string]struct{}) {
	if formFields == nil || len(tags) == 0 {
		return
	}

	siteCfg, err := publishmapping.LoadSitePublishConfig("longpt")
	if err != nil || siteCfg == nil {
		return
	}
	tagMapping := siteCfg.Mappings["tag"]
	if len(tagMapping) == 0 {
		return
	}

	tagIDs := make([]string, 0, len(tags))
	for tag := range tags {
		candidates := []string{tag}
		if strings.HasPrefix(tag, "tag.") {
			candidates = append(candidates, strings.TrimPrefix(tag, "tag."))
		} else {
			candidates = append(candidates, "tag."+tag)
		}
		for _, candidate := range candidates {
			if mappedValue := publishmapping.PickMappedValueWithFallbackNoDefault("tag", tagMapping, candidate); strings.TrimSpace(mappedValue) != "" {
				tagIDs = append(tagIDs, strings.TrimSpace(mappedValue))
				break
			}
		}
	}
	if len(tagIDs) == 0 {
		return
	}

	unique := map[string]struct{}{}
	for _, id := range tagIDs {
		unique[id] = struct{}{}
	}
	finalIDs := make([]string, 0, len(unique))
	for id := range unique {
		finalIDs = append(finalIDs, id)
	}
	sort.Strings(finalIDs)

	rebuildIndexedFormFields(formFields, "tags[4]", finalIDs)
}

// --- 码率提取 ---

var reLongPTBitrate = regexp.MustCompile(`(?i)Overall\s*bit\s*rate\s*:\s*([\d\s.]+)\s*(Mb/s|kb/s|Kbps|Mbps)`)

func extractLongPTBitrateMbps(text string) float64 {
	if text == "" {
		return 0
	}
	text = normalizeLongPTMediaInfoWhitespace(text)
	match := reLongPTBitrate.FindStringSubmatch(text)
	if len(match) < 3 {
		return 0
	}
	number := strings.NewReplacer(" ", "", "\u00a0", "", "\u2007", "", "\u202f", "").
		Replace(strings.TrimSpace(match[1]))
	value, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return 0
	}
	unit := strings.ToLower(strings.TrimSpace(match[2]))
	switch unit {
	case "kb/s", "kbps":
		return value / 1000
	default:
		return value
	}
}

// --- 豆瓣评分提取 ---

var reLongPTDoubanRating = regexp.MustCompile(`豆瓣评分[：:]\s*([\d.]+)`)
var reLongPTRatingGeneric = regexp.MustCompile(`(?:评[分分]|rating|score)[：:\s]*([\d.]+)\s*(?:[/／]\s*10)?`)

func extractLongPTDoubanRating(uploadData map[string]any, description string) float64 {
	if uploadData != nil {
		// 检查标准化参数中的 douban 相关字段
		if standardized, ok := uploadData["standardized_params"].(map[string]any); ok && standardized != nil {
			for _, key := range []string{"douban_rating", "rating", "score"} {
				if val := standardized[key]; val != nil {
					if rating := parseLongPTFloat(toStringAny(val, "")); rating > 0 {
						return rating
					}
				}
			}
		}
		// 检查顶层字段
		for _, key := range []string{"douban_rating", "rating", "score"} {
			if val := uploadData[key]; val != nil {
				if rating := parseLongPTFloat(toStringAny(val, "")); rating > 0 {
					return rating
				}
			}
		}
	}
	// 从简介文本中提取豆瓣评分
	return extractLongPTRatingFromText(description)
}

func extractLongPTRatingFromText(text string) float64 {
	if text == "" {
		return 0
	}
	// 优先匹配"豆瓣评分"
	if match := reLongPTDoubanRating.FindStringSubmatch(text); len(match) > 1 {
		if rating := parseLongPTFloat(match[1]); rating > 0 && rating <= 10 {
			return rating
		}
	}
	// 兜底匹配通用评分格式
	if match := reLongPTRatingGeneric.FindStringSubmatch(text); len(match) > 1 {
		if rating := parseLongPTFloat(match[1]); rating > 0 && rating <= 10 {
			return rating
		}
	}
	return 0
}

// --- 工具函数 ---

func extractLongPTStandardizedParams(uploadData map[string]any) map[string]any {
	if uploadData == nil {
		return nil
	}
	if item, ok := uploadData["standardized_params"].(map[string]any); ok && item != nil {
		return item
	}
	return nil
}

func parseLongPTFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

func normalizeLongPTMediaInfoWhitespace(text string) string {
	return strings.NewReplacer(
		"\u00a0", " ",
		"\u2007", " ",
		"\u202f", " ",
	).Replace(text)
}
