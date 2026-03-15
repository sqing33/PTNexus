package tagging

import "strings"

var restrictedStandardTags = map[string]string{
	"禁转":     "tag.禁转",
	"tag.禁转": "tag.禁转",
	"限转":     "tag.限转",
	"tag.限转": "tag.限转",
	"分集":     "tag.分集",
	"tag.分集": "tag.分集",
}

// DetectRestrictedTags 检测标签列表中的禁转/限转/分集标签，并返回去重后的标准标签。
func DetectRestrictedTags(tags []string) []string {
	result := make([]string, 0, 3)
	seen := make(map[string]struct{}, 3)
	for _, raw := range tags {
		mapped, ok := restrictedStandardTags[strings.TrimSpace(raw)]
		if !ok || strings.TrimSpace(mapped) == "" {
			continue
		}
		if _, exists := seen[mapped]; exists {
			continue
		}
		seen[mapped] = struct{}{}
		result = append(result, mapped)
	}
	return result
}
