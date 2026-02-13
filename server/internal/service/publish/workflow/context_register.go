package workflow

// RegisterDBContext 创建并保存“数据库命中”场景的迁移上下文。
// 参数/返回：state 为上下文状态；contextID 为外部生成的上下文 ID；其余参数用于组装上下文；返回 contextID。
// 失败场景：state 为空时仅返回 contextID，不写入状态。
// 副作用：写入 ContextState。
func RegisterDBContext(
	state *ContextState,
	contextID string,
	torrentID string,
	siteName string,
	hash string,
	name string,
	savePath string,
	downloaderID string,
	sourceNickname string,
	sourceDetailURL string,
) string {
	if state == nil {
		return contextID
	}
	state.Set(contextID, BuildContextFromDBRow(
		contextID,
		torrentID,
		siteName,
		hash,
		name,
		savePath,
		downloaderID,
		sourceNickname,
		sourceDetailURL,
	))
	return contextID
}

// RegisterFetchContext 创建并保存“抓取成功”场景的迁移上下文。
// 参数/返回：state 为上下文状态；contextID 为外部生成的上下文 ID；其余参数用于组装上下文；返回 contextID。
// 失败场景：state 为空时仅返回 contextID，不写入状态。
// 副作用：写入 ContextState。
func RegisterFetchContext(
	state *ContextState,
	contextID string,
	torrentID string,
	siteName string,
	hash string,
	name string,
	savePath string,
	downloaderID string,
	sourceNickname string,
	sourceDetailURL string,
	originalTorrentPath string,
) string {
	if state == nil {
		return contextID
	}
	state.Set(contextID, BuildContextFromFetch(
		contextID,
		torrentID,
		siteName,
		hash,
		name,
		savePath,
		downloaderID,
		sourceNickname,
		sourceDetailURL,
		originalTorrentPath,
	))
	return contextID
}
