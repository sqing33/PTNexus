package extract

import (
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
)

const routeLogModule = "迁移-参数提取"

// Engine 负责按站点选择提取器，并在特殊提取失败时回退公共提取器。
type Engine struct {
	publicExtractor   Extractor
	specialByCode     map[string]Extractor
	specialByNickname map[string]Extractor
}

// NewEngine 创建可配置提取器引擎。
func NewEngine(publicExtractor Extractor, specialByCode map[string]Extractor, specialByNickname map[string]Extractor) *Engine {
	engine := &Engine{
		publicExtractor:   publicExtractor,
		specialByCode:     map[string]Extractor{},
		specialByNickname: map[string]Extractor{},
	}
	if engine.publicExtractor == nil {
		engine.publicExtractor = NewPublicExtractor(func(input Input) (SeedData, error) {
			return SeedData{}, nil
		})
	}
	for key, extractor := range specialByCode {
		normalized := normalizeKey(key)
		if normalized == "" || extractor == nil {
			continue
		}
		engine.specialByCode[normalized] = extractor
	}
	for key, extractor := range specialByNickname {
		normalized := normalizeKey(key)
		if normalized == "" || extractor == nil {
			continue
		}
		engine.specialByNickname[normalized] = extractor
	}
	return engine
}

// NewDefaultEngine 创建默认站点提取器注册表。
func NewDefaultEngine(publicExtractor Extractor, runtimeFactory RuntimeFactory) *Engine {
	ssdExtractor := NewSSDSpecialExtractor(publicExtractor, runtimeFactory)
	audiencesExtractor := NewAudiencesSpecialExtractor(publicExtractor, runtimeFactory)
	hhclubExtractor := NewHHanClubSpecialExtractor(publicExtractor, runtimeFactory)
	keepfrdsExtractor := NewKeepfrdsSpecialExtractor(publicExtractor, runtimeFactory)
	chdbitsExtractor := NewCHDBitsSpecialExtractor(publicExtractor, runtimeFactory)
	hdskyExtractor := NewHDSkySpecialExtractor(publicExtractor, runtimeFactory)
	pterclubExtractor := NewPTerClubSpecialExtractor(publicExtractor, runtimeFactory)
	hddolbyExtractor := NewHDDolbySpecialExtractor(publicExtractor, runtimeFactory)
	dstudioExtractor := NewDStudioSpecialExtractor(publicExtractor, runtimeFactory)

	return NewEngine(
		publicExtractor,
		map[string]Extractor{
			"ssd":       ssdExtractor,
			"audiences": audiencesExtractor,
			"hhanclub":  hhclubExtractor,
			"keepfrds":  keepfrdsExtractor,
			"chdbits":   chdbitsExtractor,
			"hdsky":     hdskyExtractor,
			"pterclub":  pterclubExtractor,
			"hddolby":   hddolbyExtractor,
			"ds":        dstudioExtractor,
			"dstudio":   dstudioExtractor,
		},
		map[string]Extractor{
			"不可说":          ssdExtractor,
			"人人":           audiencesExtractor,
			"憨憨":           hhclubExtractor,
			"月月":           keepfrdsExtractor,
			"彩虹岛":          chdbitsExtractor,
			"天空":           hdskyExtractor,
			"猫站":           pterclubExtractor,
			"杜比":           hddolbyExtractor,
			"屌丝":           dstudioExtractor,
			"dstudio":      dstudioExtractor,
			"Depth Studio": dstudioExtractor,
		},
	)
}

// Extract 执行提取器路由，并在特殊提取异常/结果不足时回退公共提取器。
func (e *Engine) Extract(input Input) (SeedData, Meta) {
	if e == nil {
		return NewDefaultEngine(nil, nil).Extract(input)
	}

	publicExtractor := e.publicExtractor
	if publicExtractor == nil {
		publicExtractor = NewPublicExtractor(func(input Input) (SeedData, error) {
			return SeedData{}, nil
		})
	}

	meta := Meta{ExtractorName: publicExtractor.Name()}
	specialExtractor := e.pickSpecialExtractor(input.SiteCode, input.SiteNickname)
	if specialExtractor == nil {
		data, err := publicExtractor.Extract(input)
		if err != nil {
			meta.FallbackReason = compactText(err.Error(), 180)
			logx.Warnf(routeLogModule, "公共提取器执行异常 site_code=%s site_name=%s err=%v", strings.TrimSpace(input.SiteCode), strings.TrimSpace(input.SiteNickname), err)
		}
		return data.NormalizeWithFallback(input.FallbackTitle), meta
	}

	specialData, err := specialExtractor.Extract(input)
	if err == nil && specialData.IsMeaningful() {
		meta.ExtractorName = specialExtractor.Name()
		return specialData.NormalizeWithFallback(input.FallbackTitle), meta
	}

	meta.UsedFallback = true
	meta.ExtractorName = publicExtractor.Name()
	reason := "特殊提取结果为空"
	if err != nil {
		reason = err.Error()
	}
	meta.FallbackReason = compactText(reason, 180)
	logx.Warnf(
		routeLogModule,
		"特殊提取器回退公共 site_code=%s site_name=%s special=%s reason=%s",
		strings.TrimSpace(input.SiteCode),
		strings.TrimSpace(input.SiteNickname),
		specialExtractor.Name(),
		meta.FallbackReason,
	)

	publicData, publicErr := publicExtractor.Extract(input)
	if publicErr != nil {
		logx.Warnf(routeLogModule, "公共提取器回退执行异常 site_code=%s site_name=%s err=%v", strings.TrimSpace(input.SiteCode), strings.TrimSpace(input.SiteNickname), publicErr)
		meta.FallbackReason = compactText(meta.FallbackReason+"; 公共提取异常: "+publicErr.Error(), 220)
	}
	return publicData.NormalizeWithFallback(input.FallbackTitle), meta
}

func (e *Engine) pickSpecialExtractor(siteCode string, siteNickname string) Extractor {
	if e == nil {
		return nil
	}
	if byCode := e.specialByCode[normalizeKey(siteCode)]; byCode != nil {
		return byCode
	}
	if byName := e.specialByNickname[normalizeKey(siteNickname)]; byName != nil {
		return byName
	}
	return nil
}

// SpecialExtractorName 返回命中的特殊提取器名称。
func (e *Engine) SpecialExtractorName(siteCode string, siteNickname string) (name string, ok bool) {
	if e == nil {
		return "", false
	}
	extractor := e.pickSpecialExtractor(siteCode, siteNickname)
	if extractor == nil {
		return "", false
	}
	return extractor.Name(), true
}

func normalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func compactText(text string, maxLen int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || maxLen <= 0 {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= maxLen {
		return trimmed
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
