package workflow

// BuildFetchStoreSuccessResponse 构建 FetchAndStore 成功响应体。
// 参数/返回：contextID 为发布上下文任务 ID；返回与历史兼容的响应字段。
// 失败场景：无。
// 副作用：无。
func BuildFetchStoreSuccessResponse(contextID string) map[string]any {
	return map[string]any{
		"success": true,
		"task_id": contextID,
		"message": "种子信息已成功保存到数据库",
		"logs":    "种子信息已成功保存到数据库",
	}
}
