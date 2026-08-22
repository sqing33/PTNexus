package engine

import (
	"strings"

	"github.com/pt-nexus/server/internal/service/publish/publisher"
	publishsites "github.com/pt-nexus/server/internal/service/publish/publisher/sites"
)

// Publish 按站点 code 路由到公共发布器或对应站点的特殊发布器。
// 参数/返回：input 由 workflow 统一构建；返回 PublishResult 与 error。
// 失败场景：发布器内部参数缺失、读取 torrent 失败、站点返回错误等返回 error。
// 副作用：读取本地 torrent 文件并向目标站点发起请求（由具体发布器实现）。
func Publish(input publisher.PublishInput) (publisher.PublishResult, error) {
	siteCode := strings.ToLower(strings.TrimSpace(input.SiteCode))

	switch siteCode {
	case "cbg":
		return publishsites.PublishCBG(input)
	case "baozi":
		return publishsites.PublishBaozi(input)
	case "audiences":
		return publishsites.PublishAudiences(input)
	case "hddolby":
		return publishsites.PublishHDDolby(input)
	case "hdkyl":
		return publishsites.PublishHDKyl(input)
	case "luckpt":
		return publishsites.PublishLuckPT(input)
	case "pterclub":
		return publishsites.PublishPTerClub(input)
	case "ptskit":
		return publishsites.PublishPTSKit(input)
	case "zhuque":
		return publishsites.PublishZhuque(input)
	case "haidan":
		return publishsites.PublishHaidan(input)
	case "rousi":
		return publishsites.PublishRousi(input)
	case "ptlgs":
		return publishsites.PublishPTLGS(input)
	case "hdfans":
		return publishsites.PublishHdfans(input)
	case "hdvideo":
		return publishsites.PublishHDVideo(input)
	case "hdhome":
		return publishsites.PublishHDHome(input)
	case "ourbits":
		return publishsites.PublishOurBits(input)
	case "crabpt":
		return publishsites.PublishCrabPT(input)
	case "ttg":
		return publishsites.PublishTTG(input)
	case "longpt":
		return publishsites.PublishLongPT(input)
	case "xdypt":
		return publishsites.PublishXDYPT(input)
	default:
		return publisher.PublishPublic(input)
	}
}
