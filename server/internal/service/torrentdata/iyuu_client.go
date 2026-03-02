package torrentdata

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
)

const (
	iyuuAPIBase       = "https://2025.iyuu.cn"
	iyuuClientVersion = "8.2.0"
)

const iyuuLogModule = "IYUU查询"

type iyuuSite struct {
	ID       int64  `json:"id"`
	Nickname string `json:"nickname"`
	BaseURL  string `json:"base_url"`
	Site     string `json:"site"`
}

type iyuuClient struct {
	httpClient *http.Client

	baseURL string

	mu              sync.Mutex
	lastRequestTime time.Time
	rateLimitDelay  time.Duration
}

// newIYUUClient 创建 IYUU API 客户端。
// 参数/返回：无输入；返回可复用的客户端实例。
// 失败场景：无。
// 副作用：无（仅初始化内存对象）。
func newIYUUClient() *iyuuClient {
	return &iyuuClient{
		httpClient:     &http.Client{Timeout: 25 * time.Second},
		baseURL:        iyuuAPIBase,
		rateLimitDelay: 5 * time.Second,
	}
}

func (c *iyuuClient) doRequest(ctx context.Context, method string, rawURL string, token string, headers map[string]string, body io.Reader) (map[string]any, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("IYUU Token 未配置")
	}

	c.mu.Lock()
	if !c.lastRequestTime.IsZero() {
		elapsed := time.Since(c.lastRequestTime)
		if elapsed < c.rateLimitDelay {
			wait := c.rateLimitDelay - elapsed
			c.mu.Unlock()
			time.Sleep(wait)
			c.mu.Lock()
		}
	}
	c.lastRequestTime = time.Now()
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Token", token)
	for k, v := range headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("IYUU API HTTP错误 status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(limited)))
	}

	var parsed map[string]any
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("IYUU API 响应JSON解析失败: %w", err)
	}

	if code, ok := parsed["code"]; ok {
		codeNumber := intValue(code, 0)
		if codeNumber != 0 {
			msg := strings.TrimSpace(stringValue(parsed["msg"], "未知 API 错误"))
			return nil, fmt.Errorf("IYUU API 错误: %s (代码: %d)", msg, codeNumber)
		}
	}

	return parsed, nil
}

func sha1Hex(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}

// getSupportedSites 获取 IYUU 支持的可辅种站点列表。
// 参数/返回：token 为 IYUU Token；返回站点列表。
// 失败场景：网络错误、Token 无效、API 返回错误。
// 副作用：发起远程 HTTP 请求。
func (c *iyuuClient) getSupportedSites(ctx context.Context, token string) ([]iyuuSite, error) {
	rawURL := strings.TrimRight(c.baseURL, "/") + "/reseed/sites/index"
	resp, err := c.doRequest(ctx, http.MethodGet, rawURL, token, nil, nil)
	if err != nil {
		return nil, err
	}
	data, _ := resp["data"].(map[string]any)
	rawSites, _ := data["sites"].([]any)
	if len(rawSites) == 0 {
		return nil, errors.New("未能获取到可辅种站点列表，请检查 Token 或 IYUU 服务状态")
	}

	sites := make([]iyuuSite, 0, len(rawSites))
	for _, raw := range rawSites {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		site := iyuuSite{
			ID:       int64Value(m["id"], 0),
			Nickname: strings.TrimSpace(stringValue(m["nickname"], "")),
			BaseURL:  strings.TrimSpace(stringValue(m["base_url"], "")),
			Site:     strings.TrimSpace(stringValue(m["site"], "")),
		}
		if site.ID <= 0 {
			continue
		}
		sites = append(sites, site)
	}
	if len(sites) == 0 {
		return nil, errors.New("IYUU 站点列表为空（解析失败）")
	}
	return sites, nil
}

// reportExisting 上报现有站点列表并获取 sid_sha1。
// 参数/返回：sidList 为 IYUU 站点ID列表；返回 sid_sha1。
// 失败场景：网络错误、API 返回错误、sid_sha1 缺失。
// 副作用：发起远程 HTTP 请求。
func (c *iyuuClient) reportExisting(ctx context.Context, token string, sidList []int64) (string, error) {
	if len(sidList) == 0 {
		return "", errors.New("sid_list 为空，无法生成 sid_sha1")
	}
	payload := map[string]any{"sid_list": sidList}
	bodyBytes, _ := json.Marshal(payload)
	rawURL := strings.TrimRight(c.baseURL, "/") + "/reseed/sites/reportExisting"
	resp, err := c.doRequest(ctx, http.MethodPost, rawURL, token, map[string]string{"Content-Type": "application/json"}, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	data, _ := resp["data"].(map[string]any)
	sidSha1 := strings.TrimSpace(stringValue(data["sid_sha1"], ""))
	if sidSha1 == "" {
		return "", errors.New("未能从 IYUU API 获取 sid_sha1")
	}
	return sidSha1, nil
}

// queryCrossSeedBatch 批量查询多个 infohash 的辅种信息（单次请求传入 hash 数组）。
// 参数/返回：hashes 为 infohash 列表（忽略空值，统一转小写去重）；返回 hash->torrentList 的映射。
// 失败场景：网络错误、API 返回错误（如 Token 无效等）。
// 副作用：发起远程 HTTP 请求。
func (c *iyuuClient) queryCrossSeedBatch(ctx context.Context, token string, hashes []string, sidSha1 string) (map[string][]map[string]any, error) {
	unique := make([]string, 0, len(hashes))
	seen := map[string]struct{}{}
	for _, h := range hashes {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		unique = append(unique, h)
	}
	if len(unique) == 0 {
		return map[string][]map[string]any{}, nil
	}
	if strings.TrimSpace(sidSha1) == "" {
		return nil, errors.New("sid_sha1 为空，无法查询辅种信息")
	}

	hashesJSON, _ := json.Marshal(unique)
	hashesJSONStr := string(hashesJSON)
	form := url.Values{}
	form.Set("hash", hashesJSONStr)
	form.Set("sha1", sha1Hex(hashesJSONStr))
	form.Set("sid_sha1", sidSha1)
	form.Set("timestamp", fmt.Sprintf("%d", time.Now().Unix()))
	form.Set("version", iyuuClientVersion)

	rawURL := strings.TrimRight(c.baseURL, "/") + "/reseed/index/index"
	resp, err := c.doRequest(ctx, http.MethodPost, rawURL, token, map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, strings.NewReader(form.Encode()))
	if err != nil {
		// 对齐 Python：400/未查询到数据 视为无结果，不作为硬错误。
		msg := err.Error()
		if strings.Contains(msg, "400") || strings.Contains(msg, "未查询到可辅种数据") {
			result := map[string][]map[string]any{}
			for _, h := range unique {
				result[h] = []map[string]any{}
			}
			return result, nil
		}
		return nil, err
	}
	data, _ := resp["data"].(map[string]any)
	result := map[string][]map[string]any{}
	for _, h := range unique {
		entry, _ := data[h].(map[string]any)
		rawTorrents, _ := entry["torrent"].([]any)
		items := make([]map[string]any, 0, len(rawTorrents))
		for _, raw := range rawTorrents {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			items = append(items, m)
		}
		result[h] = items
	}
	return result, nil
}

func iyuuInfof(format string, args ...any) {
	logx.Infof(iyuuLogModule, format, args...)
}

func iyuuWarnf(format string, args ...any) {
	logx.Warnf(iyuuLogModule, format, args...)
}

func iyuuErrorf(format string, args ...any) {
	logx.Errorf(iyuuLogModule, format, args...)
}
