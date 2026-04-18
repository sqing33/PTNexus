package persist

import "strings"

// SeedQueryRepo 定义查询并归一化种子参数所需的最小仓储接口。
type SeedQueryRepo interface {
	GetSeedParameter(torrentID, siteName string) (map[string]any, error)
	GetCurrentTorrentByHash(hash string) (map[string]any, error)
	GetCurrentTorrentByName(name string) (map[string]any, error)
}

// QueryAndNormalizeSeed 从 seed_parameters 查询种子并补全标准化参数、发布参数与上下文字段。
// 参数/返回：repo 为查询依赖；torrentID/siteName 为主键条件；返回归一化后的数据与 seed_id。
// 失败场景：数据库查询失败时返回 error（包含记录不存在）。
// 副作用：仅执行数据库读取，不写入任何状态。
func QueryAndNormalizeSeed(repo SeedQueryRepo, torrentID, siteName string) (map[string]any, string, error) {
	row, err := repo.GetSeedParameter(strings.TrimSpace(torrentID), strings.TrimSpace(siteName))
	if err != nil {
		return nil, "", err
	}

	normalized := NormalizeSeedRow(row)
	if strings.TrimSpace(toStringSimple(normalized["title"])) == "" {
		normalized["title"] = strings.TrimSpace(firstNonEmptyString(toStringSimple(normalized["name"]), strings.TrimSpace(torrentID)))
	}
	if strings.TrimSpace(toStringSimple(normalized["name"])) == "" {
		normalized["name"] = strings.TrimSpace(firstNonEmptyString(toStringSimple(normalized["title"]), strings.TrimSpace(torrentID)))
	}
	if strings.TrimSpace(toStringSimple(normalized["title"])) == "" {
		normalized["title"] = strings.TrimSpace(torrentID)
	}

	current := map[string]any{}
	if hash := strings.TrimSpace(toStringSimple(normalized["hash"])); hash != "" {
		if row, currentErr := repo.GetCurrentTorrentByHash(hash); currentErr == nil {
			current = row
		}
	}
	if len(current) == 0 {
		if name := strings.TrimSpace(toStringSimple(normalized["name"])); name != "" {
			if row, currentErr := repo.GetCurrentTorrentByName(name); currentErr == nil {
				current = row
			}
		}
	}
	if len(current) > 0 {
		normalized["save_path"] = toStringSimple(current["save_path"])
		normalized["downloader_id"] = toStringSimple(current["downloader_id"])
	}
	if normalized["save_path"] == nil {
		normalized["save_path"] = ""
	}
	if normalized["downloader_id"] == nil {
		normalized["downloader_id"] = ""
	}

	seedID := ComposeSeedID(
		strings.TrimSpace(toStringSimple(normalized["hash"])),
		strings.TrimSpace(torrentID),
		firstNonEmptyString(strings.TrimSpace(toStringSimple(normalized["site_name"])), strings.TrimSpace(siteName)),
	)
	normalized["seed_id"] = seedID
	normalized["standardized_params"] = BuildStandardizedParams(normalized)
	normalized["final_publish_parameters"] = BuildFinalPublishParameters(normalized)
	normalized["complete_publish_params"] = BuildCompletePublishParams(normalized)
	normalized["raw_params_for_preview"] = BuildRawPreviewParams(normalized)
	return normalized, seedID, nil
}

func firstNonEmptyString(items ...string) string {
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
