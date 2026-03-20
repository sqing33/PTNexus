package sites

import (
	"regexp"
	"strings"

	publishmapping "github.com/pt-nexus/server/internal/service/publish/mapping"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

const cbgPublishLogModule = "发布-CBG"

var (
	reCBGEpisodeTokenStrong = regexp.MustCompile(`(?i)\bS\d{1,2}E\d{1,3}(?:\s*[-~]\s*(?:S?\d{1,2})?E?\d{1,3})?\b`)
	reCBGEpisodeTokenLoose  = regexp.MustCompile(`(?i)\b(?:E[Pp]?|Episode)\s*0?\d{1,3}\b`)
	reCBGEpisodeTokenCN     = regexp.MustCompile(`第\s*\d{1,4}\s*[集话話期]`)
)

// 定义 CBG 站点在公共表单发布流程上的差异步骤。
type cbgPublisher struct {
	publicSiteDefaults
}

// PublishCBG 执行 CBG 站点特殊发布流程（动画电影/动画剧集分类分流）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回公共发布器结果。
// 失败场景：公共发布器失败时返回 error。
// 副作用：调用公共发布器，并按 CBG 规则覆盖最终分类字段。
func PublishCBG(input publisher.PublishInput) (publisher.PublishResult, error) {
	return publishWithPublicSite(input, cbgPublisher{})
}

func (cbgPublisher) LogModule() string {
	return cbgPublishLogModule
}

func (cbgPublisher) AttemptPrefix(input publisher.PublishInput) string {
	return "检测到 CBG 站点：启用动画/动漫分类分流"
}

func (cbgPublisher) AdjustFormFields(input publisher.PublishInput, formFields map[string]string) {
	applyCBGAnimationCategoryOverride(input, formFields)
}

// 按 CBG 站点规则将动画类内容分流到“动画(404)”或“动漫(405)”。
func applyCBGAnimationCategoryOverride(input publisher.PublishInput, formFields map[string]string) {
	if formFields == nil {
		return
	}
	standardized, _ := input.UploadData["standardized_params"].(map[string]any)
	if strings.TrimSpace(toStringAny(standardized["type"], "")) != "category.animation" {
		return
	}

	siteCfg, err := publishmapping.LoadSitePublishConfig("cbg")
	if err != nil || siteCfg == nil {
		return
	}

	categoryField := strings.TrimSpace(siteCfg.FormFields["category"])
	if categoryField == "" {
		categoryField = "type"
	}
	animeValue := strings.TrimSpace(siteCfg.Mappings["type"]["category.anime"])
	animationValue := strings.TrimSpace(siteCfg.Mappings["type"]["category.animation"])
	if animeValue == "" || animationValue == "" {
		return
	}

	if hasCBGSeriesEvidence(input) {
		formFields[categoryField] = animationValue
		return
	}
	formFields[categoryField] = animeValue
}

// 判断当前动画类内容是否存在明确的多集/剧集证据。
func hasCBGSeriesEvidence(input publisher.PublishInput) bool {
	if titleComponents := parseTitleComponentsLocal(input.UploadData["title_components"]); len(titleComponents) > 0 {
		if seasonEpisode := strings.TrimSpace(findTitleComponentValue(titleComponents, "季集")); seasonEpisode != "" {
			return true
		}
	}

	tagSet := resolveSiteCombinedTags(input.UploadData)
	if hasAnySiteTagLower(tagSet, "tag.分集", "分集") {
		return true
	}

	for _, text := range []string{strings.TrimSpace(input.Title), strings.TrimSpace(input.Subtitle)} {
		if hasCBGEpisodeToken(text) {
			return true
		}
	}
	return false
}

// 判断标题文本中是否包含明显的集号特征。
func hasCBGEpisodeToken(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	return reCBGEpisodeTokenStrong.MatchString(trimmed) ||
		reCBGEpisodeTokenLoose.MatchString(trimmed) ||
		reCBGEpisodeTokenCN.MatchString(trimmed)
}
