package sites

import (
	"regexp"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

const hdhomePublishLogModule = "发布-家园"

// 定义家园站点在公共表单发布流程上的差异步骤。
type hdhomePublisher struct {
	publicSiteDefaults
}

// PublishHDHome 执行家园站点特殊发布流程。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回公共发布器结果。
// 失败场景：公共发布器失败时返回 error。
// 副作用：调用公共发布器，并按家园上传表单修正分类、来源、处理、制作组与编码字段。
func PublishHDHome(input publisher.PublishInput) (publisher.PublishResult, error) {
	return publishWithPublicSite(input, hdhomePublisher{})
}

func (hdhomePublisher) LogModule() string {
	return hdhomePublishLogModule
}

func (hdhomePublisher) AttemptPrefix(input publisher.PublishInput) string {
	return "检测到家园站点：启用细分类与来源/处理字段映射"
}

func (hdhomePublisher) BuildDescription(input publisher.PublishInput) string {
	return buildHDHomeDescription(input)
}

func (hdhomePublisher) BuildExtraFormFields(input publisher.PublishInput) (map[string]string, error) {
	return buildHDHomeExtraFields(input), nil
}

func (hdhomePublisher) AdjustFormFields(input publisher.PublishInput, formFields map[string]string) {
	ensureHDHomeRequiredFields(input, formFields)
	adjustHDHomeCategory(input, formFields)
	adjustHDHomeCodec(input, formFields)
	adjustHDHomeAudio(input, formFields)
}

// buildHDHomeExtraFields 构造家园上传页独立的来源、处理与制作组字段。
func buildHDHomeExtraFields(input publisher.PublishInput) map[string]string {
	siteCfg, err := publishmapping.LoadSitePublishConfig("hdhome")
	if err != nil || siteCfg == nil {
		return nil
	}
	standardized := hdhomeStandardized(input)
	medium := strings.TrimSpace(toStringAny(standardized["medium"], ""))

	result := map[string]string{}
	sourceField := firstNonEmpty(siteCfg.FormFields["source"], "source_sel")
	if value := resolveHDHomeSourceValue(medium); value != "" {
		result[sourceField] = value
	}
	processingField := firstNonEmpty(siteCfg.FormFields["processing"], "processing_sel")
	if value := resolveHDHomeProcessingValue(medium); value != "" {
		result[processingField] = value
	}
	teamField := firstNonEmpty(siteCfg.FormFields["team"], "team_sel")
	result[teamField] = "@index:11"
	return result
}

// adjustHDHomeCategory 按家园分类树把标准类型细分为原盘、Remux、4K、Pad 等具体分区。
func adjustHDHomeCategory(input publisher.PublishInput, formFields map[string]string) {
	if formFields == nil {
		return
	}
	siteCfg, err := publishmapping.LoadSitePublishConfig("hdhome")
	if err != nil || siteCfg == nil {
		return
	}
	categoryField := firstNonEmpty(siteCfg.FormFields["category"], "type")
	category := resolveHDHomeCategory(input)
	if category == "" {
		return
	}
	formFields[categoryField] = category
	logx.Infof(hdhomePublishLogModule, "家园分类映射 category=%s title=%s", category, strings.TrimSpace(input.Title))
}

// ensureHDHomeRequiredFields 兜底填充家园上传页必填的基础下拉字段。
func ensureHDHomeRequiredFields(input publisher.PublishInput, formFields map[string]string) {
	if formFields == nil {
		return
	}
	siteCfg, err := publishmapping.LoadSitePublishConfig("hdhome")
	if err != nil || siteCfg == nil {
		return
	}
	standardized := hdhomeStandardized(input)
	applyMapped := func(mappingKey string, formKey string, fallbackField string, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		field := firstNonEmpty(siteCfg.FormFields[formKey], fallbackField)
		if strings.TrimSpace(formFields[field]) != "" {
			return
		}
		mapped := strings.TrimSpace(publishmapping.PickMappedValue(siteCfg.Mappings[mappingKey], value))
		if mapped == "" {
			return
		}
		formFields[field] = mapped
	}
	applyMapped("medium", "medium", "medium_sel", toStringAny(standardized["medium"], ""))
	applyMapped("resolution", "resolution", "standard_sel", toStringAny(standardized["resolution"], ""))
	applyMapped("video_codec", "video_codec", "codec_sel", toStringAny(standardized["video_codec"], ""))
	applyMapped("audio_codec", "audio_codec", "audiocodec_sel", toStringAny(standardized["audio_codec"], ""))
}

func adjustHDHomeCodec(input publisher.PublishInput, formFields map[string]string) {
	if formFields == nil {
		return
	}
	standardized := hdhomeStandardized(input)
	video := strings.TrimSpace(toStringAny(standardized["video_codec"], ""))
	if video != "video.h265" && video != "video.x265" && video != "video.hevc" {
		return
	}
	if !strings.Contains(strings.ToLower(input.Title), "hevc") {
		return
	}
	formFields["codec_sel"] = "@index:6"
}

func adjustHDHomeAudio(input publisher.PublishInput, formFields map[string]string) {
	if formFields == nil {
		return
	}
	standardized := hdhomeStandardized(input)
	audio := strings.TrimSpace(toStringAny(standardized["audio_codec"], ""))
	if audio != "audio.dts_hd_ma" && audio != "audio.dtsx" {
		return
	}
	title := strings.ToLower(input.Title)
	if strings.Contains(title, "x 7.1") || strings.Contains(title, "x7.1") || strings.Contains(title, "ma 7.1") || strings.Contains(title, "ma.7.1") {
		formFields["audiocodec_sel"] = "@index:12"
	}
}

// buildHDHomeDescription 按家园要求把 MediaInfo 放在截图与简介详情中间。
func buildHDHomeDescription(input publisher.PublishInput) string {
	statement := resolveUploadSection(input.UploadData, "statement")
	poster := resolveUploadSection(input.UploadData, "poster")
	screenshots := resolveUploadSection(input.UploadData, "screenshots")
	mediainfo := firstNonEmpty(input.MediaInfo, toStringAny(input.UploadData["mediainfo"], ""))
	body := resolveUploadSection(input.UploadData, "body")

	parts := make([]string, 0, 5)
	for _, section := range []string{statement, poster, screenshots} {
		if strings.TrimSpace(section) != "" {
			parts = append(parts, strings.TrimSpace(section))
		}
	}
	if strings.TrimSpace(mediainfo) != "" {
		parts = append(parts, "[quote]"+strings.TrimSpace(mediainfo)+"[/quote]")
	}
	if strings.TrimSpace(body) != "" {
		parts = append(parts, strings.TrimSpace(body))
	}
	if len(parts) == 0 {
		return strings.TrimSpace(firstNonEmpty(input.Description, input.Subtitle))
	}
	return strings.Join(parts, "\n")
}

func resolveHDHomeSourceValue(medium string) string {
	switch strings.TrimSpace(medium) {
	case "medium.uhd_bluray", "medium.uhd_diy":
		return "@index:1"
	case "medium.bluray", "medium.bluray_diy", "medium.remux", "medium.encode", "medium.encode_2160p", "medium.encode_1080p", "medium.encode_720p":
		return "@index:2"
	case "medium.hdtv":
		return "@index:3"
	case "medium.dvd":
		return "@index:4"
	case "medium.webdl":
		return "@index:5"
	default:
		return "@index:6"
	}
}

func resolveHDHomeProcessingValue(medium string) string {
	switch strings.TrimSpace(medium) {
	case "medium.uhd_bluray", "medium.uhd_diy", "medium.bluray", "medium.bluray_diy":
		return "@index:1"
	case "medium.encode", "medium.encode_2160p", "medium.encode_1080p", "medium.encode_720p":
		return "@index:2"
	default:
		return "@index:0"
	}
}

func resolveHDHomeCategory(input publisher.PublishInput) string {
	standardized := hdhomeStandardized(input)
	category := strings.TrimSpace(toStringAny(standardized["type"], ""))
	medium := strings.TrimSpace(toStringAny(standardized["medium"], ""))
	resolution := strings.TrimSpace(toStringAny(standardized["resolution"], ""))
	title := strings.ToLower(strings.TrimSpace(input.Title))
	description := strings.ToLower(firstNonEmpty(input.Description, input.MediaInfo))
	isPad := strings.Contains(title, "ipad") || strings.HasSuffix(title, "pad")

	switch category {
	case "category.movie":
		return hdhomeCategoryByProfile(medium, resolution, isPad, "450", "499", "415", "412", "413", "414", "416", "411", "414")
	case "category.tv_series":
		return hdhomeCategoryByProfile(medium, resolution, isPad, "453", "502", "437", "433", "434", "436", "438", "432", "436")
	case "category.music":
		if strings.Contains(title, "ape") {
			return "439"
		}
		if strings.Contains(title, "flac") {
			return "440"
		}
		if strings.Contains(description, "mpls") {
			return "503"
		}
		return "440"
	case "category.mv":
		return "441"
	case "category.tv_shows":
		return hdhomeCategoryByProfile(medium, resolution, isPad, "452", "", "430", "426", "427", "429", "431", "425", "429")
	case "category.documentaries":
		return hdhomeCategoryByProfile(medium, resolution, isPad, "451", "500", "421", "418", "419", "420", "422", "417", "420")
	case "category.animation":
		return hdhomeCategoryByProfile(medium, resolution, isPad, "454", "501", "448", "445", "446", "447", "449", "444", "447")
	case "category.study":
		return "409"
	case "category.sports":
		if resolution == "resolution.r720p" {
			return "442"
		}
		return "443"
	default:
		return "409"
	}
}

func hdhomeCategoryByProfile(medium string, resolution string, isPad bool, bluray string, uhd string, remux string, pad string, r720 string, r1080 string, r2160 string, sd string, fallback string) string {
	switch medium {
	case "medium.bluray", "medium.bluray_diy":
		return firstNonEmpty(bluray, fallback)
	case "medium.uhd_bluray", "medium.uhd_diy":
		return firstNonEmpty(uhd, bluray, fallback)
	case "medium.remux":
		return firstNonEmpty(remux, fallback)
	}
	if isPad {
		return firstNonEmpty(pad, fallback)
	}
	switch resolution {
	case "resolution.r720p":
		return firstNonEmpty(r720, fallback)
	case "resolution.r1080i", "resolution.r1080p":
		return firstNonEmpty(r1080, fallback)
	case "resolution.r2160p", "resolution.r4320p":
		return firstNonEmpty(r2160, fallback)
	case "resolution.sd":
		return firstNonEmpty(sd, fallback)
	default:
		return fallback
	}
}

func hdhomeStandardized(input publisher.PublishInput) map[string]any {
	uploadData := input.UploadData
	title := strings.TrimSpace(input.Title)
	if uploadData == nil {
		return inferHDHomeStandardizedFromTitle(title)
	}
	result := map[string]any{}
	if standardized, ok := uploadData["standardized_params"].(map[string]any); ok && standardized != nil {
		for key, value := range standardized {
			result[key] = value
		}
	}
	if inferred, ok := uploadData["inferred_standardized_params"].(map[string]any); ok && inferred != nil {
		for key, value := range inferred {
			if strings.TrimSpace(toStringAny(result[key], "")) == "" {
				result[key] = value
			}
		}
	}
	for key, candidates := range map[string][]string{
		"type":        {"type", "category"},
		"medium":      {"medium"},
		"resolution":  {"resolution"},
		"video_codec": {"video_codec", "videoCodec"},
		"audio_codec": {"audio_codec", "audioCodec"},
	} {
		if strings.TrimSpace(toStringAny(result[key], "")) != "" {
			continue
		}
		for _, candidate := range candidates {
			if value := strings.TrimSpace(toStringAny(uploadData[candidate], "")); value != "" {
				result[key] = value
				break
			}
		}
	}
	inferred := inferHDHomeStandardizedFromTitle(firstNonEmpty(title, toStringAny(uploadData["name"], "")))
	for key, value := range inferred {
		if strings.TrimSpace(toStringAny(result[key], "")) == "" {
			result[key] = value
		}
	}
	return result
}

var hdhomeSeasonPattern = regexp.MustCompile(`(?i)(^|[.\s_-])S\d{1,2}([E.\s_-]|\d|$)`)

func inferHDHomeStandardizedFromTitle(title string) map[string]any {
	text := strings.ToLower(strings.TrimSpace(title))
	result := map[string]any{}
	if text == "" {
		return result
	}
	if hdhomeSeasonPattern.MatchString(title) {
		result["type"] = "category.tv_series"
	} else {
		result["type"] = "category.movie"
	}
	switch {
	case strings.Contains(text, "web-dl") || strings.Contains(text, "webdl"):
		result["medium"] = "medium.webdl"
	case strings.Contains(text, "remux"):
		result["medium"] = "medium.remux"
	case strings.Contains(text, "hdtv"):
		result["medium"] = "medium.hdtv"
	case strings.Contains(text, "blu-ray") || strings.Contains(text, "bluray"):
		if strings.Contains(text, "2160p") || strings.Contains(text, "uhd") {
			result["medium"] = "medium.uhd_bluray"
		} else {
			result["medium"] = "medium.bluray"
		}
	case strings.Contains(text, "dvd"):
		result["medium"] = "medium.dvd"
	case strings.Contains(text, "cd"):
		result["medium"] = "medium.cd"
	}
	switch {
	case strings.Contains(text, "4320p") || strings.Contains(text, "8k"):
		result["resolution"] = "resolution.r4320p"
	case strings.Contains(text, "2160p") || strings.Contains(text, "4k"):
		result["resolution"] = "resolution.r2160p"
	case strings.Contains(text, "1080i"):
		result["resolution"] = "resolution.r1080i"
	case strings.Contains(text, "1080p"):
		result["resolution"] = "resolution.r1080p"
	case strings.Contains(text, "720p"):
		result["resolution"] = "resolution.r720p"
	}
	switch {
	case strings.Contains(text, "h.265") || strings.Contains(text, "h265") || strings.Contains(text, "x265") || strings.Contains(text, "hevc"):
		result["video_codec"] = "video.h265"
	case strings.Contains(text, "h.264") || strings.Contains(text, "h264") || strings.Contains(text, "x264") || strings.Contains(text, "avc"):
		result["video_codec"] = "video.h264"
	case strings.Contains(text, "vc-1") || strings.Contains(text, "vc1"):
		result["video_codec"] = "video.vc1"
	case strings.Contains(text, "mpeg-2") || strings.Contains(text, "mpeg2"):
		result["video_codec"] = "video.mpeg2"
	}
	switch {
	case strings.Contains(text, "atmos"):
		result["audio_codec"] = "audio.truehd_atmos"
	case strings.Contains(text, "ddp") || strings.Contains(text, "e-ac-3") || strings.Contains(text, "eac3"):
		result["audio_codec"] = "audio.ddp"
	case strings.Contains(text, "truehd"):
		result["audio_codec"] = "audio.truehd"
	case strings.Contains(text, "dts-hd") || strings.Contains(text, "dtshd"):
		result["audio_codec"] = "audio.dts_hd"
	case strings.Contains(text, "dts"):
		result["audio_codec"] = "audio.dts"
	case strings.Contains(text, "aac"):
		result["audio_codec"] = "audio.aac"
	case strings.Contains(text, "flac"):
		result["audio_codec"] = "audio.flac"
	case strings.Contains(text, "ac3") || strings.Contains(text, "dd5"):
		result["audio_codec"] = "audio.ac3"
	}
	return result
}
