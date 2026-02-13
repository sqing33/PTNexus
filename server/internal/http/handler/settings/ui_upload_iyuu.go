package settings

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

	"github.com/gin-gonic/gin"
)

func (h *Handler) GetUISettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.GetTorrentsUIViewSettings())
}

func (h *Handler) SaveUISettings(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	if err := h.settings.SaveTorrentsUIViewSettings(payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "无法保存 UI 设置。"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "UI 设置已成功保存。"})
}

func (h *Handler) GetCrossSeedUISettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.GetCrossSeedUIViewSettings())
}

func (h *Handler) SaveCrossSeedUISettings(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	if err := h.settings.SaveCrossSeedUIViewSettings(payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "无法保存 Cross Seed UI 设置。"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Cross Seed UI 设置已成功保存。"})
}

func (h *Handler) GetUploadSettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.GetUploadSettings())
}

func (h *Handler) SaveUploadSettings(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	if err := h.settings.SaveUploadSettings(payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "无法保存上传设置。"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "上传设置已成功保存。"})
}

func (h *Handler) UploadImage(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file part"})
		return
	}
	if fileHeader == nil || strings.TrimSpace(fileHeader.Filename) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No selected file"})
		return
	}
	if !isAllowedImage(fileHeader.Filename) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File type not allowed"})
		return
	}

	tmpFile, err := saveMultipartToTemp(fileHeader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save uploaded file"})
		return
	}
	defer os.Remove(tmpFile)

	showURL, err := uploadToPixhost(tmpFile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upload to image host: " + err.Error()})
		return
	}
	directURL := strings.TrimSpace(showURL)
	if strings.Contains(directURL, "pixhost.to/show/") {
		directURL = strings.Replace(directURL, "https://pixhost.to/show/", "https://img2.pixhost.to/images/", 1)
	}
	c.JSON(http.StatusOK, gin.H{"url": directURL})
}

func (h *Handler) GetIYUUSettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.GetIYUUSettings())
}

func (h *Handler) SaveIYUUSettings(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	if err := h.settings.SaveIYUUSettings(payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "无法保存 IYUU 设置。"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "IYUU 设置已成功保存。"})
}

func (h *Handler) TriggerIYUUQuery(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.TriggerIYUUQuery())
}

func (h *Handler) IYUUlogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "logs": h.settings.GetIYUULogs()})
}

func isAllowedImage(filename string) bool {
	lower := strings.ToLower(strings.TrimSpace(filename))
	return strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") || strings.HasSuffix(lower, ".gif")
}

func saveMultipartToTemp(fileHeader *multipart.FileHeader) (string, error) {
	src, err := fileHeader.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()

	tmpDir := os.TempDir()
	targetPath := filepath.Join(tmpDir, fmt.Sprintf("pt-nexus-upload-%d-%s", time.Now().UnixNano(), filepath.Base(fileHeader.Filename)))
	dst, err := os.Create(targetPath)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}
	return targetPath, nil
}

func uploadToPixhost(imagePath string) (string, error) {
	const maxRetries = 3
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		file, err := os.Open(imagePath)
		if err != nil {
			return "", err
		}

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("img", filepath.Base(imagePath))
		if err != nil {
			_ = file.Close()
			return "", err
		}
		if _, err = io.Copy(part, file); err != nil {
			_ = file.Close()
			return "", err
		}
		_ = file.Close()
		_ = writer.WriteField("content_type", "0")
		if err = writer.Close(); err != nil {
			return "", err
		}

		req, err := http.NewRequest(http.MethodPost, "https://api.pixhost.to/images", body)
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

		client := &http.Client{Timeout: 120 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
				continue
			}
			break
		}

		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("Pixhost HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
				continue
			}
			break
		}

		result := struct {
			ShowURL string `json:"show_url"`
		}{}
		if err := json.Unmarshal(respBody, &result); err != nil {
			lastErr = fmt.Errorf("Pixhost 响应解析失败: %w", err)
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
				continue
			}
			break
		}
		if strings.TrimSpace(result.ShowURL) == "" {
			lastErr = fmt.Errorf("Pixhost 未返回 show_url")
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
				continue
			}
			break
		}
		return result.ShowURL, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("未知上传错误")
	}
	return "", lastErr
}
