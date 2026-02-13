package bdflow

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pt-nexus/server-go/internal/platform/logx"
)

const bdinfoToolLogModule = "BDInfo任务"

// FindBlurayRootPath 从给定路径向上回溯，查找蓝光目录根路径。
// 参数/返回：rawPath 可为文件或目录；命中则返回包含 BDMV/CERTIFICATE 的根目录。
// 失败场景：路径不存在或不满足蓝光目录结构时返回空字符串。
// 副作用：访问本地文件系统。
func FindBlurayRootPath(rawPath string) string {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return ""
	}
	info, err := os.Stat(trimmed)
	if err != nil {
		return ""
	}

	current := trimmed
	if !info.IsDir() {
		current = filepath.Dir(trimmed)
	}
	current = strings.TrimSpace(current)
	if current == "" {
		return ""
	}

	for depth := 0; depth < 10; depth++ {
		if isDir(pathJoin(current, "BDMV")) && isDir(pathJoin(current, "CERTIFICATE")) {
			return current
		}
		if strings.EqualFold(filepath.Base(current), "BDMV") {
			parent := filepath.Dir(current)
			if isDir(pathJoin(parent, "CERTIFICATE")) {
				return parent
			}
		}
		next := filepath.Dir(current)
		if next == current {
			break
		}
		current = next
	}

	return ""
}

// ExtractBDInfo 调用系统 BDInfo 工具提取蓝光盘信息文本。
// 参数/返回：blurayRoot 为蓝光根目录；返回 BDInfo 文本输出。
// 失败场景：找不到 BDInfo 可执行文件、执行失败、输出为空时返回错误。
// 副作用：调用外部命令并创建临时文件。
func ExtractBDInfo(blurayRoot string) (string, error) {
	bdinfoBin, err := resolveBDInfoBinaryPath()
	if err != nil {
		return "", err
	}

	tmpFile, err := os.CreateTemp("", "ptnexus-bdinfo-*.txt")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer os.Remove(tmpPath)

	cmd := exec.Command(bdinfoBin, "-p", blurayRoot, "-o", tmpPath, "-m")
	output, execErr := cmd.CombinedOutput()
	outputText := strings.TrimSpace(string(output))
	if execErr != nil {
		if outputText == "" {
			outputText = execErr.Error()
		}
		return "", fmt.Errorf("BDInfo 执行失败: %s", outputText)
	}

	data, readErr := os.ReadFile(tmpPath)
	if readErr != nil {
		if outputText != "" {
			return outputText, nil
		}
		return "", fmt.Errorf("读取 BDInfo 输出失败: %w", readErr)
	}

	text := strings.TrimSpace(string(data))
	if text == "" && outputText != "" {
		text = outputText
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("BDInfo 输出为空")
	}

	if !strings.Contains(text, "DISC INFO") && !strings.Contains(text, "PLAYLIST REPORT") {
		if outputText != "" && (strings.Contains(outputText, "DISC INFO") || strings.Contains(outputText, "PLAYLIST REPORT")) {
			text = outputText
		}
	}
	return text, nil
}

func resolveBDInfoBinaryPath() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("PTNEXUS_BDINFO_PATH")); configured != "" {
		if _, err := os.Stat(configured); err == nil {
			logx.Infof(bdinfoToolLogModule, "使用环境变量指定 BDInfo 可执行文件 path=%s", configured)
			return configured, nil
		}
		return "", fmt.Errorf("PTNEXUS_BDINFO_PATH 指向的文件不存在: %s", configured)
	}

	isWindows := runtime.GOOS == "windows"
	binaryCandidates := []string{"BDInfo", "BDInfo.exe"}
	if isWindows {
		binaryCandidates = []string{"BDInfo.exe", "BDInfo"}
	}

	candidateDirs := make([]string, 0)
	seenDirs := map[string]struct{}{}
	addDir := func(dir string) {
		trimmed := strings.TrimSpace(dir)
		if trimmed == "" {
			return
		}
		cleaned := filepath.Clean(trimmed)
		if _, exists := seenDirs[cleaned]; exists {
			return
		}
		seenDirs[cleaned] = struct{}{}
		candidateDirs = append(candidateDirs, cleaned)
	}

	if dir := strings.TrimSpace(os.Getenv("PTNEXUS_BDINFO_DIR")); dir != "" {
		addDir(dir)
		addDir(pathJoin(dir, "linux"))
		addDir(pathJoin(dir, "windows"))
	}

	if baseDir := strings.TrimSpace(os.Getenv("PTNEXUS_BASE_DIR")); baseDir != "" {
		addDir(pathJoin(baseDir, "bdinfo"))
		addDir(pathJoin(baseDir, "bdinfo", "linux"))
		addDir(pathJoin(baseDir, "bdinfo", "windows"))
		addDir(pathJoin(baseDir, "server-go", "bdinfo"))
		addDir(pathJoin(baseDir, "server-go", "bdinfo", "linux"))
		addDir(pathJoin(baseDir, "server-go", "bdinfo", "windows"))
	}

	if cwd, err := os.Getwd(); err == nil {
		roots := []string{cwd, filepath.Dir(cwd), filepath.Dir(filepath.Dir(cwd))}
		for _, root := range roots {
			addDir(pathJoin(root, "server-go", "bdinfo"))
			addDir(pathJoin(root, "server-go", "bdinfo", "linux"))
			addDir(pathJoin(root, "server-go", "bdinfo", "windows"))
			addDir(pathJoin(root, "bdinfo"))
			addDir(pathJoin(root, "bdinfo", "linux"))
			addDir(pathJoin(root, "bdinfo", "windows"))
		}
	}

	addDir("/app/server-go/bdinfo")
	addDir("/app/server-go/bdinfo/linux")
	addDir("/app/server-go/bdinfo/windows")
	addDir("/app/bdinfo")
	addDir("/app/bdinfo/linux")
	addDir("/app/bdinfo/windows")

	tried := make([]string, 0)
	for _, dir := range candidateDirs {
		for _, name := range binaryCandidates {
			candidate := pathJoin(dir, name)
			tried = append(tried, candidate)
			if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
				logx.Infof(bdinfoToolLogModule, "从候选路径找到 BDInfo 可执行文件 path=%s", candidate)
				return candidate, nil
			}
		}
	}

	for _, name := range []string{"BDInfo", "BDInfo.exe", "bdinfo"} {
		if found, err := exec.LookPath(name); err == nil {
			logx.Infof(bdinfoToolLogModule, "从系统PATH找到 BDInfo 可执行文件 path=%s", found)
			return found, nil
		}
	}

	if len(tried) > 0 {
		return "", fmt.Errorf("未找到 BDInfo 可执行文件，请设置 PTNEXUS_BDINFO_PATH 或 PTNEXUS_BDINFO_DIR（已检查: %s）", strings.Join(tried, ", "))
	}
	return "", fmt.Errorf("未找到 BDInfo 可执行文件，请设置 PTNEXUS_BDINFO_PATH 或 PTNEXUS_BDINFO_DIR")
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func pathJoin(base string, more ...string) string {
	parts := append([]string{base}, more...)
	return filepath.Join(parts...)
}
