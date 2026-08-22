package sites

import (
	"strings"

	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

const xdyptPublishLogModule = "发布-修道院"

// 定义修道院站点在公共表单发布流程上的差异步骤。
type xdyptPublisher struct {
	publicSiteDefaults
}

// PublishXDYPT 执行修道院站点发布流程，复用 NexusPHP 公共上传并清理站点不支持的字段。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回发布详情页 URL、已存在标记、上传字段和过程日志。
// 失败场景：站点配置缺失、Cookie/种子文件缺失或上传接口返回错误时返回 error。
// 副作用：读取本地种子文件并向修道院 takeupload.php 发起上传请求。
func PublishXDYPT(input publisher.PublishInput) (publisher.PublishResult, error) {
	return publishWithPublicSite(input, xdyptPublisher{})
}

func (xdyptPublisher) LogModule() string {
	return xdyptPublishLogModule
}

func (xdyptPublisher) AttemptPrefix(input publisher.PublishInput) string {
	return "检测到修道院站点：启用带 [4] 下标的公共上传字段映射"
}

func (xdyptPublisher) BuildExtraFormFields(input publisher.PublishInput) (map[string]string, error) {
	extra := map[string]string{}
	if input.UploadData == nil {
		return extra, nil
	}

	// 修道院将英文名和中文名拆成两个字段；仅在上游明确提供中文名时填充 cnname。
	for _, key := range []string{"cnname", "chinese_title", "title_cn", "original_chinese_title"} {
		if value := strings.TrimSpace(toStringAny(input.UploadData[key], "")); value != "" {
			extra["cnname"] = value
			break
		}
	}
	return extra, nil
}

func (xdyptPublisher) AdjustFormFields(input publisher.PublishInput, formFields map[string]string) {
	if formFields == nil {
		return
	}

	// 页面没有独立音频编码和技术信息字段，相关内容已随简介/MediaInfo提交。
	delete(formFields, "audiocodec")
	delete(formFields, "audiocodec_sel")
	delete(formFields, "audiocodec_sel[4]")
	delete(formFields, "technical_info")
	delete(formFields, "title")
}
