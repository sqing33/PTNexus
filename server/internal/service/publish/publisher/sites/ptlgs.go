package sites

import (
	"regexp"
	"strings"

	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

var rePTLGSImgTag = regexp.MustCompile(`(?i)\[/?img\]`)

// PublishPTLGS 执行 PTLGS 站点特殊发布流程（字段分离：封面与截图不进入 descr）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回发布详情页 URL、是否疑似“种子已存在”、用于自动编辑的表单字段，以及发布过程日志。
// 失败场景：同 Public 发布器；字段缺失会自动降级为空，不中断发布。
// 副作用：读取本地种子文件并向目标站点发起上传请求；可选写入 data/tmp/torrents 参数落盘。
func PublishPTLGS(input publisher.PublishInput) (publisher.PublishResult, error) {
	uploadData := input.UploadData

	cover, screenshots := buildPTLGSImageFields(uploadData)
	siteCfg, _ := publishmapping.LoadSitePublishConfig(strings.TrimSpace(input.SiteCode))
	resolveFieldName := func(mappingKey string, fallback string) string {
		if siteCfg == nil {
			return fallback
		}
		for _, key := range []string{mappingKey, fallback} {
			if resolved := strings.TrimSpace(siteCfg.FormFields[key]); resolved != "" {
				return resolved
			}
		}
		return fallback
	}
	extra := map[string]string{}
	if strings.TrimSpace(cover) != "" {
		extra[resolveFieldName("cover", "cover")] = strings.TrimSpace(cover)
	}
	if strings.TrimSpace(screenshots) != "" {
		extra[resolveFieldName("screenshots", "screenshots")] = strings.TrimSpace(screenshots)
	}

	next := input
	next.Description = strings.TrimSpace(buildPTLGSDescription(uploadData))
	next.ExtraFormFields = extra

	result, err := publisher.PublishPublic(next)
	prefix := "检测到 PTLGS 站点：启用特殊字段分离流程"
	if strings.TrimSpace(result.AttemptDetailLog) == "" {
		result.AttemptDetailLog = prefix
	} else {
		result.AttemptDetailLog = prefix + "\n" + strings.TrimSpace(result.AttemptDetailLog)
	}
	return result, err
}

func buildPTLGSDescription(uploadData map[string]any) string {
	return strings.TrimSpace(resolvePTLGSSection(uploadData, "statement"))
}

func buildPTLGSImageFields(uploadData map[string]any) (string, string) {
	cover := strings.TrimSpace(stripPTLGSImageBBCode(resolvePTLGSSection(uploadData, "poster")))
	screenshots := strings.TrimSpace(stripPTLGSImageBBCode(resolvePTLGSSection(uploadData, "screenshots")))
	return cover, screenshots
}

func resolvePTLGSSection(uploadData map[string]any, key string) string {
	if uploadData == nil {
		return ""
	}
	if fromTop := strings.TrimSpace(toStringAny(uploadData[key], "")); fromTop != "" {
		return fromTop
	}
	intro, _ := uploadData["intro"].(map[string]any)
	if intro == nil {
		return ""
	}
	return strings.TrimSpace(toStringAny(intro[key], ""))
}

func stripPTLGSImageBBCode(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	cleaned := rePTLGSImgTag.ReplaceAllString(text, "")
	return strings.TrimSpace(cleaned)
}
