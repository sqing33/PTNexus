package sites

import (
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service/publish/mapping"
	"github.com/pt-nexus/server/internal/service/publish/publisher"
)

const hdvideoPublishLogModule = "发布-HDVideo"

// 定义 HDVideo 站点在公共表单发布流程上的差异步骤。
type hdvideoPublisher struct {
	publicSiteDefaults
}

// PublishHDVideo 执行 HDVideo 站点特殊发布流程（确保归属地 region 字段被正确填充）。
// 参数/返回：input 提供站点信息、发布数据与通用字段；返回公共发布器结果。
// 失败场景：公共发布器失败时返回 error。
// 副作用：调用公共发布器并在表单字段中补充归属地字段。
func PublishHDVideo(input publisher.PublishInput) (publisher.PublishResult, error) {
	logx.Infof(hdvideoPublishLogModule, "开始发布HDVideo")

	// 打印关键调试信息
	uploadData := input.UploadData
	if uploadData == nil {
		uploadData = map[string]any{}
	}
	if standardized, ok := uploadData["standardized_params"].(map[string]any); ok && standardized != nil {
		logx.Infof(hdvideoPublishLogModule, "standardized_params.source=%v", standardized["source"])
	}
	if sourceParams, ok := uploadData["source_params"].(map[string]any); ok && sourceParams != nil {
		logx.Infof(hdvideoPublishLogModule, "source_params.产地=%v", sourceParams["产地"])
		logx.Infof(hdvideoPublishLogModule, "source_params.地区=%v", sourceParams["地区"])
	}

	return publishWithPublicSite(input, hdvideoPublisher{})
}

func (hdvideoPublisher) LogModule() string {
	return hdvideoPublishLogModule
}

func (hdvideoPublisher) AttemptPrefix(input publisher.PublishInput) string {
	return "检测到 HDVideo 站点：启用归属地 region 字段补全"
}

func (hdvideoPublisher) AdjustFormFields(input publisher.PublishInput, formFields map[string]string) {
	ensureHDVideoRegion(input, formFields)
}

// ensureHDVideoRegion 确保 HDVideo 发布表单中归属地字段被正确填充。
// 从多个数据源获取地区值，通过 YAML 映射转换为站点表单值后写入表单字段。
func ensureHDVideoRegion(input publisher.PublishInput, formFields map[string]string) {
	if formFields == nil {
		return
	}

	siteCode := strings.TrimSpace(input.SiteCode)
	siteCfg, _ := publishmapping.LoadSitePublishConfig(siteCode)
	if siteCfg == nil {
		return
	}

	// 确定表单字段名
	regionField := "region_sel[4]"
	if resolved := strings.TrimSpace(siteCfg.FormFields["region"]); resolved != "" {
		regionField = resolved
	}

	// 如果表单已有有效的 region 值，跳过
	if existing := strings.TrimSpace(formFields[regionField]); existing != "" {
		return
	}

	// 从多个数据源收集 region 候选值
	uploadData := input.UploadData
	if uploadData == nil {
		uploadData = map[string]any{}
	}
	standardized := map[string]any{}
	if s, ok := uploadData["standardized_params"].(map[string]any); ok && s != nil {
		standardized = s
	}
	sourceParams := map[string]any{}
	if sp, ok := uploadData["source_params"].(map[string]any); ok && sp != nil {
		sourceParams = sp
	}

	// 优先使用标准化值（如 "source.china"），其次使用原始产地文本（如 "中国大陆"）
	regionRaw := firstNonEmpty(
		strings.TrimSpace(toStringAny(standardized["source"], "")),
		strings.TrimSpace(toStringAny(sourceParams["产地"], "")),
		strings.TrimSpace(toStringAny(sourceParams["地区"], "")),
		strings.TrimSpace(toStringAny(uploadData["region"], "")),
	)
	if regionRaw == "" {
		logx.Infof(hdvideoPublishLogModule, "HDVideo 归属地为空，跳过 region 字段填充")
		return
	}

	// 通过 YAML mappings.region 映射到站点表单值
	regionMapping := siteCfg.Mappings["region"]
	mappedValue := strings.TrimSpace(publishmapping.PickMappedValueWithFallback("region", regionMapping, regionRaw))
	if mappedValue == "" {
		logx.Infof(hdvideoPublishLogModule, "HDVideo 归属地映射未命中 raw=%s", regionRaw)
		return
	}

	formFields[regionField] = mappedValue
	logx.Infof(hdvideoPublishLogModule, "HDVideo 归属地已填充 field=%s value=%s (raw=%s)", regionField, mappedValue, regionRaw)
}
