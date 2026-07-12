package settings

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
)

//go:embed default_backgrounds/*
var defaultBackgroundsFS embed.FS

const (
	backgroundDirName        = "backgrounds"
	backgroundURLPrefix      = "/backgrounds/"
	backgroundMaxUploadBytes = 15 << 20 // 15MB
	backgroundModuleName     = "背景图"
)

// BackgroundImage 本地背景图列表项。
type BackgroundImage struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	UpdatedAt int64  `json:"updated_at"`
}

// SetDataDir 注入运行时数据目录，用于本地背景图落盘。
// 参数/返回：dataDir 为 PTNEXUS_DATA_DIR；无返回。
// 失败场景：无。
// 副作用：仅写入内存字段，并确保 backgrounds 目录存在。
func (s *SettingsService) SetDataDir(dataDir string) {
	s.dataDir = strings.TrimSpace(dataDir)
	if s.dataDir == "" {
		return
	}
	if err := os.MkdirAll(s.backgroundsDir(), 0o755); err != nil {
		logx.Warnf(backgroundModuleName, "创建背景图目录失败 path=%s err=%v", s.backgroundsDir(), err)
	}
}

// EnsureDefaultBackgrounds 在 backgrounds 为空时，把内置默认图写入 data 目录。
// 参数/返回：无；返回写入过程错误。
// 失败场景：数据目录未配置、目录读写失败、内置资源读取失败。
// 副作用：可能创建目录并写入默认 jpg 文件。
func (s *SettingsService) EnsureDefaultBackgrounds() error {
	dir, err := s.requireBackgroundsDir()
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("读取背景图目录失败: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if isAllowedBackgroundExt(entry.Name()) {
			return nil
		}
	}

	embedded, err := fs.ReadDir(defaultBackgroundsFS, "default_backgrounds")
	if err != nil {
		return fmt.Errorf("读取内置默认背景失败: %w", err)
	}

	written := 0
	for _, entry := range embedded {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isAllowedBackgroundExt(name) {
			continue
		}
		safeName, err := sanitizeBackgroundFilename(name)
		if err != nil {
			logx.Warnf(backgroundModuleName, "跳过非法默认背景文件 name=%s err=%v", name, err)
			continue
		}
		srcPath := filepath.ToSlash(filepath.Join("default_backgrounds", name))
		data, err := defaultBackgroundsFS.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("读取内置背景 %s 失败: %w", name, err)
		}
		dest := filepath.Join(dir, safeName)
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("写入默认背景 %s 失败: %w", safeName, err)
		}
		written++
	}
	if written > 0 {
		logx.Infof(backgroundModuleName, "已写入默认背景图 count=%d dir=%s", written, dir)
	}
	return nil
}

// ListBackgroundImages 列出 data/backgrounds 下的本地图片。
// 参数/返回：返回图片元数据列表。
// 失败场景：数据目录未配置、目录读取失败。
// 副作用：无。
func (s *SettingsService) ListBackgroundImages() ([]BackgroundImage, error) {
	dir, err := s.requireBackgroundsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取背景图目录失败: %w", err)
	}

	items := make([]BackgroundImage, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isAllowedBackgroundExt(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		items = append(items, BackgroundImage{
			Name:      name,
			URL:       backgroundPublicURL(name),
			Size:      info.Size(),
			UpdatedAt: info.ModTime().Unix(),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt == items[j].UpdatedAt {
			return items[i].Name < items[j].Name
		}
		return items[i].UpdatedAt > items[j].UpdatedAt
	})
	return items, nil
}

// SaveUploadedBackground 将上传的图片流保存到本地背景图目录。
// 参数/返回：filename 原始文件名；r 内容流；返回保存后的元数据。
// 失败场景：扩展名非法、目录不可用、写入失败。
// 副作用：写入本地文件。
func (s *SettingsService) SaveUploadedBackground(filename string, r io.Reader) (*BackgroundImage, error) {
	safeName, err := sanitizeBackgroundFilename(filename)
	if err != nil {
		return nil, err
	}
	dir, err := s.requireBackgroundsDir()
	if err != nil {
		return nil, err
	}

	finalName := uniqueBackgroundName(dir, safeName)
	dest := filepath.Join(dir, finalName)
	tmp := dest + ".tmp"

	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}

	written, copyErr := io.Copy(file, io.LimitReader(r, backgroundMaxUploadBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("写入背景图失败: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("关闭临时文件失败: %w", closeErr)
	}
	if written > backgroundMaxUploadBytes {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("图片过大，最大允许 %d MB", backgroundMaxUploadBytes>>20)
	}
	if written == 0 {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("图片内容为空")
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("落盘背景图失败: %w", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		return nil, fmt.Errorf("读取保存结果失败: %w", err)
	}
	logx.Infof(backgroundModuleName, "上传背景图成功 name=%s size=%d", finalName, info.Size())
	return &BackgroundImage{
		Name:      finalName,
		URL:       backgroundPublicURL(finalName),
		Size:      info.Size(),
		UpdatedAt: info.ModTime().Unix(),
	}, nil
}

// DownloadBackgroundFromURL 从远程 URL 下载图片并保存到本地背景图目录。
// 参数/返回：imageURL 远程地址；返回保存后的元数据。
// 失败场景：URL 非法、下载失败、非图片、落盘失败。
// 副作用：发起 HTTP 请求并写入本地文件。
func (s *SettingsService) DownloadBackgroundFromURL(imageURL string) (*BackgroundImage, error) {
	imageURL = strings.TrimSpace(imageURL)
	if imageURL == "" {
		return nil, fmt.Errorf("图片 URL 不能为空")
	}
	if !strings.HasPrefix(imageURL, "http://") && !strings.HasPrefix(imageURL, "https://") {
		return nil, fmt.Errorf("仅支持 http/https 图片 URL")
	}

	client := &http.Client{Timeout: 45 * time.Second}
	req, err := http.NewRequest(http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构造下载请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "PTNexus-BackgroundFetcher/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载图片失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载图片失败，HTTP 状态码 %d", resp.StatusCode)
	}

	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	ext := extensionFromContentType(contentType)
	if ext == "" {
		ext = filepath.Ext(strings.Split(imageURL, "?")[0])
	}
	if !isAllowedBackgroundExt("x" + strings.ToLower(ext)) {
		return nil, fmt.Errorf("远程资源不是支持的图片类型")
	}

	base := filepath.Base(strings.Split(imageURL, "?")[0])
	if base == "." || base == "/" || base == "" || !isAllowedBackgroundExt(base) {
		base = fmt.Sprintf("background_%d%s", time.Now().Unix(), strings.ToLower(ext))
	}
	return s.SaveUploadedBackground(base, resp.Body)
}

// ResolveBackgroundFile 解析本地背景图绝对路径，防止目录穿越。
// 参数/返回：name 文件名；返回绝对路径。
// 失败场景：文件名非法、文件不存在。
// 副作用：无。
func (s *SettingsService) ResolveBackgroundFile(name string) (string, error) {
	safeName, err := sanitizeBackgroundFilename(name)
	if err != nil {
		return "", err
	}
	dir, err := s.requireBackgroundsDir()
	if err != nil {
		return "", err
	}
	full := filepath.Join(dir, safeName)
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("背景图不存在")
		}
		return "", fmt.Errorf("读取背景图失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("背景图不存在")
	}
	return full, nil
}

// DeleteBackgroundImage 删除本地背景图。
// 参数/返回：name 文件名；成功无返回值。
// 失败场景：文件名非法、文件不存在、删除失败。
// 副作用：删除本地文件。
func (s *SettingsService) DeleteBackgroundImage(name string) error {
	full, err := s.ResolveBackgroundFile(name)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil {
		return fmt.Errorf("删除背景图失败: %w", err)
	}
	logx.Infof(backgroundModuleName, "删除背景图 name=%s", name)
	return nil
}

func (s *SettingsService) backgroundsDir() string {
	return filepath.Join(s.dataDir, backgroundDirName)
}

func (s *SettingsService) requireBackgroundsDir() (string, error) {
	if strings.TrimSpace(s.dataDir) == "" {
		return "", fmt.Errorf("数据目录未配置")
	}
	dir := s.backgroundsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建背景图目录失败: %w", err)
	}
	return dir, nil
}

func backgroundPublicURL(name string) string {
	return backgroundURLPrefix + name
}

func isAllowedBackgroundExt(filename string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func sanitizeBackgroundFilename(filename string) (string, error) {
	base := filepath.Base(strings.TrimSpace(filename))
	base = strings.ReplaceAll(base, "\\", "")
	base = strings.ReplaceAll(base, "/", "")
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("文件名非法")
	}
	if !isAllowedBackgroundExt(base) {
		return "", fmt.Errorf("仅支持 png/jpg/jpeg/gif/webp")
	}
	// 仅保留安全字符，避免奇怪文件名。
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.' || r == '_' || r == '-':
			return r
		default:
			return '_'
		}
	}, base)
	if !isAllowedBackgroundExt(cleaned) {
		return "", fmt.Errorf("文件名非法")
	}
	return cleaned, nil
}

func uniqueBackgroundName(dir, name string) string {
	candidate := name
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; ; i++ {
		if i > 0 {
			candidate = fmt.Sprintf("%s_%d%s", stem, i, ext)
		}
		if _, err := os.Stat(filepath.Join(dir, candidate)); os.IsNotExist(err) {
			return candidate
		}
	}
}

func extensionFromContentType(contentType string) string {
	mediaType := strings.TrimSpace(strings.Split(contentType, ";")[0])
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}