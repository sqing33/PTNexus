package logexport

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
)

const (
	defaultExportWindowHours = 24
	logTimestampLayout       = "2006/01/02 15:04:05"
	maxLogLineBytes          = 8 * 1024 * 1024
)

// ExportService 负责收集并导出后端日志文件。
type ExportService struct {
	logDir       string
	primaryFile  string
	exportWindow time.Duration
	maxBytes     int64
	nowFunc      func() time.Time
}

// ExportFile 描述单个可导出的日志文件元信息。
type ExportFile struct {
	Path    string
	Name    string
	Size    int64
	ModTime time.Time
}

// ExportPlan 描述一次日志导出的文件清单与总大小。
type ExportPlan struct {
	Files     []ExportFile
	TotalSize int64
}

// ExportResult 描述一次日志导出的窗口信息与输出统计。
type ExportResult struct {
	WindowHours   int
	Cutoff        time.Time
	ExportedLines int64
	ExportedBytes int64
}

// New 创建日志导出服务实例并读取目录、主日志文件与导出时间窗口。
// 参数/返回：无入参，返回可复用的导出服务。
// 失败场景：日志目录无法转绝对路径时回退原值继续运行，不直接报错。
// 副作用：读取环境变量和日志组件当前配置，不写入外部状态。
func New() *ExportService {
	logDir := strings.TrimSpace(logx.GetLogDir())
	if logDir == "" {
		logDir = strings.TrimSpace(os.Getenv("PTNEXUS_LOG_DIR"))
	}
	if logDir == "" {
		logDir = "./data/logs"
	}
	if abs, err := filepath.Abs(logDir); err == nil {
		logDir = abs
	}

	primaryPath := strings.TrimSpace(logx.GetPrimaryLogFile())
	primaryFile := strings.TrimSpace(filepath.Base(primaryPath))
	if primaryFile == "" || primaryFile == "." {
		primaryFile = strings.TrimSpace(os.Getenv("PTNEXUS_LOG_FILE"))
	}
	if primaryFile == "" {
		primaryFile = "ptnexus.log"
	}

	maxMB := 200
	if raw := strings.TrimSpace(os.Getenv("PTNEXUS_LOG_EXPORT_MAX_MB")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxMB = parsed
		}
	}

	return &ExportService{
		logDir:       logDir,
		primaryFile:  primaryFile,
		exportWindow: defaultExportWindowHours * time.Hour,
		maxBytes:     int64(maxMB) * 1024 * 1024,
		nowFunc:      time.Now,
	}
}

// PrepareExport 扫描日志目录并返回导出计划。
// 参数/返回：无入参，返回候选日志文件列表与候选总大小。
// 失败场景：日志目录不可读或未匹配到任何日志文件时返回错误。
// 副作用：仅读取文件系统元数据，不修改任何日志文件。
func (s *ExportService) PrepareExport() (ExportPlan, error) {
	entries, err := os.ReadDir(s.logDir)
	if err != nil {
		return ExportPlan{}, fmt.Errorf("读取日志目录失败: %w", err)
	}

	base := strings.TrimSuffix(s.primaryFile, filepath.Ext(s.primaryFile))
	ext := filepath.Ext(s.primaryFile)
	files := make([]ExportFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !s.matchLogFile(name, base, ext) {
			continue
		}

		path := filepath.Join(s.logDir, name)
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}

		files = append(files, ExportFile{
			Path:    path,
			Name:    name,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}

	if len(files) == 0 {
		return ExportPlan{}, fmt.Errorf("暂无可导出的日志文件")
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].ModTime.Equal(files[j].ModTime) {
			return files[i].Name < files[j].Name
		}
		return files[i].ModTime.Before(files[j].ModTime)
	})

	totalSize := int64(0)
	for _, file := range files {
		totalSize += file.Size
	}

	return ExportPlan{Files: files, TotalSize: totalSize}, nil
}

// StreamExport 将导出计划中的日志按“最近24小时”过滤后写入目标输出流。
// 参数/返回：输入响应输出流与导出计划，返回导出统计信息。
// 失败场景：读取文件、解压 gzip、扫描超长行或写出失败时返回错误。
// 副作用：按时间顺序读取本地日志文件并向 HTTP 响应流持续写入文本。
func (s *ExportService) StreamExport(w io.Writer, plan ExportPlan) (ExportResult, error) {
	window := s.exportWindow
	if window <= 0 {
		window = defaultExportWindowHours * time.Hour
	}
	now := time.Now()
	if s.nowFunc != nil {
		now = s.nowFunc()
	}
	cutoff := now.Add(-window)
	result := ExportResult{
		WindowHours: int(window.Hours()),
		Cutoff:      cutoff,
	}

	buffered := bufio.NewWriterSize(w, 64*1024)
	for _, file := range plan.Files {
		lines, bytesWritten, err := s.copyLogFileWithTimeFilter(buffered, file, cutoff)
		if err != nil {
			return result, fmt.Errorf("导出日志文件失败（%s）: %w", file.Name, err)
		}
		result.ExportedLines += lines
		result.ExportedBytes += bytesWritten
		if s.maxBytes > 0 && result.ExportedBytes > s.maxBytes {
			return result, fmt.Errorf(
				"最近%d小时日志体积 %.2fMB 超出导出上限 %.2fMB，请复现后尽快导出或降低日志级别",
				result.WindowHours,
				float64(result.ExportedBytes)/1024.0/1024.0,
				float64(s.maxBytes)/1024.0/1024.0,
			)
		}
	}

	if result.ExportedLines == 0 {
		emptyMessage := fmt.Sprintf("========== 最近%d小时日志 ==========\n最近%d小时暂无后端日志，请在复现问题后再次导出。\n", result.WindowHours, result.WindowHours)
		written, err := buffered.WriteString(emptyMessage)
		if err != nil {
			return result, fmt.Errorf("写入空日志提示失败: %w", err)
		}
		result.ExportedBytes += int64(written)
	}

	if err := buffered.Flush(); err != nil {
		return result, fmt.Errorf("刷新导出输出流失败: %w", err)
	}
	return result, nil
}

// copyLogFileWithTimeFilter 将单个日志文件按时间窗口过滤后写入目标流。
func (s *ExportService) copyLogFileWithTimeFilter(dst *bufio.Writer, file ExportFile, cutoff time.Time) (int64, int64, error) {
	reader, closeReader, err := openLogReader(file.Path)
	if err != nil {
		return 0, 0, err
	}
	defer closeReader()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 256*1024), maxLogLineBytes)

	includeCurrentRecord := false
	wroteHeader := false
	var exportedLines int64
	var exportedBytes int64
	timezone := cutoff.Location()

	for scanner.Scan() {
		line := scanner.Text()
		if ts, ok := parseLogTimestamp(line, timezone); ok {
			includeCurrentRecord = !ts.Before(cutoff)
		}
		if !includeCurrentRecord {
			continue
		}

		if !wroteHeader {
			header := fmt.Sprintf("\n========== 日志文件: %s（修改时间: %s）==========\n", file.Name, file.ModTime.Format("2006-01-02 15:04:05"))
			written, writeErr := dst.WriteString(header)
			if writeErr != nil {
				return exportedLines, exportedBytes, fmt.Errorf("写入日志分段头失败: %w", writeErr)
			}
			exportedBytes += int64(written)
			wroteHeader = true
		}

		written, writeErr := dst.WriteString(line + "\n")
		if writeErr != nil {
			return exportedLines, exportedBytes, fmt.Errorf("写入日志内容失败: %w", writeErr)
		}
		exportedBytes += int64(written)
		exportedLines++
	}
	if err := scanner.Err(); err != nil {
		return exportedLines, exportedBytes, fmt.Errorf("扫描日志文件失败: %w", err)
	}
	if wroteHeader {
		written, writeErr := dst.WriteString("\n")
		if writeErr != nil {
			return exportedLines, exportedBytes, fmt.Errorf("写入日志分段换行失败: %w", writeErr)
		}
		exportedBytes += int64(written)
	}
	return exportedLines, exportedBytes, nil
}

// parseLogTimestamp 从日志行首提取时间戳，兼容 logx 与标准日志前缀。
func parseLogTimestamp(line string, location *time.Location) (time.Time, bool) {
	if len(line) < len(logTimestampLayout) {
		return time.Time{}, false
	}

	segment := line[:len(logTimestampLayout)]
	if segment[4] != '/' || segment[7] != '/' || segment[10] != ' ' || segment[13] != ':' || segment[16] != ':' {
		return time.Time{}, false
	}

	ts, err := time.ParseInLocation(logTimestampLayout, segment, location)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

// openLogReader 打开普通日志或 gzip 轮转日志并返回可读流。
func openLogReader(filePath string) (io.Reader, func(), error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, err
	}

	closeAll := func() {
		_ = file.Close()
	}

	if strings.HasSuffix(strings.ToLower(filePath), ".gz") {
		gzipReader, gzipErr := gzip.NewReader(file)
		if gzipErr != nil {
			closeAll()
			return nil, nil, fmt.Errorf("打开 gzip 日志失败: %w", gzipErr)
		}
		closeAll = func() {
			_ = gzipReader.Close()
			_ = file.Close()
		}
		return gzipReader, closeAll, nil
	}
	return file, closeAll, nil
}

// matchLogFile 判断文件名是否属于当前主日志及其轮转日志。
func (s *ExportService) matchLogFile(name string, base string, ext string) bool {
	if name == s.primaryFile {
		return true
	}
	if !strings.HasPrefix(name, base+"-") {
		return false
	}
	if strings.Contains(name, ext) {
		return true
	}
	return false
}
