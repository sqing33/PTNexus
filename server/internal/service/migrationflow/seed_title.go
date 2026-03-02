package migrationflow

import (
	"errors"
	"strings"

	processingpersist "github.com/pt-nexus/server/internal/service/processing/persist"
	processingshared "github.com/pt-nexus/server/internal/service/processing/shared"
	"gorm.io/gorm"
)

// QuerySeedTitle 从数据库中查询并返回种子标题。
// 参数/返回：torrentID/siteName 用于定位 seed_parameters；返回标题文本（找不到则返回空串）与错误。
// 失败场景：数据库异常返回错误；记录不存在不视为错误，返回空串。
// 副作用：读取数据库。
func (s *MigrateService) QuerySeedTitle(torrentID, siteName string) (string, error) {
	normalized, _, err := processingpersist.QueryAndNormalizeSeed(s.repo, torrentID, siteName)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	title := strings.TrimSpace(processingshared.ToString(normalized["title"], ""))
	if title == "" {
		title = strings.TrimSpace(processingshared.ToString(normalized["name"], ""))
	}
	return title, nil
}
