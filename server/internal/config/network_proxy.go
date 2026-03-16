package config

import (
	"fmt"
	"net/url"
	"strings"
)

// NetworkProxyConfig 描述应用级 HTTP 出网代理配置。
// 说明：proxy_url 非空时优先使用前端保存的代理；为空时回退环境变量；都为空则直连。
type NetworkProxyConfig struct {
	ProxyURL string `json:"proxy_url"`
	NoProxy  string `json:"no_proxy"`
}

// DefaultNetworkProxyConfig 返回默认的网络代理配置。
// 参数/返回：无参数；默认空配置表示运行时回退环境变量或直连。
// 失败场景：无。
// 副作用：无。
func DefaultNetworkProxyConfig() NetworkProxyConfig {
	return NetworkProxyConfig{
		ProxyURL: "",
		NoProxy:  "",
	}
}

// ParseNetworkProxyConfig 将任意配置对象解析为标准网络代理配置。
// 参数/返回：value 通常来自 config.json 的 network_proxy 字段；返回规范化后的配置。
// 失败场景：输入结构异常时回退默认值，不返回 error。
// 副作用：兼容读取旧版 http_proxy/https_proxy 字段。
func ParseNetworkProxyConfig(value any) NetworkProxyConfig {
	cfg := DefaultNetworkProxyConfig()
	raw, ok := value.(map[string]any)
	if !ok {
		return cfg
	}

	cfg.ProxyURL = firstNonEmptyString(
		toString(raw["proxy_url"], ""),
		toString(raw["http_proxy"], ""),
		toString(raw["https_proxy"], ""),
	)
	cfg.NoProxy = toString(raw["no_proxy"], "")
	return NormalizeNetworkProxyConfig(cfg)
}

// NormalizeNetworkProxyConfig 对网络代理配置做空白归一化。
// 参数/返回：cfg 为原始配置；返回值保证字段可直接持久化和运行时使用。
// 失败场景：无。
// 副作用：无。
func NormalizeNetworkProxyConfig(cfg NetworkProxyConfig) NetworkProxyConfig {
	normalized := cfg
	normalized.ProxyURL = strings.TrimSpace(normalized.ProxyURL)
	normalized.NoProxy = strings.TrimSpace(normalized.NoProxy)
	return normalized
}

// ValidateNetworkProxyConfig 校验网络代理配置是否合法。
// 参数/返回：cfg 为待校验配置；返回 error 表示前端提交内容非法。
// 失败场景：代理 URL scheme 不支持或格式错误时返回 error。
// 副作用：无。
func ValidateNetworkProxyConfig(cfg NetworkProxyConfig) error {
	normalized := NormalizeNetworkProxyConfig(cfg)
	return validateProxyURL(normalized.ProxyURL)
}

// ToMap 将网络代理配置转换为 config.json 可持久化的 map 结构。
// 参数/返回：无参数；返回值可直接写入 Manager.Save。
// 失败场景：无。
// 副作用：无。
func (c NetworkProxyConfig) ToMap() map[string]any {
	normalized := NormalizeNetworkProxyConfig(c)
	return map[string]any{
		"proxy_url": normalized.ProxyURL,
		"no_proxy":  normalized.NoProxy,
	}
}

// NetworkProxyConfig 返回当前配置管理器中的网络代理配置快照。
// 参数/返回：无参数；返回值用于启动阶段与设置保存后的热更新。
// 失败场景：内存配置缺失时回退默认值。
// 副作用：无。
func (m *Manager) NetworkProxyConfig() NetworkProxyConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return ParseNetworkProxyConfig(m.config["network_proxy"])
}

func validateProxyURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("代理地址格式错误: %w", err)
	}
	if strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("代理地址格式错误: %s", trimmed)
	}

	switch strings.ToLower(strings.TrimSpace(parsed.Scheme)) {
	case "http", "https", "socks5", "socks5h":
		return nil
	default:
		return fmt.Errorf("代理地址仅支持 http/https/socks5/socks5h 协议: %s", parsed.Scheme)
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
