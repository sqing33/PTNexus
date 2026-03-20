package sites

import (
	"regexp"
	"strings"

	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

var rePTLGSImgTag = regexp.MustCompile(`(?i)\[/?img\]`)

// 定义 PTLGS 站点在公共表单发布流程上的差异步骤。
type ptlgsPublisher struct {
	publicSiteDefaults
}

// PublishPTLGS 执行 PTLGS 站点特殊发布流程（字段分离：封面与截图不进入 descr）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回发布详情页 URL、是否疑似“种子已存在”、用于自动编辑的表单字段，以及发布过程日志。
// 失败场景：同 Public 发布器；字段缺失会自动降级为空，不中断发布。
// 副作用：读取本地种子文件并向目标站点发起上传请求；可选写入 data/tmp/torrents 参数落盘。
func PublishPTLGS(input publisher.PublishInput) (publisher.PublishResult, error) {
	return publishWithPublicSite(input, ptlgsPublisher{})
}

func (ptlgsPublisher) AttemptPrefix(input publisher.PublishInput) string {
	return "检测到 PTLGS 站点：启用特殊字段分离流程"
}

func (ptlgsPublisher) BuildDescription(input publisher.PublishInput) string {
	return strings.TrimSpace(buildPTLGSDescription(input.UploadData))
}

func (ptlgsPublisher) BuildExtraFormFields(input publisher.PublishInput) (map[string]string, error) {
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
	return extra, nil
}

// 构造 PTLGS 描述，只保留声明区块。
func buildPTLGSDescription(uploadData map[string]any) string {
	return strings.TrimSpace(resolvePTLGSSection(uploadData, "statement"))
}

// 提取 PTLGS 需要独立提交的封面与截图字段。
func buildPTLGSImageFields(uploadData map[string]any) (string, string) {
	cover := strings.TrimSpace(stripPTLGSImageBBCode(resolvePTLGSSection(uploadData, "poster")))
	screenshots := strings.TrimSpace(stripPTLGSImageBBCode(resolvePTLGSSection(uploadData, "screenshots")))
	return cover, screenshots
}

// 从顶层或 intro 区块读取 PTLGS 描述片段。
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

// 去除 PTLGS 图片字段中的 img BBCode 包装。
func stripPTLGSImageBBCode(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	cleaned := rePTLGSImgTag.ReplaceAllString(text, "")
	return strings.TrimSpace(cleaned)
}
