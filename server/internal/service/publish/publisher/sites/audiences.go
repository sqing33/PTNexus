package sites

import (
	"regexp"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

const audiencesPublishLogModule = "发布-Audiences"

// audiencesDoubanIDPattern 匹配豆瓣链接中的数字 ID。
var audiencesDoubanIDPattern = regexp.MustCompile(`(\d{5,})`)

type audiencesPublisher struct {
	publicSiteDefaults
}

// PublishAudiences 执行人人站点发布流程（豆瓣 ID 提取、MediaInfo 内嵌、冗余字段清理）。
func PublishAudiences(input publisher.PublishInput) (publisher.PublishResult, error) {
	return publishWithPublicSite(input, audiencesPublisher{})
}

func (audiencesPublisher) LogModule() string {
	return audiencesPublishLogModule
}

func (audiencesPublisher) BuildDescription(input publisher.PublishInput) string {
	uploadData := input.UploadData
	if uploadData == nil {
		uploadData = map[string]any{}
	}

	intro := map[string]any{}
	if item, ok := uploadData["intro"].(map[string]any); ok && item != nil {
		intro = item
	}

	statement := pickAudiencesDescriptionSection(uploadData, intro, "statement")
	poster := pickAudiencesDescriptionSection(uploadData, intro, "poster")
	body := pickAudiencesDescriptionSection(uploadData, intro, "body")
	screenshots := pickAudiencesDescriptionSection(uploadData, intro, "screenshots")
	mediainfo := strings.TrimSpace(toStringAny(uploadData["mediainfo"], ""))
	if mediainfo == "" {
		mediainfo = strings.TrimSpace(toStringAny(uploadData["media_info"], ""))
	}
	if mediainfo == "" {
		mediainfo = strings.TrimSpace(toStringAny(uploadData["mediainfo_text"], ""))
	}
	if mediainfo == "" {
		mediainfo = strings.TrimSpace(input.MediaInfo)
	}

	parts := make([]string, 0, 5)
	for _, section := range []string{statement, poster, body} {
		if strings.TrimSpace(section) != "" {
			parts = append(parts, section)
		}
	}

	if mediainfo != "" {
		// Audiences 统一使用 [Mediainfo] 标签，BDInfo 格式也放在其中
		parts = append(parts, "[Mediainfo]"+mediainfo+"[/Mediainfo]")
	}

	if screenshots != "" {
		parts = append(parts, screenshots)
	}

	if len(parts) == 0 {
		return strings.TrimSpace(toStringAny(uploadData["subtitle"], ""))
	}
	return strings.Join(parts, "\n")
}

func (audiencesPublisher) AdjustFormFields(input publisher.PublishInput, formFields map[string]string) {
	if formFields == nil {
		return
	}

	// --- 调试日志：打印发布前关键表单字段 ---
	descr := formFields["descr"]
	descrLen := len(descr)
	descrPreview := descr
	if descrLen > 500 {
		descrPreview = descr[:500] + "..."
	}
	hasMediainfoTag := strings.Contains(strings.ToLower(descr), "[mediainfo]")
	hasBDInfoTag := strings.Contains(strings.ToLower(descr), "[bdinfo]")
	logx.Infof(audiencesPublishLogModule, "descr 长度=%d 含[mediainfo]=%v 含[bdinfo]=%v", descrLen, hasMediainfoTag, hasBDInfoTag)
	logx.Infof(audiencesPublishLogModule, "descr 预览: %s", descrPreview)

	mediaInfoVal := strings.TrimSpace(input.MediaInfo)
	logx.Infof(audiencesPublishLogModule, "input.MediaInfo 长度=%d", len(mediaInfoVal))
	if mi, ok := input.UploadData["mediainfo"]; ok {
		logx.Infof(audiencesPublishLogModule, "uploadData[mediainfo] 存在, 类型=%T, 长度=%d", mi, len(strings.TrimSpace(toStringAny(mi, ""))))
	} else {
		logx.Infof(audiencesPublishLogModule, "uploadData[mediainfo] 不存在")
	}

	// 打印所有表单字段名和值（截断）
	for k, v := range formFields {
		vPreview := v
		if len(vPreview) > 120 {
			vPreview = vPreview[:120] + "..."
		}
		logx.Infof(audiencesPublishLogModule, "表单字段: %s = %s", k, vPreview)
	}
	// --- 调试日志结束 ---

	// DIY 标签时自动切换媒介为对应 DIY 选项
	hasDIY := false
	for k, v := range formFields {
		if strings.HasPrefix(k, "tags[") && strings.EqualFold(strings.TrimSpace(v), "diy") {
			hasDIY = true
			break
		}
	}
	if hasDIY {
		switch formFields["medium_sel"] {
		case "12": // UHD Blu-ray原盘 → UHD Blu-ray DIY
			formFields["medium_sel"] = "13"
		case "1": // Blu-ray原盘 → Blu-ray DIY
			formFields["medium_sel"] = "14"
		}
	}

	// dburl / pt_gen → douban_id：人人表单接收豆瓣数字 ID 而非完整链接
	if dburl := strings.TrimSpace(formFields["dburl"]); dburl != "" {
		if id := extractAudiencesDoubanID(dburl); id != "" {
			formFields["douban_id"] = id
		}
		delete(formFields, "dburl")
	}
	delete(formFields, "pt_gen")

	// MediaInfo 已内嵌到简介的 [Mediainfo] 标签内，移除独立字段
	delete(formFields, "technical_info")
}

func pickAudiencesDescriptionSection(uploadData map[string]any, intro map[string]any, key string) string {
	fromTop := strings.TrimSpace(toStringAny(uploadData[key], ""))
	if fromTop != "" {
		return fromTop
	}
	return strings.TrimSpace(toStringAny(intro[key], ""))
}

func extractAudiencesDoubanID(url string) string {
	matches := audiencesDoubanIDPattern.FindStringSubmatch(url)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}
