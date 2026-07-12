package settings

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ListBackgroundImages 返回本地背景图列表。
// 参数/返回：无请求体；HTTP 200 返回 {success, items}。
// 失败场景：服务层目录读取失败返回 500。
// 副作用：无。
func (h *Handler) ListBackgroundImages(c *gin.Context) {
	items, err := h.settings.ListBackgroundImages()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "items": items})
}

// UploadBackgroundImage 上传本地背景图到 data/backgrounds。
// 参数/返回：multipart 字段 file；成功返回图片元数据。
// 失败场景：缺少文件/类型非法/过大返回 400；落盘失败返回 500。
// 副作用：写入本地背景图文件。
func (h *Handler) UploadBackgroundImage(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请选择图片文件"})
		return
	}
	if fileHeader == nil || strings.TrimSpace(fileHeader.Filename) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请选择图片文件"})
		return
	}

	src, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无法读取上传文件"})
		return
	}
	defer src.Close()

	item, err := h.settings.SaveUploadedBackground(fileHeader.Filename, src)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "item": item})
}

// DownloadBackgroundImage 将远程 URL 图片下载到本地背景图目录。
// 参数/返回：JSON {url}；成功返回图片元数据。
// 失败场景：URL 非法/下载失败返回 400。
// 副作用：发起外网请求并写入本地文件。
func (h *Handler) DownloadBackgroundImage(c *gin.Context) {
	payload := struct {
		URL string `json:"url"`
	}{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}
	item, err := h.settings.DownloadBackgroundFromURL(payload.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "item": item})
}

// DeleteBackgroundImage 删除本地背景图。
// 参数/返回：路径参数 name；成功返回 success=true。
// 失败场景：文件不存在返回 404；其他错误返回 400。
// 副作用：删除本地文件。
func (h *Handler) DeleteBackgroundImage(c *gin.Context) {
	name := strings.TrimPrefix(c.Param("name"), "/")
	if err := h.settings.DeleteBackgroundImage(name); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "不存在") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ServeBackgroundImage 提供本地背景图静态访问。
// 参数/返回：路径参数 name；直接返回文件内容。
// 失败场景：文件不存在返回 404。
// 副作用：无。
func (h *Handler) ServeBackgroundImage(c *gin.Context) {
	name := strings.TrimPrefix(c.Param("name"), "/")
	full, err := h.settings.ResolveBackgroundFile(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.Header("Cache-Control", "public, max-age=86400")
	c.File(full)
}