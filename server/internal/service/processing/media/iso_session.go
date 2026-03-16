package media

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
)

const isoSessionLogModule = "媒体访问-ISO"

// MediaSession 表示一次本地媒体访问会话。
// 参数/返回：OriginalPath 为原始输入路径；ResolvedPath 为可直接访问的目录或文件路径。
// 失败场景：无。
// 副作用：Close 可能触发卸载 ISO 或清理临时挂载点。
type MediaSession struct {
	OriginalPath string
	ResolvedPath string
	Mounted      bool
	OwnedMount   bool

	closeFn func() error
}

// LocalMediaAccess 描述单个候选路径解析后的本地媒体访问结果。
// 参数/返回：SourcePath 为实际命中的原始文件或目录；ResolvedPath 为后续业务可访问的目录或文件。
// 失败场景：无。
// 副作用：Close 可能触发 ISO 卸载。
type LocalMediaAccess struct {
	Session      *MediaSession
	SourcePath   string
	ResolvedPath string
}

// ResolvedMediaTarget 表示已解析出的本地媒体目标文件。
// 参数/返回：TargetFile 为后续可传给 ffprobe/mediainfo/mpv/ffmpeg 的真实媒体文件。
// 失败场景：无。
// 副作用：Close 可能触发 ISO 卸载。
type ResolvedMediaTarget struct {
	Access       *LocalMediaAccess
	SourcePath   string
	ResolvedPath string
	TargetFile   string
}

// Close 结束本地媒体访问会话。
// 参数/返回：无；返回底层卸载/清理错误。
// 失败场景：ISO 卸载失败或临时目录清理失败时返回错误。
// 副作用：可能卸载 ISO 或删除临时挂载点。
func (s *MediaSession) Close() error {
	if s == nil || s.closeFn == nil {
		return nil
	}
	closeFn := s.closeFn
	s.closeFn = nil
	return closeFn()
}

// Close 结束单个候选路径的本地媒体访问。
// 参数/返回：无；返回底层会话关闭错误。
// 失败场景：无。
// 副作用：可能触发 ISO 卸载。
func (a *LocalMediaAccess) Close() error {
	if a == nil || a.Session == nil {
		return nil
	}
	return a.Session.Close()
}

// Close 结束媒体目标解析结果对应的本地媒体访问。
// 参数/返回：无；返回底层会话关闭错误。
// 失败场景：无。
// 副作用：可能触发 ISO 卸载。
func (r *ResolvedMediaTarget) Close() error {
	if r == nil || r.Access == nil {
		return nil
	}
	return r.Access.Close()
}

// OpenMediaSession 打开一次本地媒体访问会话，必要时自动挂载 ISO。
// 参数/返回：rawPath 为本地真实路径；scene 用于日志标记；返回可直接访问的会话对象。
// 失败场景：路径不存在、ISO 挂载失败、当前平台不支持自动挂载时返回错误。
// 副作用：可能创建挂载点、调用系统挂载命令并写业务日志。
func OpenMediaSession(rawPath string, scene string) (*MediaSession, error) {
	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return nil, errors.New("媒体路径为空")
	}
	info, err := os.Stat(trimmedPath)
	if err != nil {
		return nil, fmt.Errorf("访问媒体路径失败: %w", err)
	}
	if info.IsDir() || !isISOPath(trimmedPath) {
		return newPassthroughMediaSession(trimmedPath), nil
	}
	return openISOSession(trimmedPath, normalizeMediaScene(scene))
}

// BuildMediaPathCandidates 生成媒体访问候选路径列表。
// 参数/返回：savePath 为保存路径，torrentName/contentName 为候选子路径；返回去重前的候选路径顺序列表。
// 失败场景：无。
// 副作用：无。
func BuildMediaPathCandidates(savePath, torrentName, contentName string) []string {
	trimmedSavePath := strings.TrimSpace(savePath)
	trimmedTorrentName := strings.TrimSpace(torrentName)
	trimmedContentName := strings.TrimSpace(contentName)

	candidates := make([]string, 0, 3)
	if trimmedSavePath != "" && trimmedTorrentName != "" {
		candidates = append(candidates, filepath.Join(trimmedSavePath, trimmedTorrentName))
	}
	if trimmedSavePath != "" && trimmedContentName != "" && !strings.EqualFold(trimmedContentName, trimmedTorrentName) {
		candidates = append(candidates, filepath.Join(trimmedSavePath, trimmedContentName))
	}
	if trimmedSavePath != "" {
		candidates = append(candidates, trimmedSavePath)
	}
	return candidates
}

// ResolveMediaAccessForPath 解析单个候选路径，必要时自动挂载 ISO。
// 参数/返回：candidate 为待解析路径；scene 用于日志标记；返回可直接访问的本地路径结果。
// 失败场景：候选路径不存在、目录内未找到可访问媒体、ISO 挂载失败时返回错误。
// 副作用：可能调用系统挂载命令并创建临时挂载点。
func ResolveMediaAccessForPath(candidate string, scene string) (*LocalMediaAccess, error) {
	trimmed := strings.TrimSpace(candidate)
	if trimmed == "" {
		return nil, errors.New("候选路径为空")
	}

	info, err := os.Stat(trimmed)
	if err != nil {
		return nil, fmt.Errorf("访问候选路径失败: %w", err)
	}

	if !info.IsDir() {
		session, openErr := OpenMediaSession(trimmed, scene)
		if openErr != nil {
			return nil, openErr
		}
		return &LocalMediaAccess{
			Session:      session,
			SourcePath:   trimmed,
			ResolvedPath: session.ResolvedPath,
		}, nil
	}

	if detectBlurayDiscAtPath(trimmed) {
		session := newPassthroughMediaSession(trimmed)
		return &LocalMediaAccess{
			Session:      session,
			SourcePath:   trimmed,
			ResolvedPath: session.ResolvedPath,
		}, nil
	}

	leafPath, pickErr := pickMediaEntry(trimmed, true)
	if pickErr != nil {
		return nil, pickErr
	}
	session, openErr := OpenMediaSession(leafPath, scene)
	if openErr != nil {
		return nil, openErr
	}
	return &LocalMediaAccess{
		Session:      session,
		SourcePath:   leafPath,
		ResolvedPath: session.ResolvedPath,
	}, nil
}

// ResolveMediaAccessByCandidates 按候选路径顺序解析本地可访问媒体路径。
// 参数/返回：savePath/torrentName/contentName 用于生成候选路径；scene 用于日志标记；返回首个可访问结果。
// 失败场景：所有候选均不存在或无法访问时返回聚合错误。
// 副作用：可能调用系统挂载命令并创建临时挂载点。
func ResolveMediaAccessByCandidates(savePath, torrentName, contentName, scene string) (*LocalMediaAccess, error) {
	candidates := BuildMediaPathCandidates(savePath, torrentName, contentName)
	seen := map[string]struct{}{}
	errorsList := make([]string, 0)

	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}

		access, err := ResolveMediaAccessForPath(trimmed, scene)
		if err != nil {
			errorsList = append(errorsList, fmt.Sprintf("%s => %v", trimmed, err))
			continue
		}
		return access, nil
	}

	if len(errorsList) == 0 {
		return nil, fmt.Errorf("未找到可访问的媒体路径")
	}
	return nil, fmt.Errorf("未找到可访问的媒体路径: %s", strings.Join(errorsList, "；"))
}

// ResolveMediaTargetByCandidates 按候选路径顺序定位实际可分析的媒体文件。
// 参数/返回：savePath/torrentName/contentName 用于生成候选路径；scene 用于日志标记；返回包含目标文件与会话的解析结果。
// 失败场景：所有候选均无法解析出真实媒体文件时返回错误。
// 副作用：可能调用系统挂载命令并创建临时挂载点。
func ResolveMediaTargetByCandidates(savePath, torrentName, contentName, scene string) (*ResolvedMediaTarget, error) {
	candidates := BuildMediaPathCandidates(savePath, torrentName, contentName)
	seen := map[string]struct{}{}
	errorsList := make([]string, 0)

	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}

		target, err := ResolveMediaTargetForPath(trimmed, scene)
		if err != nil {
			errorsList = append(errorsList, fmt.Sprintf("%s => %v", trimmed, err))
			continue
		}
		return target, nil
	}

	if len(errorsList) == 0 {
		return nil, fmt.Errorf("未找到可用于分析的媒体文件")
	}
	return nil, fmt.Errorf("未找到可用于分析的媒体文件: %s", strings.Join(errorsList, "；"))
}

// ResolveMediaTargetForPath 解析单个路径对应的真实媒体文件。
// 参数/返回：path 为单个候选路径；scene 用于日志标记；返回目标文件与会话信息。
// 失败场景：路径不存在、媒体文件定位失败、ISO 挂载失败时返回错误。
// 副作用：可能调用系统挂载命令并创建临时挂载点。
func ResolveMediaTargetForPath(path string, scene string) (*ResolvedMediaTarget, error) {
	access, err := ResolveMediaAccessForPath(path, scene)
	if err != nil {
		return nil, err
	}

	targetFile, resolveErr := resolveMediaTargetFromPath(access.ResolvedPath)
	if resolveErr != nil {
		if closeErr := access.Close(); closeErr != nil {
			logx.Warnf(isoSessionLogModule, "媒体访问会话关闭失败 scene=%s path=%s err=%v", normalizeMediaScene(scene), access.SourcePath, closeErr)
		}
		return nil, resolveErr
	}

	return &ResolvedMediaTarget{
		Access:       access,
		SourcePath:   access.SourcePath,
		ResolvedPath: access.ResolvedPath,
		TargetFile:   targetFile,
	}, nil
}

func newPassthroughMediaSession(path string) *MediaSession {
	return &MediaSession{
		OriginalPath: strings.TrimSpace(path),
		ResolvedPath: strings.TrimSpace(path),
		Mounted:      false,
		OwnedMount:   false,
		closeFn:      func() error { return nil },
	}
}

func resolveMediaTargetFromPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("媒体解析路径为空")
	}

	info, err := os.Stat(trimmed)
	if err != nil {
		return "", fmt.Errorf("访问媒体解析路径失败: %w", err)
	}
	if !info.IsDir() {
		if isISOPath(trimmed) {
			return "", fmt.Errorf("ISO 路径必须先完成挂载后再解析媒体文件: %s", trimmed)
		}
		return trimmed, nil
	}
	return PickMediaTarget(trimmed)
}

func pickMediaEntry(savePath string, allowISO bool) (string, error) {
	trimmed := strings.TrimSpace(savePath)
	if trimmed == "" {
		return "", errors.New("保存路径为空")
	}

	info, err := os.Stat(trimmed)
	if err != nil {
		return "", fmt.Errorf("访问保存路径失败: %w", err)
	}
	if !info.IsDir() {
		if !allowISO && isISOPath(trimmed) {
			return "", fmt.Errorf("ISO 路径必须先完成挂载后再解析: %s", trimmed)
		}
		return trimmed, nil
	}

	allowedExt := supportedMediaExtensions(allowISO)
	largest := ""
	largestSize := int64(0)
	walkErr := filepath.WalkDir(trimmed, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if _, ok := allowedExt[ext]; !ok {
			return nil
		}
		stat, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if stat.Size() > largestSize {
			largestSize = stat.Size()
			largest = path
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if largest == "" {
		return "", fmt.Errorf("目录中未找到可分析的视频文件: %s", trimmed)
	}
	return largest, nil
}

func supportedMediaExtensions(allowISO bool) map[string]struct{} {
	out := map[string]struct{}{
		".mkv":  {},
		".mp4":  {},
		".m2ts": {},
		".ts":   {},
		".avi":  {},
	}
	if allowISO {
		out[".iso"] = struct{}{}
	}
	return out
}

func isISOPath(path string) bool {
	return strings.EqualFold(filepath.Ext(strings.TrimSpace(path)), ".iso")
}

func normalizeMediaScene(scene string) string {
	trimmed := strings.TrimSpace(scene)
	if trimmed == "" {
		return "本地媒体访问"
	}
	return trimmed
}

func resolveISOMountRoot() string {
	if configured := strings.TrimSpace(os.Getenv("PTNEXUS_ISO_MOUNT_ROOT")); configured != "" {
		return filepath.Clean(configured)
	}
	if dataDir := strings.TrimSpace(os.Getenv("PTNEXUS_DATA_DIR")); dataDir != "" {
		return filepath.Join(filepath.Clean(dataDir), "tmp", "iso-mounts")
	}
	return filepath.Join(os.TempDir(), "ptnexus", "iso-mounts")
}
