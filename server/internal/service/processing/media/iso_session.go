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

type mediaPathCandidate struct {
	Path         string
	Source       string
	AllowDirScan bool
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
	return BuildMediaPathCandidatesForRoots([]string{savePath}, torrentName, contentName)
}

// BuildMediaPathCandidatesForRoots 为多组保存路径生成媒体访问候选路径列表。
// 参数/返回：savePaths 为按优先级排列的保存路径根；torrentName/contentName 为候选子路径；返回去重后的候选路径顺序列表。
// 失败场景：无。
// 副作用：无。
func BuildMediaPathCandidatesForRoots(savePaths []string, torrentName, contentName string) []string {
	candidates := buildMediaPathCandidates(savePaths, torrentName, contentName)
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.Path)
	}
	return out
}

// ResolveMediaAccessByCandidates 按候选路径顺序解析本地可访问媒体路径。
// 参数/返回：savePath/torrentName/contentName 用于生成候选路径；scene 用于日志标记；返回首个可访问结果。
// 失败场景：所有候选均不存在或无法访问时返回聚合错误。
// 副作用：可能调用系统挂载命令并创建临时挂载点。
func ResolveMediaAccessByCandidates(savePath, torrentName, contentName, scene string) (*LocalMediaAccess, error) {
	return ResolveMediaAccessByRoots([]string{savePath}, torrentName, contentName, scene)
}

// ResolveMediaAccessByRoots 按多个保存路径根解析首个可访问的本地媒体路径。
// 参数/返回：savePaths 为按优先级排列的保存路径根；torrentName/contentName 为候选子路径；scene 用于日志标记；返回首个可访问结果。
// 失败场景：所有候选均不存在、目录扫描被禁止或 ISO 挂载失败时返回聚合错误。
// 副作用：可能调用系统挂载命令并创建临时挂载点。
func ResolveMediaAccessByRoots(savePaths []string, torrentName, contentName, scene string) (*LocalMediaAccess, error) {
	candidates := buildMediaPathCandidates(savePaths, torrentName, contentName)
	return resolveMediaAccessByCandidateList(candidates, scene)
}

// ResolveMediaAccessForPath 解析单个候选路径，必要时自动挂载 ISO。
// 参数/返回：candidate 为待解析路径；scene 用于日志标记；返回可直接访问的本地路径结果。
// 失败场景：候选路径不存在、目录内未找到可访问媒体、ISO 挂载失败时返回错误。
// 副作用：可能调用系统挂载命令并创建临时挂载点。
func ResolveMediaAccessForPath(candidate string, scene string) (*LocalMediaAccess, error) {
	return resolveMediaAccessForCandidate(mediaPathCandidate{
		Path:         candidate,
		Source:       "manual",
		AllowDirScan: true,
	}, scene)
}

// ResolveMediaTargetByCandidates 按候选路径顺序定位实际可分析的媒体文件。
// 参数/返回：savePath/torrentName/contentName 用于生成候选路径；scene 用于日志标记；返回包含目标文件与会话的解析结果。
// 失败场景：所有候选均无法解析出真实媒体文件时返回错误。
// 副作用：可能调用系统挂载命令并创建临时挂载点。
func ResolveMediaTargetByCandidates(savePath, torrentName, contentName, scene string) (*ResolvedMediaTarget, error) {
	return ResolveMediaTargetByRoots([]string{savePath}, torrentName, contentName, scene)
}

// ResolveMediaTargetByRoots 按多个保存路径根定位可分析的真实媒体文件。
// 参数/返回：savePaths 为按优先级排列的保存路径根；torrentName/contentName 为候选子路径；scene 用于日志标记；返回包含目标文件与会话的解析结果。
// 失败场景：所有候选均无法解析出真实媒体文件时返回聚合错误。
// 副作用：可能调用系统挂载命令并创建临时挂载点。
func ResolveMediaTargetByRoots(savePaths []string, torrentName, contentName, scene string) (*ResolvedMediaTarget, error) {
	candidates := buildMediaPathCandidates(savePaths, torrentName, contentName)
	return resolveMediaTargetByCandidateList(candidates, scene)
}

// ResolveMediaTargetForPath 解析单个路径对应的真实媒体文件。
// 参数/返回：path 为单个候选路径；scene 用于日志标记；返回目标文件与会话信息。
// 失败场景：路径不存在、媒体文件定位失败、ISO 挂载失败时返回错误。
// 副作用：可能调用系统挂载命令并创建临时挂载点。
func ResolveMediaTargetForPath(path string, scene string) (*ResolvedMediaTarget, error) {
	return resolveMediaTargetForCandidate(mediaPathCandidate{
		Path:         path,
		Source:       "manual",
		AllowDirScan: true,
	}, scene)
}

func resolveMediaAccessByCandidateList(candidates []mediaPathCandidate, scene string) (*LocalMediaAccess, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("未找到可访问的媒体路径")
	}
	logx.Infof(isoSessionLogModule, "媒体候选列表 scene=%s candidates=%s", normalizeMediaScene(scene), formatMediaPathCandidatesForLog(candidates))

	errorsList := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		access, err := resolveMediaAccessForCandidate(candidate, scene)
		if err != nil {
			errorsList = append(errorsList, fmt.Sprintf("%s => %v", formatMediaPathCandidateLabel(candidate), err))
			continue
		}
		return access, nil
	}
	return nil, fmt.Errorf("未找到可访问的媒体路径: %s", strings.Join(errorsList, "；"))
}

func resolveMediaTargetByCandidateList(candidates []mediaPathCandidate, scene string) (*ResolvedMediaTarget, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("未找到可用于分析的媒体文件")
	}
	logx.Infof(isoSessionLogModule, "媒体目标候选列表 scene=%s candidates=%s", normalizeMediaScene(scene), formatMediaPathCandidatesForLog(candidates))

	errorsList := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		target, err := resolveMediaTargetForCandidate(candidate, scene)
		if err != nil {
			errorsList = append(errorsList, fmt.Sprintf("%s => %v", formatMediaPathCandidateLabel(candidate), err))
			continue
		}
		return target, nil
	}
	return nil, fmt.Errorf("未找到可用于分析的媒体文件: %s", strings.Join(errorsList, "；"))
}

func resolveMediaAccessForCandidate(candidate mediaPathCandidate, scene string) (*LocalMediaAccess, error) {
	trimmed := strings.TrimSpace(candidate.Path)
	if trimmed == "" {
		return nil, errors.New("候选路径为空")
	}

	logx.Infof(
		isoSessionLogModule,
		"尝试解析候选 scene=%s source=%s path=%s allow_dir_scan=%t",
		normalizeMediaScene(scene), normalizeMediaCandidateSource(candidate.Source), trimmed, candidate.AllowDirScan,
	)
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
	if !candidate.AllowDirScan {
		return nil, fmt.Errorf("候选目录存在，但当前场景禁止在目录内回退扫描媒体文件: %s", trimmed)
	}

	leafPath, pickErr := pickMediaEntry(trimmed, true)
	if pickErr != nil {
		return nil, pickErr
	}
	logx.Infof(
		isoSessionLogModule,
		"目录候选命中叶子节点 scene=%s source=%s base=%s leaf=%s",
		normalizeMediaScene(scene), normalizeMediaCandidateSource(candidate.Source), trimmed, leafPath,
	)
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

func resolveMediaTargetForCandidate(candidate mediaPathCandidate, scene string) (*ResolvedMediaTarget, error) {
	access, err := resolveMediaAccessForCandidate(candidate, scene)
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

func buildMediaPathCandidates(savePaths []string, torrentName, contentName string) []mediaPathCandidate {
	trimmedTorrentName := strings.TrimSpace(torrentName)
	trimmedContentName := strings.TrimSpace(contentName)

	candidates := make([]mediaPathCandidate, 0, len(savePaths)*3)
	indexByPath := map[string]int{}
	for _, savePath := range savePaths {
		trimmedSavePath := strings.TrimSpace(savePath)
		if trimmedSavePath == "" {
			continue
		}
		allowSavePathDirScan := shouldAllowSavePathDirScan(trimmedSavePath, trimmedTorrentName, trimmedContentName)
		if trimmedTorrentName != "" {
			appendMediaPathCandidate(&candidates, indexByPath, mediaPathCandidate{
				Path:         filepath.Join(trimmedSavePath, trimmedTorrentName),
				Source:       "torrent_name",
				AllowDirScan: true,
			})
		}
		if trimmedContentName != "" && !strings.EqualFold(trimmedContentName, trimmedTorrentName) {
			appendMediaPathCandidate(&candidates, indexByPath, mediaPathCandidate{
				Path:         filepath.Join(trimmedSavePath, trimmedContentName),
				Source:       "content_name",
				AllowDirScan: true,
			})
		}
		appendMediaPathCandidate(&candidates, indexByPath, mediaPathCandidate{
			Path:         trimmedSavePath,
			Source:       "save_path",
			AllowDirScan: allowSavePathDirScan,
		})
	}
	return candidates
}

func shouldAllowSavePathDirScan(savePath, torrentName, contentName string) bool {
	return strings.TrimSpace(savePath) != ""
}

func appendMediaPathCandidate(candidates *[]mediaPathCandidate, indexByPath map[string]int, candidate mediaPathCandidate) {
	candidate.Path = strings.TrimSpace(candidate.Path)
	if candidate.Path == "" {
		return
	}
	if idx, exists := indexByPath[candidate.Path]; exists {
		current := (*candidates)[idx]
		if mediaCandidateSourcePriority(candidate.Source) > mediaCandidateSourcePriority(current.Source) {
			current.Source = candidate.Source
		}
		current.AllowDirScan = current.AllowDirScan || candidate.AllowDirScan
		(*candidates)[idx] = current
		return
	}
	indexByPath[candidate.Path] = len(*candidates)
	*candidates = append(*candidates, candidate)
}

func mediaCandidateSourcePriority(source string) int {
	switch normalizeMediaCandidateSource(source) {
	case "torrent_name":
		return 3
	case "content_name":
		return 2
	case "save_path":
		return 1
	default:
		return 0
	}
}

func formatMediaPathCandidatesForLog(candidates []mediaPathCandidate) string {
	parts := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		parts = append(parts, fmt.Sprintf("%s{%s scan=%t}", normalizeMediaCandidateSource(candidate.Source), candidate.Path, candidate.AllowDirScan))
	}
	return strings.Join(parts, ", ")
}

func formatMediaPathCandidateLabel(candidate mediaPathCandidate) string {
	return fmt.Sprintf("%s[%s]", normalizeMediaCandidateSource(candidate.Source), strings.TrimSpace(candidate.Path))
}

func normalizeMediaCandidateSource(source string) string {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
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

func buildLinuxDockerISOMountHint() string {
	return "若运行在原生 Linux Docker，请设置 PTNEXUS_ISO_MOUNT_ROOT=/app/data/tmp/iso-mounts，并为容器增加 cap_add: [SYS_ADMIN]、devices: /dev/loop-control 与 /dev/loop0..3；Docker Desktop / WSL 不支持容器内自动挂载 ISO"
}
