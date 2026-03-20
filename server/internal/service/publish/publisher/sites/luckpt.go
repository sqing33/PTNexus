package sites

import (
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

const luckptPublishLogModule = "发布-LuckPT"

// 定义 LuckPT 站点在公共表单发布流程上的差异步骤。
type luckptPublisher struct {
	publicSiteDefaults
}

// PublishLuckPT 执行 LuckPT 站点特殊发布流程（中文字幕/国语/粤语存在时移除英语标签）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回公共发布器结果。
// 失败场景：公共发布器失败时返回 error。
// 副作用：调用公共发布器并在站点层重排标签字段。
func PublishLuckPT(input publisher.PublishInput) (publisher.PublishResult, error) {
	return publishWithPublicSite(input, luckptPublisher{})
}

func (luckptPublisher) LogModule() string {
	return luckptPublishLogModule
}

func (luckptPublisher) AttemptPrefix(input publisher.PublishInput) string {
	return "检测到 LuckPT 站点：启用英语标签过滤"
}

func (luckptPublisher) AdjustFormFields(input publisher.PublishInput, formFields map[string]string) {
	applyLuckPTTagFilter(formFields)
}

// 在存在中文语境标签时移除英语标签，避免 LuckPT 标签冲突。
func applyLuckPTTagFilter(formFields map[string]string) {
	if formFields == nil {
		return
	}
	siteCfg, err := publishmapping.LoadSitePublishConfig("luckpt")
	if err != nil || siteCfg == nil {
		return
	}

	tagBase := "tags[4]"
	englishID := strings.TrimSpace(siteCfg.Mappings["tag"]["tag.英语"])
	mandarinID := strings.TrimSpace(siteCfg.Mappings["tag"]["tag.国语"])
	chineseSubID := strings.TrimSpace(siteCfg.Mappings["tag"]["tag.中字"])
	cantoneseID := strings.TrimSpace(siteCfg.Mappings["tag"]["tag.粤语"])

	values := make([]string, 0, 8)
	hasChineseContext := false
	for key, value := range formFields {
		if !strings.HasPrefix(strings.TrimSpace(key), tagBase+"[") {
			continue
		}
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue == "" {
			continue
		}
		if trimmedValue == mandarinID || trimmedValue == chineseSubID || trimmedValue == cantoneseID {
			hasChineseContext = true
		}
		values = append(values, trimmedValue)
	}
	if !hasChineseContext || englishID == "" {
		return
	}

	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value == englishID {
			continue
		}
		filtered = append(filtered, value)
	}
	rebuildIndexedFormFields(formFields, tagBase, filtered)
	logx.Infof(luckptPublishLogModule, "LuckPT 已移除英语标签 english=%s remain=%d", englishID, len(filtered))
}
