package uploader

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

var errLikelyLoginPage = errors.New("疑似登录页（cookie 可能已失效）")

// TryFetchDetailHTML 使用 Cookie 拉取种子详情页 HTML，用于判断是否具备编辑权限。
// 参数/返回：detailURL 为详情页绝对地址；cookie 为登录态；返回 HTML 与本次请求日志。
// 失败场景：HTTP 请求失败、返回非 2xx、命中疑似登录页。
// 副作用：发起网络请求并读取响应正文。
func TryFetchDetailHTML(detailURL, cookie string) (string, string, error) {
	detailLines := []string{
		fmt.Sprintf("正在获取种子详情页..."),
	}
	buildDetail := func() string {
		return strings.Join(detailLines, "\n")
	}

	req, err := http.NewRequest(http.MethodGet, strings.TrimSpace(detailURL), nil)
	if err != nil {
		detailLines = append(detailLines, fmt.Sprintf("创建 HTTP 请求失败: %v", err))
		return "", buildDetail(), err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	if strings.TrimSpace(cookie) != "" {
		req.Header.Set("Cookie", cookie)
	}

	client := &http.Client{
		Timeout: 45 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		detailLines = append(detailLines, fmt.Sprintf("请求失败: %v", err))
		return "", buildDetail(), err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	bodyText := string(body)

	detailLines = append(detailLines, fmt.Sprintf("响应状态: %d %s", resp.StatusCode, strings.TrimSpace(resp.Status)))
	if location := strings.TrimSpace(resp.Header.Get("Location")); location != "" {
		detailLines = append(detailLines, fmt.Sprintf("Location: %s", redactURLQuery(location)))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err = fmt.Errorf("HTTP %d 获取详情页失败", resp.StatusCode)
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return "", buildDetail(), err
	}

	if looksLikeLoginHTML(body) {
		detailLines = append(detailLines, fmt.Sprintf("站点响应: %s", summarizeResponseBody(bodyText)))
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", errLikelyLoginPage))
		return "", buildDetail(), errLikelyLoginPage
	}

	return bodyText, buildDetail(), nil
}

// TryEditTorrent 对已存在种子发起 takeedit.php 编辑请求（NexusPHP 兼容表单）。
// 参数/返回：takeEditURL 为 takeedit.php 绝对地址；cookie/referer 用于鉴权；formFields 为编辑字段（必须包含 id）。
// 失败场景：HTTP 请求失败、返回 4xx/5xx、疑似命中登录页、表单缺少 id。
// 副作用：发起 HTTP POST 请求并读取响应正文。
func TryEditTorrent(takeEditURL, cookie, referer string, formFields map[string]string) (bool, string, error) {
	detailLines := []string{
		fmt.Sprintf("正在提交编辑请求..."),
	}
	buildDetail := func() string {
		return strings.Join(detailLines, "\n")
	}

	values := neturl.Values{}
	hasID := false
	for key, value := range formFields {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" {
			continue
		}
		if trimmedKey == "id" && trimmedValue != "" {
			hasID = true
		}
		if trimmedValue == "" {
			continue
		}
		values.Set(trimmedKey, trimmedValue)
	}
	if !hasID {
		err := errors.New("编辑表单缺少 id")
		detailLines = append(detailLines, fmt.Sprintf("构建编辑表单失败: %v", err))
		return false, buildDetail(), err
	}

	req, err := http.NewRequest(http.MethodPost, strings.TrimSpace(takeEditURL), strings.NewReader(values.Encode()))
	if err != nil {
		detailLines = append(detailLines, fmt.Sprintf("创建 HTTP 请求失败: %v", err))
		return false, buildDetail(), err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	if strings.TrimSpace(cookie) != "" {
		req.Header.Set("Cookie", cookie)
	}
	if strings.TrimSpace(referer) != "" {
		req.Header.Set("Referer", strings.TrimSpace(referer))
	}

	client := &http.Client{
		Timeout: 120 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		detailLines = append(detailLines, fmt.Sprintf("请求失败: %v", err))
		return false, buildDetail(), err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	bodyText := string(respBody)
	responseDetail := summarizeResponseBody(bodyText)

	detailLines = append(detailLines, fmt.Sprintf("响应状态: %d %s", resp.StatusCode, strings.TrimSpace(resp.Status)))
	location := strings.TrimSpace(resp.Header.Get("Location"))
	if location != "" {
		detailLines = append(detailLines, fmt.Sprintf("Location: %s", redactURLQuery(location)))
	}
	if responseDetail != "" {
		detailLines = append(detailLines, fmt.Sprintf("站点响应: %s", responseDetail))
	}

	if looksLikeLoginHTML(respBody) || strings.Contains(strings.ToLower(location), "login.php") {
		err = errLikelyLoginPage
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return false, buildDetail(), err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		if responseDetail == "" {
			responseDetail = "<empty response body>"
		}
		err = fmt.Errorf("HTTP %d 编辑失败: %s", resp.StatusCode, responseDetail)
		detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
		return false, buildDetail(), err
	}

	success := looksLikeEditSuccess(resp.StatusCode, location, bodyText)
	if success {
		detailLines = append(detailLines, "✓ 编辑成功")
	} else {
		detailLines = append(detailLines, "✗ 编辑失败")
	}
	return success, buildDetail(), nil
}

func looksLikeEditSuccess(statusCode int, location string, bodyText string) bool {
	if statusCode >= 300 && statusCode < 400 {
		loc := strings.ToLower(strings.TrimSpace(location))
		if loc == "" || strings.Contains(loc, "login.php") {
			return false
		}
		if strings.Contains(loc, "details.php") || strings.Contains(loc, "edit.php") {
			return true
		}
		return false
	}

	trimmed := strings.TrimSpace(bodyText)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "error") || strings.Contains(trimmed, "失败") || strings.Contains(trimmed, "无权") || strings.Contains(lower, "denied") {
		return false
	}
	if strings.Contains(trimmed, "成功") || strings.Contains(lower, "success") || strings.Contains(lower, "saved") {
		return true
	}
	return strings.Contains(lower, "details.php?id=")
}

func looksLikeLoginHTML(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	limit := len(content)
	if limit > 4096 {
		limit = 4096
	}
	sample := strings.ToLower(string(content[:limit]))
	trimmed := strings.TrimSpace(sample)
	if !strings.HasPrefix(trimmed, "<!doctype") && !strings.HasPrefix(trimmed, "<html") {
		return false
	}
	if strings.Contains(sample, "login.php") && strings.Contains(sample, "returnto=") {
		return true
	}
	if strings.Contains(sample, "name=\"username\"") && strings.Contains(sample, "name=\"password\"") {
		return true
	}
	return strings.Contains(sample, "action=\"login.php\"")
}

func redactURLQuery(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := neturl.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
