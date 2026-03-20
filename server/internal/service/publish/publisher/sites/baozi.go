package sites

import (
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

const baoziPublishLogModule = "发布-包子"

// 定义 Baozi 站点在公共表单发布流程上的差异步骤。
type baoziPublisher struct {
	publicSiteDefaults
}

// PublishBaozi 执行 Baozi 站点特殊发布流程（DIY 标签触发原盘媒介修正）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回公共发布器结果。
// 失败场景：公共发布器失败时返回 error。
// 副作用：调用公共发布器并按站点规则改写最终媒介字段。
func PublishBaozi(input publisher.PublishInput) (publisher.PublishResult, error) {
	return publishWithPublicSite(input, baoziPublisher{})
}

func (baoziPublisher) LogModule() string {
	return baoziPublishLogModule
}

func (baoziPublisher) AttemptPrefix(input publisher.PublishInput) string {
	return "检测到 Baozi 站点：启用 DIY 媒介修正"
}

func (baoziPublisher) AdjustFormFields(input publisher.PublishInput, formFields map[string]string) {
	applyBaoziDIYMediumOverride(input.UploadData, formFields)
}

// 根据 DIY 标签将原盘媒介切换为 *_diy 映射值。
func applyBaoziDIYMediumOverride(uploadData map[string]any, formFields map[string]string) {
	if formFields == nil {
		return
	}
	tags := resolveSiteCombinedTags(uploadData)
	if !hasAnySiteTagLower(tags, "tag.diy", "diy") {
		return
	}

	siteCfg, err := publishmapping.LoadSitePublishConfig("baozi")
	if err != nil || siteCfg == nil {
		return
	}
	mediumField := strings.TrimSpace(siteCfg.FormFields["medium"])
	if mediumField == "" {
		mediumField = "medium_sel[4]"
	}
	currentValue := strings.TrimSpace(formFields[mediumField])
	if currentValue == "" {
		return
	}

	blurayValue := strings.TrimSpace(siteCfg.Mappings["medium"]["medium.bluray"])
	blurayDIYValue := strings.TrimSpace(siteCfg.Mappings["medium"]["medium.bluray_diy"])
	uhdValue := strings.TrimSpace(siteCfg.Mappings["medium"]["medium.uhd_bluray"])
	uhdDIYValue := strings.TrimSpace(siteCfg.Mappings["medium"]["medium.uhd_diy"])

	switch currentValue {
	case blurayValue:
		if blurayDIYValue != "" {
			formFields[mediumField] = blurayDIYValue
			logx.Infof(baoziPublishLogModule, "检测到 DIY 标签，包子媒介改写 bluray=%s -> diy=%s", blurayValue, blurayDIYValue)
		}
	case uhdValue:
		if uhdDIYValue != "" {
			formFields[mediumField] = uhdDIYValue
			logx.Infof(baoziPublishLogModule, "检测到 DIY 标签，包子媒介改写 uhd=%s -> diy=%s", uhdValue, uhdDIYValue)
		}
	}
}
