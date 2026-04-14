package fetch

import (
	"errors"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
	"gorm.io/gorm"
)

const sourceSiteCheckLogModule = "迁移-源站校验"

// ResolveSourceSiteForFetch 校验源站配置并返回可抓取所需的站点信息。
// 参数/返回：reader 用于读取站点配置；sourceSite 为源站昵称；返回站点配置、建议状态码和错误。
// 失败场景：站点不存在、缺少 cookie/passkey 或数据库读取失败。
// 副作用：读取数据库并打印源站校验日志。
func ResolveSourceSiteForFetch(reader SiteInfoReader, sourceSite string) (map[string]any, int, error) {
	logx.Infof(sourceSiteCheckLogModule, "开始校验源站点 source_site=%s", sourceSite)
	sourceInfo, err := reader.GetSiteByName(sourceSite)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logx.Warnf(sourceSiteCheckLogModule, "源站点不存在 source_site=%s", sourceSite)
			return nil, 404, errors.New("源站点 '" + sourceSite + "' 不存在。")
		}
		logx.Errorf(sourceSiteCheckLogModule, "读取站点配置失败 source_site=%s err=%v", sourceSite, err)
		return nil, 500, errors.New("读取站点配置失败: " + err.Error())
	}

	cookie := strings.TrimSpace(toStringAny(sourceInfo["cookie"], ""))
	passkey := strings.TrimSpace(toStringAny(sourceInfo["passkey"], ""))
	if cookie == "" && passkey == "" {
		logx.Warnf(sourceSiteCheckLogModule, "源站点配置不完整 source_site=%s 缺少cookie/passkey", sourceSite)
		return nil, 404, errors.New("源站点 '" + sourceSite + "' 配置不完整。")
	}

	logx.Infof(
		sourceSiteCheckLogModule,
		"源站点校验通过 source_site=%s site_code=%s migration=%d",
		sourceSite,
		strings.TrimSpace(toStringAny(sourceInfo["site"], "")),
		int(toFloatAny(sourceInfo["migration"])),
	)
	return sourceInfo, 200, nil
}
