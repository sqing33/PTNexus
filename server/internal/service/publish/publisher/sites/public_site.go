package sites

import (
	"fmt"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

// 定义公共表单站点的可覆写步骤。
// 说明：站点文件只实现差异步骤，主流程仍复用公共上传底座。
type publicSitePublisher interface {
	LogModule() string
	AttemptPrefix(input publisher.PublishInput) string
	BuildDescription(input publisher.PublishInput) string
	BuildExtraFormFields(input publisher.PublishInput) (map[string]string, error)
	AdjustFormFields(input publisher.PublishInput, formFields map[string]string)
}

// 提供公共表单站点的默认步骤实现。
type publicSiteDefaults struct{}

func (publicSiteDefaults) LogModule() string {
	return ""
}

func (publicSiteDefaults) AttemptPrefix(input publisher.PublishInput) string {
	return ""
}

func (publicSiteDefaults) BuildDescription(input publisher.PublishInput) string {
	return strings.TrimSpace(input.Description)
}

func (publicSiteDefaults) BuildExtraFormFields(input publisher.PublishInput) (map[string]string, error) {
	return nil, nil
}

func (publicSiteDefaults) AdjustFormFields(input publisher.PublishInput, formFields map[string]string) {
}

// 按站点覆写步骤执行公共表单发布流程。
// 参数/返回：input 为统一发布输入；site 提供站点差异步骤；返回公共发布结果。
// 失败场景：站点额外字段构造失败或公共上传失败时返回 error。
// 副作用：可能记录站点日志、调整最终表单字段，并调用公共上传器发起真实发布。
func publishWithPublicSite(input publisher.PublishInput, site publicSitePublisher) (publisher.PublishResult, error) {
	next := input
	next.Description = strings.TrimSpace(site.BuildDescription(input))

	extra, err := site.BuildExtraFormFields(input)
	if err != nil {
		if module := strings.TrimSpace(site.LogModule()); module != "" {
			logx.Errorf(module, "构造站点额外字段失败 site=%s err=%v", strings.TrimSpace(input.SiteCode), err)
		}
		detail := strings.TrimSpace(site.AttemptPrefix(input))
		if detail == "" {
			detail = fmt.Sprintf("检测到 %s 站点：构造发布字段失败", strings.TrimSpace(input.SiteCode))
		}
		return publisher.PublishResult{AttemptDetailLog: detail}, err
	}
	next.ExtraFormFields = mergeStringMap(input.ExtraFormFields, extra)

	prevAdjust := input.AdjustFormFields
	next.AdjustFormFields = func(formFields map[string]string) {
		if prevAdjust != nil {
			prevAdjust(formFields)
		}
		site.AdjustFormFields(input, formFields)
	}

	result, publishErr := publisher.PublishPublic(next)
	prefix := strings.TrimSpace(site.AttemptPrefix(input))
	if prefix != "" {
		if strings.TrimSpace(result.AttemptDetailLog) == "" {
			result.AttemptDetailLog = prefix
		} else {
			result.AttemptDetailLog = prefix + "\n" + strings.TrimSpace(result.AttemptDetailLog)
		}
	}
	return result, publishErr
}

// 合并站点附加表单字段并过滤空键值。
func mergeStringMap(base map[string]string, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	result := map[string]string{}
	for key, value := range base {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		result[trimmedKey] = trimmedValue
	}
	for key, value := range extra {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		result[trimmedKey] = trimmedValue
	}
	return result
}
