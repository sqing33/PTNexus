package sites

import (
	"strings"

	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

const (
	crabptBrowseCategoryField = "id"
	crabptBrowseCategoryValue = "browsecat"
	crabptModeField           = "data-mode"
	crabptModeValue           = "4"
)

// PublishCrabPT 执行 CrabPT 站点特殊发布流程（固定普通区）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回发布详情页 URL、是否疑似“种子已存在”、用于自动编辑的表单字段，以及发布过程日志。
// 失败场景：同 Public 发布器；若站点配置缺失或上传失败则返回对应 error。
// 副作用：读取本地种子文件并向目标站点发起上传请求；始终按普通区表单模式提交。
func PublishCrabPT(input publisher.PublishInput) (publisher.PublishResult, error) {
	next := input
	prevAdjust := input.AdjustFormFields
	next.AdjustFormFields = func(formFields map[string]string) {
		if prevAdjust != nil {
			prevAdjust(formFields)
		}
		applyCrabPTRegularMode(formFields)
	}

	result, publishErr := publisher.PublishPublic(next)
	prefix := "检测到 CrabPT 站点：固定使用普通区发布模式"
	if strings.TrimSpace(result.AttemptDetailLog) == "" {
		result.AttemptDetailLog = prefix
	} else {
		result.AttemptDetailLog = prefix + "\n" + strings.TrimSpace(result.AttemptDetailLog)
	}
	return result, publishErr
}

func applyCrabPTRegularMode(formFields map[string]string) {
	if formFields == nil {
		return
	}
	formFields[crabptBrowseCategoryField] = crabptBrowseCategoryValue
	formFields[crabptModeField] = crabptModeValue
}
