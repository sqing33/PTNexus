package extract

import (
	"strings"
	"testing"
)

func TestExtractDescriptionSectionsStripsNHDWEBDeclaration(t *testing.T) {
	descrBBCode := `[quote][b]NovaHD · 资源声明[/b]
- 本站提供的所有资源，不得下载用于商业盈利，否则产生的一切后果由您自行承担！
- 本站用户发布的资源链接等任何内容，均由用户自发提供，本站不负任何法律责任。
- 本站列出的资源本身并没有保存在本站的服务器上，本站仅负责连接资源，对被传播的资源的内容无法自动识别，管理员无法对用户的提交内容或其他行为负责。
- 所有内容仅作宽带测试使用，请在下载后24小时内删除。若喜欢请联系正版厂商，购买正版。
- 本站资源如侵犯了您的合法权益，请点击 [url=https://pt.NovaHD.top/contactstaff.php]联系管理员[/url]，提供相关证明，将立即删除相关资源。[/quote]

[quote]片名：示例片名
产地：中国大陆
简介：示例简介[/quote]`

	statement, _, body, _, _, _, removed := extractDescriptionSections("", descrBBCode, "")

	if strings.Contains(statement, "资源声明") || strings.Contains(body, "资源声明") {
		t.Fatalf("unexpected declaration text kept in output: statement=%q body=%q", statement, body)
	}
	if !strings.Contains(statement, "片名：示例片名") {
		t.Fatalf("expected intro statement to remain, got statement=%q body=%q", statement, body)
	}

	foundRemoved := false
	for _, item := range removed {
		if strings.Contains(item, "NovaHD · 资源声明") || strings.Contains(item, "本站提供的所有资源") {
			foundRemoved = true
			break
		}
	}
	if !foundRemoved {
		t.Fatalf("expected NovaHD declaration to be reported as removed, got=%v", removed)
	}
}

func TestExtractDescriptionSectionsStripsPlainNHDWEBDeclaration(t *testing.T) {
	descrBBCode := `[b]NovaHD · 资源声明[/b]
- 本站提供的所有资源，不得下载用于商业盈利，否则产生的一切后果由您自行承担！
- 本站用户发布的资源链接等任何内容，均由用户自发提供，本站不负任何法律责任。
- 本站列出的资源本身并没有保存在本站的服务器上，本站仅负责连接资源，对被传播的资源的内容无法自动识别，管理员无法对用户的提交内容或其他行为负责。
- 所有内容仅作宽带测试使用，请在下载后24小时内删除。若喜欢请联系正版厂商，购买正版。
- 本站资源如侵犯了您的合法权益，请点击
          [url=https://pt.NovaHD.top/contactstaff.php]联系管理员[/url]，
          提供相关证明，将立即删除相关资源。

◎片　　名　示例片名
◎产　　地　中国大陆
◎简　　介　示例简介`

	_, _, body, _, _, _, _ := extractDescriptionSections("", descrBBCode, "")

	for _, unwanted := range []string{"NovaHD · 资源声明", "本站提供的所有资源", "contactstaff", "提供相关证明"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("unexpected declaration text %q kept in body=%q", unwanted, body)
		}
	}
	if !strings.Contains(body, "◎片　　名　示例片名") || !strings.Contains(body, "◎简　　介　示例简介") {
		t.Fatalf("expected intro body to remain, got body=%q", body)
	}
}
