package sites

import (
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

const pterclubPublishLogModule = "发布-PTerClub"

// 定义 PTerClub 站点在公共表单发布流程上的差异步骤。
type pterclubPublisher struct {
	publicSiteDefaults
}

// PublishPTerClub 执行 PTerClub 站点特殊发布流程（标签转独立 checkbox 字段）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回公共发布器结果。
// 失败场景：公共发布器失败时返回 error。
// 副作用：调用公共发布器并按站点规则重建标签字段。
func PublishPTerClub(input publisher.PublishInput) (publisher.PublishResult, error) {
	return publishWithPublicSite(input, pterclubPublisher{})
}

func (pterclubPublisher) LogModule() string {
	return pterclubPublishLogModule
}

func (pterclubPublisher) AttemptPrefix(input publisher.PublishInput) string {
	return "检测到 PTerClub 站点：启用独立标签 checkbox 映射"
}

func (pterclubPublisher) AdjustFormFields(input publisher.PublishInput, formFields map[string]string) {
	applyPTerClubCheckboxTags(input.UploadData, formFields)
}

// 将通用标签集合改写为 PTerClub 独立 checkbox 字段。
func applyPTerClubCheckboxTags(uploadData map[string]any, formFields map[string]string) {
	if formFields == nil {
		return
	}

	removeFormFieldsByPrefix(formFields, "tags[4][")
	tagSet := resolveSiteCombinedTags(uploadData)
	tagToField := map[string]string{
		"tag.禁转":   "jinzhuan",
		"禁转":       "jinzhuan",
		"tag.官方":   "guanfang",
		"官方":       "guanfang",
		"tag.国语":   "guoyu",
		"国语":       "guoyu",
		"tag.粤语":   "yueyu",
		"粤语":       "yueyu",
		"tag.中字":   "zhongzi",
		"中字":       "zhongzi",
		"tag.英字":   "ensub",
		"英字":       "ensub",
		"tag.应求":   "yingqiu",
		"应求":       "yingqiu",
		"tag.DIY":  "diy",
		"DIY":      "diy",
		"tag.原创":   "pr",
		"原创":       "pr",
		"tag.自购":   "bim",
		"自购":       "bim",
		"tag.MV母盘": "mp",
		"MV母盘":     "mp",
	}

	applied := make([]string, 0, 8)
	for tag, field := range tagToField {
		if _, ok := tagSet[tag]; !ok {
			continue
		}
		formFields[field] = "yes"
		applied = append(applied, field)
	}
	if len(applied) > 0 {
		logx.Infof(pterclubPublishLogModule, "PTerClub 独立标签字段已写入 count=%d fields=%s", len(applied), strings.Join(sortedUniqueStrings(applied), ","))
	}
}
