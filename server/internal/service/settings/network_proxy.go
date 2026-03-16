package settings

import (
	"fmt"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/platform/netproxy"
)

func normalizeNetworkProxyValue(raw any) (config.NetworkProxyConfig, error) {
	cfg := config.ParseNetworkProxyConfig(raw)
	if err := config.ValidateNetworkProxyConfig(cfg); err != nil {
		return config.NetworkProxyConfig{}, &InvalidSettingsError{Message: fmt.Sprintf("网络代理配置不合法: %v", err)}
	}
	return config.NormalizeNetworkProxyConfig(cfg), nil
}

func applyNetworkProxyRuntime(cfg config.NetworkProxyConfig) error {
	if err := netproxy.Update(cfg); err != nil {
		return fmt.Errorf("应用网络代理配置失败: %w", err)
	}
	return nil
}
