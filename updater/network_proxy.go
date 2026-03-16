package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/http/httpproxy"
)

type networkProxyConfig struct {
	ProxyURL string `json:"proxy_url"`
	NoProxy  string `json:"no_proxy"`
}

func loadUpdaterNetworkProxyConfig() networkProxyConfig {
	path := updaterConfigFilePath()
	if strings.TrimSpace(path) == "" {
		return defaultNetworkProxyConfig()
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		return defaultNetworkProxyConfig()
	}

	root := map[string]any{}
	if err := json.Unmarshal(payload, &root); err != nil {
		return defaultNetworkProxyConfig()
	}

	raw, ok := root["network_proxy"].(map[string]any)
	if !ok {
		return defaultNetworkProxyConfig()
	}
	return parseUpdaterNetworkProxyConfig(raw)
}

func updaterConfigFilePath() string {
	if explicit := strings.TrimSpace(getEnv("PTNEXUS_CONFIG_FILE", "")); explicit != "" {
		return explicit
	}
	if dataDir := strings.TrimSpace(getEnv("PTNEXUS_DATA_DIR", "")); dataDir != "" {
		return filepath.Join(dataDir, "config.json")
	}
	return filepath.Join(filepath.Dir(updateDir), "config.json")
}

func defaultNetworkProxyConfig() networkProxyConfig {
	return networkProxyConfig{}
}

func parseUpdaterNetworkProxyConfig(raw map[string]any) networkProxyConfig {
	return networkProxyConfig{
		ProxyURL: firstNonEmptyUpdaterValue(
			toStringValue(raw["proxy_url"]),
			toStringValue(raw["http_proxy"]),
			toStringValue(raw["https_proxy"]),
		),
		NoProxy: strings.TrimSpace(toStringValue(raw["no_proxy"])),
	}
}

func buildUpdaterProxyFunc(cfg networkProxyConfig) (func(*http.Request) (*url.URL, error), error) {
	if strings.TrimSpace(cfg.ProxyURL) != "" {
		if err := validateUpdaterProxyURL(cfg.ProxyURL); err != nil {
			return nil, err
		}
		proxyFunc := (&httpproxy.Config{
			HTTPProxy:  cfg.ProxyURL,
			HTTPSProxy: cfg.ProxyURL,
			NoProxy:    ensureUpdaterLoopbackNoProxy(cfg.NoProxy),
		}).ProxyFunc()
		return func(req *http.Request) (*url.URL, error) {
			if req == nil || req.URL == nil {
				return nil, nil
			}
			return proxyFunc(req.URL)
		}, nil
	}

	useProxySetting := strings.TrimSpace(os.Getenv("UPDATE_USE_PROXY"))
	if useProxySetting != "" && !isTruthy(useProxySetting) {
		return func(req *http.Request) (*url.URL, error) { return nil, nil }, nil
	}

	envCfg := httpproxy.FromEnvironment()
	if strings.TrimSpace(envCfg.HTTPProxy) == "" && strings.TrimSpace(envCfg.HTTPSProxy) == "" {
		return func(req *http.Request) (*url.URL, error) { return nil, nil }, nil
	}
	envCfg.NoProxy = ensureUpdaterLoopbackNoProxy(envCfg.NoProxy)
	proxyFunc := envCfg.ProxyFunc()
	return func(req *http.Request) (*url.URL, error) {
		if req == nil || req.URL == nil {
			return nil, nil
		}
		return proxyFunc(req.URL)
	}, nil
}

func ensureUpdaterLoopbackNoProxy(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "*" {
		return trimmed
	}
	values := make([]string, 0, 4)
	seen := map[string]struct{}{}
	appendValue := func(value string) {
		cleaned := strings.TrimSpace(value)
		if cleaned == "" {
			return
		}
		key := strings.ToLower(cleaned)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		values = append(values, cleaned)
	}
	for _, item := range strings.Split(trimmed, ",") {
		appendValue(item)
	}
	appendValue("localhost")
	appendValue("127.0.0.1")
	appendValue("::1")
	return strings.Join(values, ",")
}

func validateUpdaterProxyURL(raw string) error {
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

func toStringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func firstNonEmptyUpdaterValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
