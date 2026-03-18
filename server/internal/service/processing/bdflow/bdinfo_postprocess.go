package bdflow

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var (
	bdinfoMultiBlankLinePattern = regexp.MustCompile(`\n{3,}`)
	bdinfoColonBlankLinePattern = regexp.MustCompile(`:\s*\n\s*\n`)
)

type bdinfoPostProcessResult struct {
	Content         string
	UsedSubstractor bool
	UsedInvariant   bool
	FallbackReason  string
}

func resolveBDInfoDataSubstractorPath(bdinfoPath string) string {
	dir := strings.TrimSpace(filepath.Dir(strings.TrimSpace(bdinfoPath)))
	if dir == "" {
		return ""
	}

	candidates := []string{"BDInfoDataSubstractor"}
	if runtime.GOOS == "windows" {
		candidates = []string{"BDInfoDataSubstractor.exe", "BDInfoDataSubstractor"}
	}

	for _, name := range candidates {
		candidate := filepath.Join(dir, name)
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			return candidate
		}
	}
	return ""
}

func postProcessBDInfoOutput(inputFile string, substractorPath string) (bdinfoPostProcessResult, error) {
	content, err := os.ReadFile(inputFile)
	if err != nil {
		return bdinfoPostProcessResult{}, fmt.Errorf("读取 BDInfo 原始输出失败: %w", err)
	}

	result := bdinfoPostProcessResult{
		Content: normalizeBDInfoText(string(content)),
	}

	trimmedSubstractor := strings.TrimSpace(substractorPath)
	if trimmedSubstractor == "" {
		result.FallbackReason = "未找到 BDInfoDataSubstractor，可执行文件与 BDInfo 不在同目录"
		return result, nil
	}

	output, runErr, usedInvariant := runDotnetToolWithICUFallback(trimmedSubstractor, inputFile)
	result.UsedInvariant = usedInvariant
	if runErr != nil {
		text := strings.TrimSpace(string(output))
		if text == "" {
			text = runErr.Error()
		}
		result.FallbackReason = "BDInfoDataSubstractor 执行失败: " + text
		return result, nil
	}

	baseWithoutExt := strings.TrimSuffix(inputFile, filepath.Ext(inputFile))
	outputFile := baseWithoutExt + ".bdinfo.txt"
	quickSummaryFile := baseWithoutExt + ".quicksummary.txt"
	defer cleanupBDInfoGeneratedFiles(outputFile, quickSummaryFile)

	processedContent, readErr := os.ReadFile(outputFile)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			result.FallbackReason = "BDInfoDataSubstractor 未生成输出文件"
			return result, nil
		}
		result.FallbackReason = fmt.Sprintf("读取 BDInfoDataSubstractor 输出失败: %v", readErr)
		return result, nil
	}

	normalized := normalizeBDInfoText(string(processedContent))
	if normalized == "" {
		result.FallbackReason = "BDInfoDataSubstractor 输出为空"
		return result, nil
	}

	result.Content = normalized
	result.UsedSubstractor = true
	return result, nil
}

func runDotnetToolWithICUFallback(bin string, args ...string) ([]byte, error, bool) {
	output, err := exec.Command(bin, args...).CombinedOutput()
	if err == nil || !bdinfoNeedsInvariantMode(output, err) {
		return output, err, false
	}

	retryCmd := exec.Command(bin, args...)
	retryCmd.Env = append(os.Environ(), "DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1")
	retryOutput, retryErr := retryCmd.CombinedOutput()
	return retryOutput, retryErr, true
}

func cleanupBDInfoGeneratedFiles(paths ...string) {
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		_ = os.Remove(trimmed)
	}
}

func normalizeBDInfoText(content string) string {
	filtered := bdinfoMultiBlankLinePattern.ReplaceAllString(content, "\n\n")
	filtered = bdinfoColonBlankLinePattern.ReplaceAllString(filtered, ":\n")
	return strings.TrimSpace(filtered)
}
