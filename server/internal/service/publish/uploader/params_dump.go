package uploader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pt-nexus/server-go/internal/config"
	"github.com/pt-nexus/server-go/internal/platform/logx"
)

const publishParamsDumpLogModule = "发布-参数落盘"

var reDumpTorrentFilename = regexp.MustCompile(`^([^-]+)-(\d+)-`)

// DumpUploadParametersToTmp 在 DEV_ENV=true 时将发布表单与标准化参数落盘到 data/tmp/torrents 目录，便于排查发布参数映射问题。
// 参数/返回：targetName 为目标站点昵称；torrentPath 用于从文件名推断源站点与 torrent_id；formFields 为最终提交的表单字段；
// uploadData 为发布 payload（用于提取 standardized_params）；finalMainTitle/description 为最终标题与描述正文；subtitle/imdbLink/doubanLink/mediainfo 用于生成摘要字段。
// 失败场景：创建目录失败、JSON 编码失败、写文件失败等返回 error；非 DEV_ENV 场景返回空路径与 nil。
// 副作用：在 data/tmp/torrents 写入 JSON 文件，不包含 Cookie 等敏感信息。
func DumpUploadParametersToTmp(
	targetName string,
	torrentPath string,
	formFields map[string]string,
	uploadData map[string]any,
	finalMainTitle string,
	description string,
	subtitle string,
	imdbLink string,
	doubanLink string,
	mediainfo string,
) (string, error) {
	if os.Getenv("DEV_ENV") != "true" {
		return "", nil
	}

	paths := config.ResolveRuntimePaths()
	torrentDir := filepath.Join(paths.DataDir, "tmp", "torrents")
	if err := os.MkdirAll(torrentDir, 0o755); err != nil {
		return "", err
	}

	sourceSiteCode := "unknown"
	torrentID := "unknown"
	if base := strings.TrimSpace(filepath.Base(torrentPath)); base != "" {
		if match := reDumpTorrentFilename.FindStringSubmatch(base); len(match) >= 3 {
			sourceSiteCode = strings.TrimSpace(match[1])
			torrentID = strings.TrimSpace(match[2])
		}
	}

	timestamp := time.Now().Format("2006-01-02-15:04:05")
	filename := fmt.Sprintf(
		"%s-%s-%s-%s.json",
		sanitizeDumpFilePart(sourceSiteCode),
		sanitizeDumpFilePart(torrentID),
		sanitizeDumpFilePart(targetName),
		timestamp,
	)
	filePath := filepath.Join(torrentDir, filename)

	standardizedParams := map[string]any{}
	if uploadData != nil {
		if typed, ok := uploadData["standardized_params"].(map[string]any); ok && typed != nil {
			standardizedParams = typed
		}
	}

	saveData := map[string]any{
		"site_name":           strings.TrimSpace(targetName),
		"timestamp":           timestamp,
		"form_data":           formFields,
		"standardized_params": standardizedParams,
		"final_main_title":    strings.TrimSpace(finalMainTitle),
		"description":         description,
		"upload_data_summary": map[string]any{
			"subtitle":              strings.TrimSpace(subtitle),
			"douban_link":           strings.TrimSpace(doubanLink),
			"imdb_link":             strings.TrimSpace(imdbLink),
			"mediainfo_length":      len(mediainfo),
			"modified_torrent_path": strings.TrimSpace(torrentPath),
		},
	}

	content, err := json.MarshalIndent(saveData, "", "  ")
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		return "", err
	}

	logx.Infof(publishParamsDumpLogModule, "发布参数已保存 path=%s", filePath)
	return filePath, nil
}

func sanitizeDumpFilePart(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	trimmed = strings.ReplaceAll(trimmed, "/", "_")
	trimmed = strings.ReplaceAll(trimmed, "\\", "_")
	trimmed = strings.ReplaceAll(trimmed, ":", "_")
	trimmed = strings.ReplaceAll(trimmed, "..", "_")
	trimmed = strings.ReplaceAll(trimmed, " ", "_")
	return trimmed
}
