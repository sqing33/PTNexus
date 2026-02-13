package sites

import (
	"errors"
	"fmt"
)

// ExtractHDSky 提取天空站点的详情页参数。
// 参数/返回：输入详情页上下文与运行期依赖，返回统一 SeedData。
// 失败场景：公共提取失败、标签列表抓取失败。
// 副作用：可能会发起列表页网络请求（用于补全标签）。
func ExtractHDSky(input Input, runtime Runtime) (SeedData, error) {
	if runtime.ExtractWithPublic == nil {
		return SeedData{}, errors.New("缺少公共提取器依赖")
	}
	data, err := runtime.ExtractWithPublic(input)
	if err != nil {
		return data.Normalize(input.FallbackTitle), err
	}

	if runtime.FetchTagsFromTorrentList != nil {
		tags, tagErr := runtime.FetchTagsFromTorrentList(input.BaseURL, input.Cookie, data.Title, input.TorrentID)
		if tagErr != nil {
			return data.Normalize(input.FallbackTitle), fmt.Errorf("天空标签提取失败: %w", tagErr)
		}
		if len(tags) > 0 {
			data.Tags = tags
		}
	}

	if runtime.BuildSourceParams != nil {
		data.SourceParams = runtime.BuildSourceParams(data)
	}
	return data.Normalize(input.FallbackTitle), nil
}
