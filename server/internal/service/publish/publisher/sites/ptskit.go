package sites

import (
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

const ptskitPublishLogModule = "发布-PTSKit"

// 定义 PTSKit 站点在公共表单发布流程上的差异步骤。
type ptskitPublisher struct {
	publicSiteDefaults
}

// PublishPTSKit 执行 PTSKit 站点特殊发布流程（转载标签必选且仅保留白名单类型标签）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回公共发布器结果。
// 失败场景：公共发布器失败时返回 error。
// 副作用：调用公共发布器并重建最终标签字段。
func PublishPTSKit(input publisher.PublishInput) (publisher.PublishResult, error) {
	return publishWithPublicSite(input, ptskitPublisher{})
}

func (ptskitPublisher) LogModule() string {
	return ptskitPublishLogModule
}

func (ptskitPublisher) AttemptPrefix(input publisher.PublishInput) string {
	return "检测到 PTSKit 站点：启用白名单标签映射"
}

func (ptskitPublisher) AdjustFormFields(input publisher.PublishInput, formFields map[string]string) {
	applyPTSKitTags(input.UploadData, formFields)
}

// 重建 PTSKit 的标签集合，只保留转载和类型白名单标签。
func applyPTSKitTags(uploadData map[string]any, formFields map[string]string) {
	if formFields == nil {
		return
	}
	siteCfg, err := publishmapping.LoadSitePublishConfig("ptskit")
	if err != nil || siteCfg == nil {
		return
	}
	standardized, _ := uploadData["standardized_params"].(map[string]any)
	typeKey := strings.TrimSpace(toStringAny(standardized["type"], ""))

	tagIDs := []string{}
	if reprintID := strings.TrimSpace(siteCfg.Mappings["tag"]["tag.转载"]); reprintID != "" {
		tagIDs = append(tagIDs, reprintID)
	}
	switch typeKey {
	case "category.movie":
		tagIDs = append(tagIDs, strings.TrimSpace(siteCfg.Mappings["tag"]["tag.电影"]))
	case "category.tv_series":
		tagIDs = append(tagIDs, strings.TrimSpace(siteCfg.Mappings["tag"]["tag.电视剧"]))
	case "category.tv_shows":
		tagIDs = append(tagIDs, strings.TrimSpace(siteCfg.Mappings["tag"]["tag.综艺"]))
	case "category.game":
		tagIDs = append(tagIDs, strings.TrimSpace(siteCfg.Mappings["tag"]["tag.游戏"]))
	case "category.music":
		tagIDs = append(tagIDs, strings.TrimSpace(siteCfg.Mappings["tag"]["tag.音乐"]))
	case "category.animation":
		tagIDs = append(tagIDs, strings.TrimSpace(siteCfg.Mappings["tag"]["tag.动漫"]))
	default:
		tagIDs = append(tagIDs, strings.TrimSpace(siteCfg.Mappings["tag"]["tag.其他"]))
	}

	rebuildIndexedFormFields(formFields, "tags[4]", sortedUniqueStrings(tagIDs))
	logx.Infof(ptskitPublishLogModule, "PTSKit 标签已重建 type=%s count=%d", typeKey, len(sortedUniqueStrings(tagIDs)))
}
