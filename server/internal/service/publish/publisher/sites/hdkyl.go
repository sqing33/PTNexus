package sites

import (
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

const hdkylPublishLogModule = "发布-HDKyl"

// 定义 HDKyl 站点在公共表单发布流程上的差异步骤。
type hdkylPublisher struct {
	publicSiteDefaults
}

// PublishHDKyl 执行 HDKyl 站点特殊发布流程（年份字段映射）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回公共发布器结果。
// 失败场景：公共发布器失败时返回 error。
// 副作用：调用公共发布器，并补充 year 对应的 processing 字段。
func PublishHDKyl(input publisher.PublishInput) (publisher.PublishResult, error) {
	return publishWithPublicSite(input, hdkylPublisher{})
}

func (hdkylPublisher) LogModule() string {
	return hdkylPublishLogModule
}

func (hdkylPublisher) AttemptPrefix(input publisher.PublishInput) string {
	return "检测到 HDKyl 站点：启用年份字段映射"
}

func (hdkylPublisher) BuildExtraFormFields(input publisher.PublishInput) (map[string]string, error) {
	return buildHDKylYearFields(input), nil
}

// 提取年份并映射到 HDKyl 站点的独立年份字段。
func buildHDKylYearFields(input publisher.PublishInput) map[string]string {
	siteCfg, err := publishmapping.LoadSitePublishConfig("hdkyl")
	if err != nil || siteCfg == nil {
		return nil
	}
	yearField := strings.TrimSpace(siteCfg.FormFields["year"])
	if yearField == "" {
		yearField = "processing_sel[4]"
	}
	yearKey := strings.TrimSpace(resolveHDKylYearKey(input.UploadData))
	if yearKey == "" {
		yearKey = "year.earlier"
	}
	mappedValue := strings.TrimSpace(publishmapping.PickMappedValue(siteCfg.Mappings["year"], yearKey))
	if mappedValue == "" {
		return nil
	}
	logx.Infof(hdkylPublishLogModule, "HDKyl 年份字段映射 year=%s mapped=%s", yearKey, mappedValue)
	return map[string]string{yearField: mappedValue}
}

// 从标题组件和原始参数中解析 year.* 映射键。
func resolveHDKylYearKey(uploadData map[string]any) string {
	if uploadData == nil {
		return ""
	}
	if titleComponents := parseTitleComponentsLocal(uploadData["title_components"]); len(titleComponents) > 0 {
		if yearText := strings.TrimSpace(findTitleComponentValue(titleComponents, "年份")); yearText != "" {
			if year := extractFourDigitYear(yearText); year != "" {
				return "year." + year
			}
		}
	}
	if year := extractFourDigitYear(toStringAny(uploadData["year"], "")); year != "" {
		return "year." + year
	}
	if sourceParams, ok := uploadData["source_params"].(map[string]any); ok && sourceParams != nil {
		if year := extractFourDigitYear(toStringAny(sourceParams["年代"], "")); year != "" {
			return "year." + year
		}
	}
	return ""
}
