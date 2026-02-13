package fetch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var videoFileExts = map[string]struct{}{
	".mp4":  {},
	".mkv":  {},
	".avi":  {},
	".mov":  {},
	".wmv":  {},
	".flv":  {},
	".webm": {},
	".m4v":  {},
	".mpg":  {},
	".mpeg": {},
	".ts":   {},
	".m2ts": {},
	".vob":  {},
	".iso":  {},
}

// ExtractVideoSizeFromTorrentFile 解析 torrent 文件并统计视频文件总体积。
// 参数/返回：torrentPath 为 torrent 文件路径；返回视频文件总体积（字节）、最大视频文件名（仅文件名）与错误。
// 失败场景：文件读取失败、torrent 结构非法、未包含任何视频文件时返回错误。
// 副作用：读取磁盘文件。
func ExtractVideoSizeFromTorrentFile(torrentPath string) (int64, string, error) {
	trimmed := strings.TrimSpace(torrentPath)
	if trimmed == "" {
		return 0, "", errors.New("torrent 路径为空")
	}
	content, err := os.ReadFile(trimmed)
	if err != nil {
		return 0, "", fmt.Errorf("读取 torrent 文件失败: %w", err)
	}
	return extractVideoSizeFromTorrentBytes(content)
}

func extractVideoSizeFromTorrentBytes(content []byte) (int64, string, error) {
	if len(content) == 0 {
		return 0, "", errors.New("torrent 内容为空")
	}
	p := &bdecodeParser{data: content}
	if err := p.expect('d'); err != nil {
		return 0, "", err
	}

	var infoValue any
	for p.idx < len(p.data) && p.data[p.idx] != 'e' {
		keyBytes, err := p.parseBytes()
		if err != nil {
			return 0, "", err
		}
		key := string(keyBytes)
		if key == "info" {
			value, err := p.parseValue()
			if err != nil {
				return 0, "", err
			}
			infoValue = value
			continue
		}
		if _, err := p.parseValue(); err != nil {
			return 0, "", err
		}
	}
	if err := p.expect('e'); err != nil {
		return 0, "", err
	}

	if infoValue == nil {
		return 0, "", errors.New("torrent 缺少 info 字段")
	}
	infoMap, ok := infoValue.(map[string]any)
	if !ok {
		return 0, "", errors.New("torrent info 结构异常")
	}

	total, largestName, _ := extractVideoSizeFromInfo(infoMap)
	if total <= 0 {
		return 0, "", errors.New("torrent 未包含视频文件")
	}
	return total, largestName, nil
}

func extractVideoSizeFromInfo(info map[string]any) (int64, string, int64) {
	files, ok := info["files"].([]any)
	if !ok {
		name := strings.TrimSpace(firstNonEmptyString(info["name.utf-8"], info["name"]))
		length := toInt64Any(info["length"])
		if isVideoFileName(name) && length > 0 {
			return length, name, length
		}
		return 0, "", 0
	}

	var total int64
	var largestName string
	var largestSize int64

	for _, item := range files {
		fileMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		length := toInt64Any(fileMap["length"])
		if length <= 0 {
			continue
		}

		pathValue, ok := fileMap["path.utf-8"]
		if !ok {
			pathValue = fileMap["path"]
		}
		pathList, ok := pathValue.([]any)
		if !ok || len(pathList) == 0 {
			continue
		}
		name := strings.TrimSpace(toStringAny(pathList[len(pathList)-1], ""))
		if !isVideoFileName(name) {
			continue
		}

		total += length
		if length > largestSize {
			largestSize = length
			largestName = name
		}
	}

	return total, largestName, largestSize
}

func isVideoFileName(name string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	if ext == "" {
		return false
	}
	_, ok := videoFileExts[ext]
	return ok
}
