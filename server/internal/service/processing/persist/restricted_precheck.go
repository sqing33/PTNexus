package persist

// FetchRestrictionPrecheckResult 表示抓取修复前的受限标签预检结果。
type FetchRestrictionPrecheckResult struct {
	Matched        bool
	SkipRepairs    bool
	RestrictedTags []string
	Reason         string
}

// DetectFetchRestrictionPrecheck 在抓取修复前执行兼容占位检查。
// 当前已取消基于禁转/限转/分集标签的修复短路逻辑，因此始终返回空结果。
func DetectFetchRestrictionPrecheck(
	draft *SeedDraft,
	siteIdentifier string,
	savePath string,
	torrentNameForPath string,
	downloaderID string,
	rootConfig map[string]any,
) FetchRestrictionPrecheckResult {
	_ = draft
	_ = siteIdentifier
	_ = savePath
	_ = torrentNameForPath
	_ = downloaderID
	_ = rootConfig
	return FetchRestrictionPrecheckResult{}
}
