package repair

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
)

const lskyUploadLogModule = "迁移-末日图床上传"

// lskyDefaultDomain 是末日图床的固定域名。
const lskyDefaultDomain = "https://img.seedvault.cn"

// CheveretoUploadConfig 描述末日图床（Lsky Pro）上传所需的配置。
// 名称保留 CheveretoUploadConfig 以兼容已有调用方。
type CheveretoUploadConfig struct {
	BaseURL  string // 图床 API 根 URL，如 https://img.seedvault.cn（不含尾部斜杠）
	Email    string
	Password string
}

// lskyTokenResponse 描述 Lsky Pro 获取 token 的响应。
type lskyTokenResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Token string `json:"token"`
	} `json:"data"`
}

// lskyUploadResponse 描述 Lsky Pro 上传响应。
type lskyUploadResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Key        string `json:"key"`
		Name       string `json:"name"`
		OriginName string `json:"origin_name"`
		Size       float64 `json:"size"`
		Mimetype   string `json:"mimetype"`
		Extension  string `json:"extension"`
		Links      struct {
			URL             string `json:"url"`
			HTML            string `json:"html"`
			BBCode          string `json:"bbcode"`
			Markdown        string `json:"markdown"`
			MarkdownWithLink string `json:"markdown_with_link"`
			ThumbnailURL    string `json:"thumbnail_url"`
		} `json:"links"`
	} `json:"data"`
}

// CheveretoLogin 使用邮箱和密码向 Lsky Pro 获取 Bearer token。
// 函数名保留 CheveretoLogin 以兼容已有调用方。
func CheveretoLogin(cfg CheveretoUploadConfig) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("末日图床域名未配置")
	}
	email := strings.TrimSpace(cfg.Email)
	password := strings.TrimSpace(cfg.Password)
	if email == "" || password == "" {
		return "", fmt.Errorf("末日图床邮箱或密码未配置")
	}

	tokenURL := baseURL + "/api/v1/tokens"
	logx.Infof(lskyUploadLogModule, "开始登录末日图床: %s email=%s", baseURL, email)

	payload, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})

	req, err := http.NewRequest(http.MethodPost, tokenURL, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("创建登录请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("登录请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("末日图床登录失败 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp lskyTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("登录响应解析失败: %w body=%s", err, strings.TrimSpace(string(body)))
	}

	if !tokenResp.Status {
		return "", fmt.Errorf("末日图床登录失败: %s", tokenResp.Message)
	}

	token := strings.TrimSpace(tokenResp.Data.Token)
	if token == "" {
		return "", fmt.Errorf("末日图床未返回 token，响应: %s", strings.TrimSpace(string(body)))
	}

	logx.Infof(lskyUploadLogModule, "末日图床登录成功 email=%s", email)
	return token, nil
}

// UploadImageToChevereto 将本地图片上传到末日图床（Lsky Pro），返回图片直链 URL。
// 函数名保留 UploadImageToChevereto 以兼容已有调用方。
func UploadImageToChevereto(imagePath string, cfg CheveretoUploadConfig, accessToken string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("末日图床域名未配置")
	}
	if strings.TrimSpace(accessToken) == "" {
		return "", fmt.Errorf("末日图床 access token 为空")
	}

	file, err := os.Open(imagePath)
	if err != nil {
		return "", fmt.Errorf("打开文件失败 %s: %w", imagePath, err)
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filepath.Base(imagePath))
	if err != nil {
		return "", fmt.Errorf("创建表单文件失败: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", fmt.Errorf("写入文件数据失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("关闭 multipart writer 失败: %w", err)
	}

	uploadURL := baseURL + "/api/v1/upload"
	logx.Infof(lskyUploadLogModule, "上传文件到末日图床: %s file=%s", uploadURL, filepath.Base(imagePath))

	req, err := http.NewRequest(http.MethodPost, uploadURL, &buf)
	if err != nil {
		return "", fmt.Errorf("创建上传请求失败: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("上传请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("末日图床上传失败 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var uploadResp lskyUploadResponse
	if err := json.Unmarshal(body, &uploadResp); err != nil {
		return "", fmt.Errorf("上传响应解析失败: %w body=%s", err, strings.TrimSpace(string(body)))
	}

	if !uploadResp.Status {
		return "", fmt.Errorf("末日图床上传失败: %s", uploadResp.Message)
	}

	directURL := strings.TrimSpace(uploadResp.Data.Links.URL)
	if directURL == "" {
		return "", fmt.Errorf("末日图床未返回图片 URL，响应: %s", strings.TrimSpace(string(body)))
	}

	logx.Infof(lskyUploadLogModule, "上传成功: %s → %s", filepath.Base(imagePath), directURL)
	return directURL, nil
}

// UploadImageToCheveretoNarrative 按叙事式日志风格上传图片到末日图床，与 UploadImageToPixhostNarrativeWithLogger 风格一致。
func UploadImageToCheveretoNarrative(imagePath string, cfg CheveretoUploadConfig, accessToken string, logLine func(string, ...any)) (string, error) {
	if logLine == nil {
		logLine = logx.PlainInfof
	}

	logLine("准备上传到末日图床: %s", imagePath)
	if _, err := os.Stat(imagePath); err != nil {
		logLine("错误：文件不存在 %s", imagePath)
		return "", err
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		logLine("错误：末日图床域名未配置")
		return "", fmt.Errorf("末日图床域名未配置")
	}

	token := strings.TrimSpace(accessToken)
	if token == "" {
		logLine("access token 为空，尝试重新登录...")
		var loginErr error
		token, loginErr = CheveretoLogin(cfg)
		if loginErr != nil {
			logLine("❌ 登录失败: %s", loginErr)
			return "", loginErr
		}
		logLine("✅ 登录成功")
	}

	file, err := os.Open(imagePath)
	if err != nil {
		logLine("❌ 打开文件失败: %s", err)
		return "", err
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filepath.Base(imagePath))
	if err != nil {
		logLine("❌ 创建表单失败: %s", err)
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		logLine("❌ 写入文件数据失败: %s", err)
		return "", err
	}
	_ = writer.Close()

	uploadURL := baseURL + "/api/v1/upload"
	logLine("正在发送上传请求到末日图床: %s", uploadURL)

	req, err := http.NewRequest(http.MethodPost, uploadURL, &buf)
	if err != nil {
		logLine("❌ 创建请求失败: %s", err)
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logLine("❌ 上传请求失败: %s", classifyPixhostUploadError(err))
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		logLine("❌ 上传失败 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		return "", fmt.Errorf("末日图床上传失败 HTTP %d", resp.StatusCode)
	}

	var uploadResp lskyUploadResponse
	if err := json.Unmarshal(body, &uploadResp); err != nil {
		logLine("❌ 响应解析失败: %s", err)
		return "", err
	}

	if !uploadResp.Status {
		logLine("❌ 上传失败: %s", uploadResp.Message)
		return "", fmt.Errorf("末日图床上传失败: %s", uploadResp.Message)
	}

	directURL := strings.TrimSpace(uploadResp.Data.Links.URL)
	if directURL == "" {
		logLine("❌ 未返回图片 URL")
		return "", fmt.Errorf("末日图床未返回图片 URL")
	}

	logLine("直接上传成功！图片链接: %s", directURL)
	return directURL, nil
}

// GetImageHosterFromConfig 从 rootConfig 的 cross_seed 节提取 image_hoster 值。
func GetImageHosterFromConfig(rootConfig map[string]any) string {
	if rootConfig == nil {
		return "pixhost"
	}
	cs, ok := rootConfig["cross_seed"].(map[string]any)
	if !ok {
		return "pixhost"
	}
	hoster := strings.TrimSpace(toStringAny(cs["image_hoster"], ""))
	if hoster == "" {
		return "pixhost"
	}
	return hoster
}

// GetCheveretoConfigFromRootConfig 从 rootConfig 的 cross_seed 节提取末日图床上传配置。
// 域名固定为 https://img.seedvault.cn，无需用户配置。
func GetCheveretoConfigFromRootConfig(rootConfig map[string]any) CheveretoUploadConfig {
	cfg := CheveretoUploadConfig{
		BaseURL: lskyDefaultDomain,
	}
	if rootConfig == nil {
		return cfg
	}
	cs, ok := rootConfig["cross_seed"].(map[string]any)
	if !ok {
		return cfg
	}
	cfg.Email = strings.TrimSpace(toStringAny(cs["agsv_email"], ""))
	cfg.Password = strings.TrimSpace(toStringAny(cs["agsv_password"], ""))
	return cfg
}

// TransferRemoteImageToChevereto 将远程图片下载后上传到末日图床，返回图片直链。
func TransferRemoteImageToChevereto(imageURL string, cfg CheveretoUploadConfig, accessToken string) (string, error) {
	trimmed := strings.TrimSpace(imageURL)
	if trimmed == "" {
		return "", fmt.Errorf("empty image url")
	}

	candidates := buildPosterDownloadCandidates(trimmed)
	logx.Infof(lskyUploadLogModule, "开始海报转存到末日图床 source=%s candidates=%d", CompactLogText(trimmed, 160), len(candidates))

	errMsgs := make([]string, 0, len(candidates)*posterTransferDownloadRetry)

	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		for attempt := 1; attempt <= posterTransferDownloadRetry; attempt++ {
			data, contentType, downloadErr := downloadPosterImage(candidate)
			if downloadErr != nil {
				errMsgs = append(errMsgs, fmt.Sprintf("下载失败 candidate=%s attempt=%d err=%v", CompactLogText(candidate, 120), attempt, downloadErr))
				logx.Warnf(lskyUploadLogModule, "下载海报失败 candidate=%s attempt=%d/%d err=%v", CompactLogText(candidate, 160), attempt, posterTransferDownloadRetry, downloadErr)
				if attempt < posterTransferDownloadRetry {
					time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
				}
				continue
			}

			logx.Infof(
				lskyUploadLogModule,
				"下载海报成功 candidate=%s attempt=%d bytes=%d content_type=%s",
				CompactLogText(candidate, 160), attempt, len(data), contentType,
			)

			tmpPath, tmpErr := writePosterTempFile(data, contentType)
			if tmpErr != nil {
				errMsgs = append(errMsgs, fmt.Sprintf("落盘失败 candidate=%s err=%v", CompactLogText(candidate, 120), tmpErr))
				logx.Warnf(lskyUploadLogModule, "海报临时文件写入失败 candidate=%s err=%v", CompactLogText(candidate, 160), tmpErr)
				continue
			}

			directURL, uploadErr := UploadImageToChevereto(tmpPath, cfg, accessToken)
			_ = os.Remove(tmpPath)
			if uploadErr != nil {
				errMsgs = append(errMsgs, fmt.Sprintf("上传失败 candidate=%s attempt=%d err=%v", CompactLogText(candidate, 120), attempt, uploadErr))
				logx.Warnf(lskyUploadLogModule, "上传海报到末日图床失败 candidate=%s attempt=%d/%d err=%v", CompactLogText(candidate, 160), attempt, posterTransferDownloadRetry, uploadErr)
				if attempt < posterTransferDownloadRetry {
					time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
				}
				continue
			}

			logx.Infof(lskyUploadLogModule, "海报转存末日图床成功 source=%s target=%s", CompactLogText(trimmed, 160), CompactLogText(directURL, 160))
			return strings.TrimSpace(directURL), nil
		}
	}

	if len(errMsgs) == 0 {
		return "", fmt.Errorf("poster transfer to lsky failed without details")
	}
	return "", fmt.Errorf(strings.Join(errMsgs, " | "))
}

// ScreenshotUploadContext 封装截图上传所需的上下文。
type ScreenshotUploadContext struct {
	Hoster      string
	Chevereto   CheveretoUploadConfig
	AccessToken string
}

// PrepareScreenshotUploadContext 从 rootConfig 构建截图上传上下文，按 image_hoster 配置分派。
func PrepareScreenshotUploadContext(rootConfig map[string]any) ScreenshotUploadContext {
	hoster := GetImageHosterFromConfig(rootConfig)
	ctx := ScreenshotUploadContext{Hoster: hoster}
	if hoster == "agsv" {
		cfg := GetCheveretoConfigFromRootConfig(rootConfig)
		ctx.Chevereto = cfg
		token, err := CheveretoLogin(cfg)
		if err != nil {
			logx.Warnf(lskyUploadLogModule, "末日图床登录失败，截图将回退到 Pixhost: %v", err)
			ctx.Hoster = "pixhost"
			return ctx
		}
		ctx.AccessToken = token
		logx.Infof(lskyUploadLogModule, "末日图床登录成功，准备上传截图")
	}
	return ctx
}

// UploadScreenshot 执行单张截图上传，按 hoster 类型分派。
func (ctx ScreenshotUploadContext) UploadScreenshot(filePath string, logLine func(string, ...any)) (string, error) {
	if ctx.Hoster == "agsv" {
		return UploadImageToCheveretoNarrative(filePath, ctx.Chevereto, ctx.AccessToken, logLine)
	}
	return UploadImageToPixhostNarrativeWithLogger(filePath, logLine)
}

// NormalizeScreenshotURL 将上传返回的 URL 规范化。Pixhost 需要将 show_url 转为直链，末日图床直接返回直链。
func (ctx ScreenshotUploadContext) NormalizeScreenshotURL(showURL string) string {
	if ctx.Hoster == "agsv" {
		return strings.TrimSpace(showURL)
	}
	finalURL := strings.TrimSpace(showURL)
	if direct := PixhostShowToDirectURL(showURL); strings.TrimSpace(direct) != "" {
		if normalized := NormalizePixhostDirectHost(direct); strings.TrimSpace(normalized) != "" {
			finalURL = normalized
		} else {
			finalURL = direct
		}
	}
	return finalURL
}
