package sites

import (
	"regexp"
	"strings"

	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

// 定义 TTG 站点在公共表单发布流程上的差异步骤。
type ttgPublisher struct {
	publicSiteDefaults
}

// PublishTTG 执行 TTG 站点特殊发布流程（合并 type 下拉框、提取 IMDB/Douban ID）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回发布详情页 URL、是否疑似"种子已存在"、用于自动编辑的表单字段，以及发布过程日志。
// 失败场景：同 Public 发布器。
// 副作用：读取本地种子文件并向目标站点发起上传请求；可选写入 data/tmp/torrents 参数落盘。
func PublishTTG(input publisher.PublishInput) (publisher.PublishResult, error) {
	return publishWithPublicSite(input, ttgPublisher{})
}

func (ttgPublisher) LogModule() string {
	return "发布-TTG"
}

func (ttgPublisher) AdjustFormFields(input publisher.PublishInput, formFields map[string]string) {
	if formFields == nil {
		return
	}

	standardized := extractTTGStandardizedParams(input.UploadData)

	// TTG 使用单一 type 下拉框，合并 category + medium + resolution
	category := strings.TrimSpace(toStringAny(standardized["type"], ""))
	medium := strings.TrimSpace(toStringAny(standardized["medium"], ""))
	resolution := strings.TrimSpace(toStringAny(standardized["resolution"], ""))

	if ttgType := resolveTTGType(category, medium, resolution, hasAnimationTag(input.UploadData)); ttgType != "" {
		formFields["type"] = ttgType
	}

	// 移除 medium/resolution 等可能残留的无效字段（TTG 无此表单字段）
	delete(formFields, "medium")
	delete(formFields, "codec")
	delete(formFields, "audiocodec")
	delete(formFields, "standard")

	// 提取 IMDB ID（TTG 的 imdb_c 字段期望纯 ID，如 tt0000000）
	if imdbURL := strings.TrimSpace(formFields["imdb_c"]); imdbURL != "" && strings.Contains(imdbURL, "imdb") {
		if id := extractTTGIMDBID(imdbURL); id != "" {
			formFields["imdb_c"] = id
		}
	}

	// 提取 Douban ID（TTG 的 douban_id 字段期望纯数字 ID）
	if doubanURL := strings.TrimSpace(formFields["douban_id"]); doubanURL != "" && strings.Contains(doubanURL, "douban") {
		if id := extractTTGDoubanID(doubanURL); id != "" {
			formFields["douban_id"] = id
		}
	}

	// TTG 必填字段：匿名发布选择（默认非匿名）、禁转选择（默认否）和 HR 制度（默认否）
	if _, exists := formFields["anonymity"]; !exists {
		formFields["anonymity"] = "no"
	}
	if _, exists := formFields["nodistr"]; !exists {
		formFields["nodistr"] = "no"
	}
	if _, exists := formFields["hr"]; !exists {
		formFields["hr"] = "no"
	}

	// TTG 发种标题规范：主标题中的 "." 替换为 "{@}"，副标题以 "[]" 包裹并追加到主标题后
	adjustTTGTitle(formFields)
}

// resolveTTGType 根据分类、媒介和分辨率的组合，返回 TTG 的 type 值。
func resolveTTGType(category, medium, resolution string, isAnimation bool) string {
	isBluRay := medium == "medium.bluray" || medium == "medium.uhd_bluray" ||
		medium == "medium.bluray_diy" || medium == "medium.uhd_diy"
	isUHD := medium == "medium.uhd_bluray" || medium == "medium.uhd_diy" ||
		resolution == "resolution.r2160p"
	if isAnimation {
		if isBluRay {
			return "111" // 动漫原盘
		}
		return "58" // 高清动漫
	}

	switch category {
	case "category.movie":
		if isBluRay && isUHD {
			return "109" // UHD原盘
		}
		if isBluRay {
			return "54" // BluRay原盘
		}
		switch resolution {
		case "resolution.r2160p":
			return "108" // 影视2160p
		case "resolution.r720p":
			return "52" // 电影720p
		default:
			return "53" // 电影1080i/p
		}

	case "category.documentary":
		if isBluRay {
			return "67" // 纪录片BluRay原盘
		}
		switch resolution {
		case "resolution.r720p":
			return "62" // 纪录片720p
		default:
			return "63" // 纪录片1080i/p
		}

	case "category.tv_series":
		// 需要更细粒度的地区区分，但标准化参数通常不包含地区信息
		// 默认使用大陆港台剧（最常见的中文 PT 场景）
		switch resolution {
		case "resolution.r720p":
			return "76" // 大陆港台剧720p(单集)
		default:
			return "75" // 大陆港台剧1080i/p(单集)
		}

	case "category.tv_shows":
		return "90" // 华语剧包(全集)

	case "category.mv":
		return "59" // MV&演唱会

	case "category.music":
		return "83" // 无损音乐FLAC&APE

	case "category.sports":
		return "57" // 高清体育节目

	case "category.other":
		return "91" // MiniVideo
	}

	return ""
}

func extractTTGStandardizedParams(uploadData map[string]any) map[string]any {
	if uploadData == nil {
		return nil
	}
	if item, ok := uploadData["standardized_params"].(map[string]any); ok && item != nil {
		return item
	}
	return nil
}

var reTTGIMDBID = regexp.MustCompile(`(tt\d{7,})`)

func extractTTGIMDBID(value string) string {
	if matches := reTTGIMDBID.FindStringSubmatch(value); len(matches) > 1 {
		return matches[1]
	}
	return ""
}

var reTTGDoubanID = regexp.MustCompile(`douban\.com/(?:movie|subject)/(\d+)`)

func extractTTGDoubanID(value string) string {
	if matches := reTTGDoubanID.FindStringSubmatch(value); len(matches) > 1 {
		return matches[1]
	}
	// 如果已经是纯数字，直接返回
	trimmed := strings.TrimSpace(value)
	if trimmed != "" && regexp.MustCompile(`^\d+$`).MatchString(trimmed) {
		return trimmed
	}
	return ""
}

// adjustTTGTitle 按 TTG 发种标题规范调整标题字段。
// TTG 要求主标题中不得包含 "."，需用 "{@}" 替代（如 5.1 → 5{@}1）；
// 副标题先剔除其中的 "[" / "]" 字符，再以 "[]" 包裹后以空格连接追加到主标题尾部，
// 并清空独立副标题字段避免重复展示。
// 例：主标题 "Red Planet 2000 ... DTS-HD MA 5.1-HDS" + 副标题 "红色星球 / ... [简繁英字幕]"
//
//	→ "Red Planet 2000 ... DTS-HD MA 5{@}1-HDS [红色星球 / ... 简繁英字幕]"
func adjustTTGTitle(formFields map[string]string) {
	// 副标题可能落在 small_descr（NexusPHP 默认）或 subtitle（TTG 配置）字段，兼容读取
	subtitle := strings.TrimSpace(formFields["small_descr"])
	if subtitle == "" {
		subtitle = strings.TrimSpace(formFields["subtitle"])
	}
	// 剔除副标题内的 "[" / "]" 字符，避免与外层包裹的 "[]" 冲突
	subtitle = strings.NewReplacer("[", "", "]", "").Replace(subtitle)

	// NexusPHP 表单同时设置 name 与 title，统一处理
	for _, key := range []string{"name", "title"} {
		title := strings.TrimSpace(formFields[key])
		if title == "" {
			continue
		}
		title = strings.ReplaceAll(title, ".", "{@}")
		if subtitle != "" {
			title = title + " [" + subtitle + "]"
		}
		formFields[key] = title
	}

	// 副标题已并入主标题，清空独立副标题字段避免重复展示
	if subtitle != "" {
		delete(formFields, "small_descr")
		delete(formFields, "subtitle")
	}
}
