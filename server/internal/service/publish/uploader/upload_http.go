package uploader

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// TryUploadTorrent 执行单次上传尝试，支持重定向 Location 与响应正文解析详情页链接。
// 参数/返回：uploadURL/baseURL/cookie/fileField 为站点上传所需信息；torrentFile 为种子字节；formFields 为表单字段；
// 返回发布详情页 URL、是否疑似“种子已存在”、本次尝试日志，以及错误。
// 失败场景：HTTP 请求失败、未能解析发布链接、服务端返回错误等。
// 副作用：发起 HTTP 请求并读取响应正文。
func TryUploadTorrent(uploadURL, baseURL, cookie, fileField string, torrentFile []byte, fileName string, formFields map[string]string) (string, bool, string, error) {
	detailLines := []string{
		fmt.Sprintf("请求地址: %s", strings.TrimSpace(uploadURL)),
		fmt.Sprintf("上传字段: %s", strings.TrimSpace(fileField)),
	}
	buildDetail := func() string {
		return strings.Join(detailLines, "\n")
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range formFields {
		if strings.TrimSpace(value) == "" {
			continue
		}
		_ = writer.WriteField(key, value)
	}
	part, err := writer.CreateFormFile(fileField, fileName)
	if err != nil {
		detailLines = append(detailLines, fmt.Sprintf("构建上传表单失败: %v", err))
		return "", false, buildDetail(), err
	}
	if _, err := part.Write(torrentFile); err != nil {
		detailLines = append(detailLines, fmt.Sprintf("写入种子内容失败: %v", err))
		return "", false, buildDetail(), err
	}
	if err := writer.Close(); err != nil {
		detailLines = append(detailLines, fmt.Sprintf("封装上传请求失败: %v", err))
		return "", false, buildDetail(), err
	}

	req, err := http.NewRequest(http.MethodPost, uploadURL, body)
	if err != nil {
		detailLines = append(detailLines, fmt.Sprintf("创建 HTTP 请求失败: %v", err))
		return "", false, buildDetail(), err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Referer", strings.TrimRight(baseURL, "/")+"/upload.php")

	client := &http.Client{
		Timeout: 120 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		detailLines = append(detailLines, fmt.Sprintf("请求失败: %v", err))
		return "", false, buildDetail(), err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	bodyText := string(respBody)
	responseDetail := summarizeResponseBody(bodyText)
	detailLines = append(detailLines, fmt.Sprintf("响应状态: %d %s", resp.StatusCode, strings.TrimSpace(resp.Status)))
	if location := strings.TrimSpace(resp.Header.Get("Location")); location != "" {
		detailLines = append(detailLines, fmt.Sprintf("Location: %s", location))
	}
	if responseDetail != "" {
		detailLines = append(detailLines, fmt.Sprintf("站点响应: %s", responseDetail))
	}

	isExisting := looksLikeExistingTorrent(bodyText)
	detailLines = append(detailLines, fmt.Sprintf("已存在判定: %t", isExisting))

	if location := resp.Header.Get("Location"); location != "" {
		if publishURL := NormalizePublishURLWithOfferSupport(baseURL, location); publishURL != "" {
			detailLines = append(detailLines, fmt.Sprintf("解析详情页: %s", publishURL))
			return publishURL, isExisting, buildDetail(), nil
		}
	}
	if publishURL := ExtractPublishURLFromText(baseURL, bodyText); publishURL != "" {
		detailLines = append(detailLines, fmt.Sprintf("解析详情页: %s", publishURL))
		return publishURL, isExisting, buildDetail(), nil
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		if strings.Contains(bodyText, "已存在") || strings.Contains(strings.ToLower(bodyText), "already exists") {
			if publishURL := ExtractPublishURLFromText(baseURL, bodyText); publishURL != "" {
				detailLines = append(detailLines, fmt.Sprintf("解析详情页: %s", publishURL))
				return publishURL, true, buildDetail(), nil
			}
			err = fmt.Errorf("目标站点提示种子已存在，但未返回详情链接")
			detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
			return "", true, buildDetail(), err
		}
	}

	if responseDetail == "" {
		responseDetail = "<empty response body>"
	}
	err = fmt.Errorf("HTTP %d 上传失败: %s", resp.StatusCode, responseDetail)
	detailLines = append(detailLines, fmt.Sprintf("尝试结论: %v", err))
	return "", isExisting, buildDetail(), err
}

func looksLikeExistingTorrent(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	return strings.Contains(trimmed, "种子已存在") ||
		strings.Contains(trimmed, "该种子已存在") ||
		strings.Contains(trimmed, "已存在") ||
		strings.Contains(lower, "already exists")
}

func summarizeResponseBody(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Fields(trimmed), " ")
}
