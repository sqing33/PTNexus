package sites

import (
	"errors"
	"regexp"
	"strings"

	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

// 定义我堡站点在公共表单发布流程上的差异步骤。
type ourbitsPublisher struct {
	publicSiteDefaults
}

// PublishOurBits 执行我堡站点特殊发布流程。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回公共发布器结果。
// 失败场景：公共发布器失败时返回 error。
// 副作用：调用公共发布器，并补充我堡上传页的海报、细分类和动态必填字段。
func PublishOurBits(input publisher.PublishInput) (publisher.PublishResult, error) {
	if isOurBitsRemux(input) {
		err := errors.New("我堡禁止发布 Remux 资源")
		return publisher.PublishResult{AttemptDetailLog: "发布前校验失败: " + err.Error()}, err
	}
	return publishWithPublicSite(input, ourbitsPublisher{})
}

func (ourbitsPublisher) AttemptPrefix(input publisher.PublishInput) string {
	return "检测到我堡站点：启用海报与动态分类字段映射"
}

func (ourbitsPublisher) BuildDescription(input publisher.PublishInput) string {
	return buildOurBitsDescription(input)
}

func (ourbitsPublisher) BuildExtraFormFields(input publisher.PublishInput) (map[string]string, error) {
	return buildOurBitsExtraFields(input), nil
}

func (ourbitsPublisher) AdjustFormFields(input publisher.PublishInput, formFields map[string]string) {
	adjustOurBitsCategory(input, formFields)
}

// buildOurBitsExtraFields 构造我堡上传页独立的海报和动态分类字段。
func buildOurBitsExtraFields(input publisher.PublishInput) map[string]string {
	result := map[string]string{}
	siteCfg, _ := publishmapping.LoadSitePublishConfig("ourbits")
	pictureField := "picture"
	if siteCfg != nil {
		pictureField = firstNonEmpty(siteCfg.FormFields["picture"], pictureField)
	}
	if poster := resolveOurBitsPoster(input); poster != "" {
		result[pictureField] = poster
	}
	for key, value := range buildOurBitsDynamicFields(input) {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			result[key] = value
		}
	}
	return result
}

func resolveOurBitsPoster(input publisher.PublishInput) string {
	candidates := []string{
		resolveUploadSection(input.UploadData, "poster"),
		resolveUploadSection(input.UploadData, "cover"),
		input.Description,
	}
	for _, candidate := range candidates {
		images := extractImageURLsFromText(candidate)
		if len(images) > 0 {
			return images[0]
		}
	}
	return ""
}

func adjustOurBitsCategory(input publisher.PublishInput, formFields map[string]string) {
	if formFields == nil {
		return
	}
	siteCfg, _ := publishmapping.LoadSitePublishConfig("ourbits")
	categoryField := "type"
	if siteCfg != nil {
		categoryField = firstNonEmpty(siteCfg.FormFields["category"], categoryField)
	}
	if category := resolveOurBitsCategory(input); category != "" {
		formFields[categoryField] = category
	}
}

// buildOurBitsDescription 按我堡要求把简介拼成“声明-海报链接-简介详情-MediaInfo-视频截图”。
func buildOurBitsDescription(input publisher.PublishInput) string {
	statement := resolveUploadSection(input.UploadData, "statement")
	poster := resolveUploadSection(input.UploadData, "poster")
	body := resolveUploadSection(input.UploadData, "body")
	mediainfo := firstNonEmpty(input.MediaInfo, toStringAny(input.UploadData["mediainfo"], ""))
	screenshots := resolveUploadSection(input.UploadData, "screenshots")

	parts := make([]string, 0, 5)
	for _, section := range []string{statement, poster, body} {
		if strings.TrimSpace(section) != "" {
			parts = append(parts, strings.TrimSpace(section))
		}
	}
	if strings.TrimSpace(mediainfo) != "" {
		parts = append(parts, "[quote]"+strings.TrimSpace(mediainfo)+"[/quote]")
	}
	if strings.TrimSpace(screenshots) != "" {
		parts = append(parts, strings.TrimSpace(screenshots))
	}
	if len(parts) == 0 {
		return strings.TrimSpace(firstNonEmpty(input.Description, input.Subtitle))
	}
	return strings.Join(parts, "\n")
}

func buildOurBitsDynamicFields(input publisher.PublishInput) map[string]string {
	standardized := ourbitsStandardized(input.UploadData)
	category := strings.TrimSpace(toStringAny(standardized["type"], ""))
	source := strings.TrimSpace(toStringAny(standardized["source"], ""))
	title := strings.TrimSpace(input.Title)
	result := map[string]string{}

	switch category {
	case "category.movie":
		result["movie_ename0day"] = strings.ReplaceAll(title, " ", ".")
		switch source {
		case "source.china", "source.hongkong", "source.taiwan", "source.hongkong_taiwan":
			result["second_type"] = "11"
			result["movie_country"] = "华语"
		case "source.japan", "source.korea", "source.india":
			result["second_type"] = "14"
			result["movie_country"] = "亚洲"
		case "source.western":
			result["second_type"] = "12"
			result["movie_country"] = "欧洲"
		}
	case "category.tv_series":
		result["tv_ename"] = strings.ReplaceAll(title, " ", ".")
		if season := extractOurBitsSeason(title); season != "" {
			result["tv_season"] = season
		}
		switch source {
		case "source.china":
			result["second_type"] = "15"
			result["tv_country"] = "大陆"
		case "source.hongkong", "source.taiwan", "source.hongkong_taiwan":
			result["second_type"] = "18"
			result["tv_country"] = "港台"
		case "source.japan", "source.korea":
			result["second_type"] = "16"
			result["tv_country"] = "日韩"
		case "source.western":
			result["second_type"] = "17"
			result["tv_country"] = "欧美"
		}
	case "category.documentaries":
		result["second_type"] = "10"
		result["record_ename"] = title
		result["record_whetherend"] = resolveOurBitsRecordEnd(title)
		if season := extractOurBitsSeason(title); season != "" {
			result["record_season"] = season
		}
		result["record_quality"] = resolveOurBitsRecordQuality(toStringAny(standardized["resolution"], ""))
		result["record_medium"] = resolveOurBitsRecordMedium(toStringAny(standardized["medium"], ""))
		if group := extractOurBitsGroup(title); group != "" {
			result["record_group"] = group
		}
	case "category.tv_shows":
		result["show_ename"] = strings.ReplaceAll(title, " ", ".")
		switch source {
		case "source.china":
			result["second_type"] = "27"
			result["show_country"] = "大陆"
		case "source.hongkong", "source.taiwan", "source.hongkong_taiwan":
			result["second_type"] = "29"
			result["show_country"] = "港台"
		case "source.japan", "source.korea":
			result["second_type"] = "28"
			result["show_country"] = "日韩"
		case "source.western":
			result["second_type"] = "30"
			result["show_country"] = "欧美"
		}
	case "category.music", "category.mv", "category.stage":
		if category == "category.mv" {
			result["music_type"] = "MV"
		} else if strings.EqualFold(toStringAny(standardized["medium"], ""), "medium.webdl") {
			result["music_type"] = "WEB"
		} else {
			result["music_type"] = "CD"
		}
	}
	return result
}

func resolveOurBitsCategory(input publisher.PublishInput) string {
	standardized := ourbitsStandardized(input.UploadData)
	category := strings.TrimSpace(toStringAny(standardized["type"], ""))
	title := strings.TrimSpace(input.Title + " " + input.Subtitle)
	switch category {
	case "category.movie":
		if regexp.MustCompile(`(?i)\b3D\b`).MatchString(title) {
			return "402"
		}
		return "401"
	case "category.tv_series":
		if regexp.MustCompile(`(?i)(complete|S\d{2}[^E])`).MatchString(title) && !regexp.MustCompile(`(?i)E\d+`).MatchString(title) {
			return "405"
		}
		return "412"
	case "category.music":
		if regexp.MustCompile(`(?i)音乐会|演唱会|concert`).MatchString(title) {
			return "419"
		}
		return "416"
	case "category.mv":
		return "414"
	case "category.tv_shows":
		return "413"
	case "category.documentaries":
		return "410"
	case "category.animation":
		return "411"
	case "category.sports":
		return "415"
	default:
		return ""
	}
}

func ourbitsStandardized(uploadData map[string]any) map[string]any {
	if uploadData == nil {
		return map[string]any{}
	}
	if standardized, ok := uploadData["standardized_params"].(map[string]any); ok && standardized != nil {
		return standardized
	}
	return map[string]any{}
}

// isOurBitsRemux 判断当前发布参数是否属于我堡禁止发布的 Remux 媒介。
func isOurBitsRemux(input publisher.PublishInput) bool {
	standardized := ourbitsStandardized(input.UploadData)
	medium := strings.ToLower(strings.TrimSpace(toStringAny(standardized["medium"], "")))
	if medium == "medium.remux" || strings.Contains(medium, "remux") {
		return true
	}
	title := strings.ToLower(strings.TrimSpace(input.Title + " " + input.Subtitle))
	return strings.Contains(title, "remux")
}

var reOurBitsSeason = regexp.MustCompile(`(?i)S\d+(-S\d+)?(E\d+)?|EP?\d+(-E?\d+)?`)

func extractOurBitsSeason(title string) string {
	return strings.TrimSpace(reOurBitsSeason.FindString(title))
}

func resolveOurBitsRecordEnd(title string) string {
	if regexp.MustCompile(`(?i)E\d+`).MatchString(title) {
		return "连载"
	}
	if regexp.MustCompile(`(?i)S\d+`).MatchString(title) {
		return "合集"
	}
	return "单集"
}

func resolveOurBitsRecordQuality(resolution string) string {
	switch strings.TrimSpace(resolution) {
	case "resolution.r2160p":
		return "2160p"
	case "resolution.r1080p":
		return "1080p"
	case "resolution.r1080i":
		return "1080i"
	case "resolution.r720p":
		return "720p"
	case "resolution.sd":
		return "480p"
	default:
		return ""
	}
}

func resolveOurBitsRecordMedium(medium string) string {
	switch strings.TrimSpace(medium) {
	case "medium.uhd_bluray", "medium.bluray", "medium.remux", "medium.encode":
		return "BluRay"
	case "medium.dvd":
		return "DVD"
	case "medium.hdtv":
		return "TV"
	case "medium.webdl":
		return "Web-DL"
	default:
		return ""
	}
}

func extractOurBitsGroup(title string) string {
	idx := strings.LastIndex(strings.TrimSpace(title), "-")
	if idx < 0 || idx+1 >= len(title) {
		return ""
	}
	return strings.TrimSpace(title[idx+1:])
}
