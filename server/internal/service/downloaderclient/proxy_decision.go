package downloaderclient

import (
	"strings"
)

// ProxyDecision 描述是否启用盒子代理的判定结果。
// 参数/返回：Enabled=true 表示可执行盒子代理请求；Reason 在 Enabled=false 时给出关闭原因（便于日志打印）。
// 失败场景：无。
// 副作用：无。
type ProxyDecision struct {
	Enabled bool
	Reason  string
}

// DecideProxy 统一判定是否启用 downloader 盒子代理能力，并返回解析后的 Downloader 配置。
// 参数/返回：rootConfig 为运行配置；downloaderID 为下载器 ID；返回 downloader、判定结果与可能的配置读取错误。
// 失败场景：当读取下载器配置失败时，返回 Enabled=false 且 err!=nil（Reason=config_error）。
// 副作用：无。
func DecideProxy(rootConfig map[string]any, downloaderID string) (Downloader, ProxyDecision, error) {
	trimmedID := strings.TrimSpace(downloaderID)
	if trimmedID == "" {
		return Downloader{}, ProxyDecision{Enabled: false, Reason: "downloader_id_empty"}, nil
	}

	downloader, err := FromConfig(rootConfig, trimmedID)
	if err != nil {
		return Downloader{}, ProxyDecision{Enabled: false, Reason: "config_error"}, err
	}
	if !downloader.UseProxy {
		return downloader, ProxyDecision{Enabled: false, Reason: "use_proxy_false"}, nil
	}
	return downloader, ProxyDecision{Enabled: true}, nil
}
