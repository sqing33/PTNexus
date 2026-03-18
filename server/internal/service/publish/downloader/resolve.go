package downloader

import (
	"strings"

	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
)

// ResolveEffectiveTarget 统一解析发布/加种链路使用的有效保存路径与下载器 ID。
// 参数/返回：payload 为请求参数，fallbackSavePath/fallbackDownloaderID 为上下文回退值，defaultDownloaderID 为默认下载器；返回最终 savePath 与 downloaderID。
// 失败场景：当显式参数、回退值和默认下载器均缺失时返回空字符串，由调用方决定是否中断流程。
// 副作用：无。
func ResolveEffectiveTarget(payload map[string]any, fallbackSavePath string, fallbackDownloaderID string, defaultDownloaderID string) (string, string) {
	if payload == nil {
		payload = map[string]any{}
	}

	savePath := strings.TrimSpace(processingshared.ToString(payload["savePath"], processingshared.ToString(payload["save_path"], strings.TrimSpace(fallbackSavePath))))
	downloaderID := strings.TrimSpace(processingshared.ToString(payload["downloaderId"], processingshared.ToString(payload["downloader_id"], strings.TrimSpace(fallbackDownloaderID))))

	useDefaultDownloader := processingpersist.BoolFromAny(payload["useDefaultDownloader"]) || processingpersist.BoolFromAny(payload["use_default_downloader"])
	if useDefaultDownloader {
		if trimmedDefaultID := strings.TrimSpace(defaultDownloaderID); trimmedDefaultID != "" {
			downloaderID = trimmedDefaultID
		}
	}

	return savePath, downloaderID
}

func resolveDefaultDownloaderID(rootConfig map[string]any) string {
	if len(rootConfig) == 0 {
		return ""
	}

	crossSeed, ok := rootConfig["cross_seed"].(map[string]any)
	if !ok || crossSeed == nil {
		return ""
	}

	return strings.TrimSpace(processingshared.ToString(crossSeed["default_downloader"], ""))
}
