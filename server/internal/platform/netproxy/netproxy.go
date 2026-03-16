package netproxy

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/platform/logx"
	"golang.org/x/net/http/httpproxy"
)

const logModule = "网络代理"

type proxyResolverState struct {
	fn      func(*http.Request) (*url.URL, error)
	summary string
}

type runtimeManager struct {
	resolver atomic.Value

	mu         sync.Mutex
	transports map[*http.Transport]struct{}
}

var (
	globalMu      sync.Mutex
	globalManager *runtimeManager
)

// Install 初始化全局网络代理管理器，并把默认 HTTP transport 接入配置代理逻辑。
// 参数/返回：cfg 为当前应用配置中的网络代理设置；返回 error 表示默认 transport 无法接管。
// 失败场景：默认 transport 类型异常时返回 error。
// 副作用：替换 http.DefaultTransport，并注册后续热更新所需的 transport 追踪。
func Install(cfg config.NetworkProxyConfig) error {
	globalMu.Lock()
	defer globalMu.Unlock()
	return installLocked(cfg)
}

// Update 热更新全局网络代理配置，并关闭已注册 transport 的空闲连接。
// 参数/返回：cfg 为新的网络代理设置；返回 error 表示配置非法或全局代理尚未初始化失败。
// 失败场景：配置非法、默认 transport 不支持接管时返回 error。
// 副作用：更新代理决策函数，并对默认/custom transport 执行 CloseIdleConnections。
func Update(cfg config.NetworkProxyConfig) error {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalManager == nil {
		return installLocked(cfg)
	}
	return globalManager.update(cfg)
}

// ConfigureTransport 将指定 transport 接入全局网络代理管理器。
// 参数/返回：transport 为调用方自定义的 HTTP transport；原对象会被原地修改并返回。
// 失败场景：transport 为空或全局代理尚未初始化时直接返回原对象。
// 副作用：覆盖 transport.Proxy，并纳入热更新的空闲连接清理集合。
func ConfigureTransport(transport *http.Transport) *http.Transport {
	if transport == nil {
		return transport
	}

	globalMu.Lock()
	manager := globalManager
	globalMu.Unlock()
	if manager == nil {
		return transport
	}
	manager.configureTransport(transport)
	return transport
}

func installLocked(cfg config.NetworkProxyConfig) error {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return fmt.Errorf("默认 HTTP transport 类型不支持接管: %T", http.DefaultTransport)
	}

	manager := globalManager
	if manager == nil {
		manager = &runtimeManager{transports: map[*http.Transport]struct{}{}}
		globalManager = manager
	}
	if err := manager.update(cfg); err != nil {
		return err
	}

	cloned := defaultTransport.Clone()
	manager.configureTransport(cloned)
	http.DefaultTransport = cloned
	return nil
}

func (m *runtimeManager) update(cfg config.NetworkProxyConfig) error {
	state, err := buildResolverState(cfg)
	if err != nil {
		return err
	}

	m.resolver.Store(state)
	logx.Infof(logModule, "已应用网络代理配置 %s", state.summary)
	m.closeIdleConnections()
	return nil
}

func (m *runtimeManager) configureTransport(transport *http.Transport) {
	if transport == nil {
		return
	}
	transport.Proxy = m.proxy

	m.mu.Lock()
	defer m.mu.Unlock()
	m.transports[transport] = struct{}{}
}

func (m *runtimeManager) proxy(req *http.Request) (*url.URL, error) {
	raw := m.resolver.Load()
	if raw == nil {
		return nil, nil
	}
	state, ok := raw.(proxyResolverState)
	if !ok || state.fn == nil {
		return nil, nil
	}
	return state.fn(req)
}

func (m *runtimeManager) closeIdleConnections() {
	m.mu.Lock()
	transports := make([]*http.Transport, 0, len(m.transports))
	for transport := range m.transports {
		transports = append(transports, transport)
	}
	m.mu.Unlock()

	for _, transport := range transports {
		if transport == nil {
			continue
		}
		transport.CloseIdleConnections()
	}
}

func buildResolverState(cfg config.NetworkProxyConfig) (proxyResolverState, error) {
	normalized := config.NormalizeNetworkProxyConfig(cfg)
	if err := config.ValidateNetworkProxyConfig(normalized); err != nil {
		return proxyResolverState{}, err
	}

	if strings.TrimSpace(normalized.ProxyURL) != "" {
		resolver := (&httpproxy.Config{
			HTTPProxy:  normalized.ProxyURL,
			HTTPSProxy: normalized.ProxyURL,
			NoProxy:    ensureLoopbackNoProxy(normalized.NoProxy),
		}).ProxyFunc()
		return proxyResolverState{
			summary: fmt.Sprintf(
				"frontend=true no_proxy=%t",
				strings.TrimSpace(normalized.NoProxy) != "",
			),
			fn: wrapProxyFunc(resolver),
		}, nil
	}

	envCfg := httpproxy.FromEnvironment()
	if strings.TrimSpace(envCfg.HTTPProxy) == "" && strings.TrimSpace(envCfg.HTTPSProxy) == "" {
		return proxyResolverState{
			summary: "frontend=false env_proxy=false",
			fn: func(req *http.Request) (*url.URL, error) {
				return nil, nil
			},
		}, nil
	}

	rawEnvNoProxy := strings.TrimSpace(envCfg.NoProxy) != ""
	envCfg.NoProxy = ensureLoopbackNoProxy(envCfg.NoProxy)
	resolver := envCfg.ProxyFunc()
	return proxyResolverState{
		summary: fmt.Sprintf(
			"frontend=false env_http=%t env_https=%t env_no_proxy=%t",
			strings.TrimSpace(envCfg.HTTPProxy) != "",
			strings.TrimSpace(envCfg.HTTPSProxy) != "",
			rawEnvNoProxy,
		),
		fn: wrapProxyFunc(resolver),
	}, nil
}

func wrapProxyFunc(fn func(*url.URL) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		if req == nil || req.URL == nil {
			return nil, nil
		}
		if fn == nil {
			return nil, nil
		}
		return fn(req.URL)
	}
}

func ensureLoopbackNoProxy(raw string) string {
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
