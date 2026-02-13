package sites

import (
	"errors"
	"regexp"
)

// ExtractHHanClub 提取憨憨站点的详情页参数。
// 参数/返回：输入详情页上下文与运行期依赖，返回统一 SeedData。
// 失败场景：公共提取器缺失或公共提取失败。
// 副作用：无。
func ExtractHHanClub(input Input, runtime Runtime) (SeedData, error) {
	if runtime.ExtractWithPublic == nil {
		return SeedData{}, errors.New("缺少公共提取器依赖")
	}
	data, err := runtime.ExtractWithPublic(input)
	if err != nil {
		return data.Normalize(input.FallbackTitle), err
	}
	if runtime.ExtractMediaInfoByRegexes != nil {
		patterns := make([]*regexp.Regexp, 0, 2)
		if runtime.ReHHClubMediaInfo != nil {
			patterns = append(patterns, runtime.ReHHClubMediaInfo)
		}
		if runtime.ReMediaInfoCodeMain != nil {
			patterns = append(patterns, runtime.ReMediaInfoCodeMain)
		}
		if len(patterns) > 0 {
			if media := runtime.ExtractMediaInfoByRegexes(input.PageHTML, patterns); media != "" {
				data.MediaInfo = media
			}
		}
	}
	if runtime.BuildSourceParams != nil {
		data.SourceParams = runtime.BuildSourceParams(data)
	}
	return data.Normalize(input.FallbackTitle), nil
}
