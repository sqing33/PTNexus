package extract

import "errors"

// PublicExtractFunc 表示公共提取器执行函数。
type PublicExtractFunc func(input Input) (SeedData, error)

type publicExtractor struct {
	name string
	fn   PublicExtractFunc
}

// NewPublicExtractor 构造公共提取器。
func NewPublicExtractor(fn PublicExtractFunc) Extractor {
	return &publicExtractor{name: "public", fn: fn}
}

func (e *publicExtractor) Name() string {
	if e == nil || e.name == "" {
		return "public"
	}
	return e.name
}

func (e *publicExtractor) Extract(input Input) (SeedData, error) {
	if e == nil || e.fn == nil {
		return SeedData{}.NormalizeWithFallback(input.FallbackTitle), errors.New("公共提取函数未初始化")
	}
	data, err := e.fn(input)
	if data.SourceParams == nil {
		data.SourceParams = BuildSourceParamsFromExtractedData(data)
	}
	return data.NormalizeWithFallback(input.FallbackTitle), err
}
