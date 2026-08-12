package extract

import (
	"strings"
	"testing"

	sites "github.com/pt-nexus/server/internal/service/acquire/extract/sites"
)

func TestExtractTopTitleCleansNexusPHPPageTitle(t *testing.T) {
	pageHTML := `<html><head><title>Depth Studio :: 种子详情 "Demon Slayer Kimetsu no Yaiba The Movie Infinity Castle 2025 JPN 1080p Blu-ray AVC TrueHD 5.1-DStudio" - Powered by NexusPHP</title></head><body></body></html>`
	want := "Demon Slayer Kimetsu no Yaiba The Movie Infinity Castle 2025 JPN 1080p Blu-ray AVC TrueHD 5.1-DStudio"

	if got := extractTopTitle(pageHTML); got != want {
		t.Fatalf("expected NexusPHP wrapped title to be cleaned, got=%q want=%q", got, want)
	}
}

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
func TestExtractDStudioSpecialIntroNormalization(t *testing.T) {
	rawIntro := "\u2741 Title:\u3000\u6211\u7684\u8352\u7cd6\u604b\u7231\n" +
		"\u2741 Original Title:\u3000\uc774\ub7f0 \uc5ff \uac19\uc740 \uc0ac\ub791\n" +
		"\u2741 Genres:\u3000\u5267\u60c5 / \u559c\u5267\n" +
		"\u2741 Languages:\u3000\ud55c\uad6d\uc5b4/\uc870\uc120\ub9d0\n" +
		"\u2741 First Air Date:\u30002026-08-07\n" +
		"\u2741 Number of Episodes:\u300012\n" +
		"\u2741 Number of Seasons:\u30001\n" +
		"\u2741 Episode Runtime:\u3000N/A\n" +
		"\u2741 Production Countries:\u3000South Korea\n" +
		"\u2741 Rating:\u30006 / 10 from 1 users\n" +
		"\u2741 TMDB Link:\u3000https://www.themoviedb.org/tv/291496/\n" +
		"\u2741 IMDb Link:\u3000https://www.imdb.com/title/tt36955608/\n" +
		"\u2741 Directors:\u3000\uf90a\u5c06\u6c49\n\n" +
		"\u2741 Cast\n" +
		"\u4e01\u6d77\u5bc5 as Jang Tae-ha\n" +
		"\u8d3a\u8425 as Go Eun-sae\n" +
		"\u8bb8\u6210\u6cf0 as Baek Sang-gil"

	public := NewPublicExtractor(func(input Input) (SeedData, error) {
		return SeedData{
			Intro: IntroData{
				Statement: rawIntro,
			},
		}, nil
	})
	toSiteSeedData := func(data SeedData) sites.SeedData {
		return sites.SeedData{
			Title:        data.Title,
			Subtitle:     data.Subtitle,
			Intro:        sites.IntroData{Statement: data.Intro.Statement, Poster: data.Intro.Poster, Body: data.Intro.Body, Screenshots: data.Intro.Screenshots, RemovedARDTUDeclarations: append([]string{}, data.Intro.RemovedARDTUDeclarations...)},
			MediaInfo:    data.MediaInfo,
			SourceParams: cloneAnyMap(data.SourceParams),
			Type:         data.Type,
			Medium:       data.Medium,
			VideoCodec:   data.VideoCodec,
			AudioCodec:   data.AudioCodec,
			Resolution:   data.Resolution,
			Team:         data.Team,
			Source:       data.Source,
			Tags:         append([]string{}, data.Tags...),
			IMDbLink:     data.IMDbLink,
			DoubanLink:   data.DoubanLink,
			TMDbLink:     data.TMDbLink,
		}
	}
	engine := NewDefaultEngine(public, func(public Extractor) sites.Runtime {
		return sites.Runtime{
			ExtractWithPublic: func(input sites.Input) (sites.SeedData, error) {
				data, err := public.Extract(Input{
					SiteCode:      input.SiteCode,
					SiteNickname:  input.SiteNickname,
					BaseURL:       input.BaseURL,
					Cookie:        input.Cookie,
					TorrentID:     input.TorrentID,
					PageHTML:      input.PageHTML,
					FallbackTitle: input.FallbackTitle,
				})
				return toSiteSeedData(data), err
			},
			BuildSourceParams: func(data sites.SeedData) map[string]any {
				return BuildSourceParamsFromExtractedData(fromSiteData(data))
			},
		}
	})

	data, meta := engine.Extract(Input{
		SiteCode:      "DS",
		SiteNickname:  "\u5c0c\u4e1d",
		FallbackTitle: "\u6211\u7684\u8352\u7cd6\u604b\u7231",
	})

	if meta.ExtractorName != "dstudio_special" {
		t.Fatalf("expected DStudio special extractor, got=%q meta=%+v", meta.ExtractorName, meta)
	}

	body := data.Intro.Body
	for _, want := range []string{
		"\u25ce\u7247\u3000\u3000\u540d\u3000\u6211\u7684\u8352\u7cd6\u604b\u7231",
		"\u25ce\u539f\u3000\u3000\u540d\u3000\uc774\ub7f0 \uc5ff \uac19\uc740 \uc0ac\ub791",
		"\u25ce\u7c7b\u3000\u3000\u522b\u3000\u5267\u60c5 / \u559c\u5267",
		"\u25ce\u8bed\u3000\u3000\u8a00\u3000\ud55c\uad6d\uc5b4/\uc870\uc120\ub9d0",
		"\u25ce\u9996\u3000\u3000\u64ad\u30002026-08-07",
		"\u25ce\u96c6\u3000\u3000\u6570\u300012",
		"\u25ce\u5b63\u3000\u3000\u6570\u30001",
		"\u25ce\u5355\u96c6\u7247\u957f\u3000\u6682\u65e0",
		"\u25ce\u4ea7\u3000\u3000\u5730\u3000\u97e9\u56fd",
		"\u25ce\u8bc4\u3000\u3000\u5206\u30006 / 10 from 1 users",
		"\u25ce\u5bfc\u3000\u3000\u6f14\u3000\uf90a\u5c06\u6c49",
		"\u25ce\u4e3b\u3000\u3000\u6f14",
		"\u4e01\u6d77\u5bc5 \u9970 Jang Tae-ha",
		"\u25ce\u7b80\u3000\u3000\u4ecb\u3000\u6682\u65e0\u7b80\u4ecb",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got=%q", want, body)
		}
	}
	if strings.Contains(body, "Title:") || strings.Contains(body, "Cast") {
		t.Fatalf("expected english field labels to be rewritten, got=%q", body)
	}
}

func TestExtractDStudioKeepsStatementSeparateFromIntro(t *testing.T) {
	rawStatement := "[quote]DStudio declaration[/quote]"
	rawIntro := "\u2741 Title:\u3000Test Title\n" +
		"\u2741 Original Title:\u3000Original Test\n" +
		"\u2741 Production Countries:\u3000South Korea\n"

	public := NewPublicExtractor(func(input Input) (SeedData, error) {
		return SeedData{
			Intro: IntroData{
				Statement: rawStatement,
				Body:      rawIntro,
			},
		}, nil
	})
	toSiteSeedData := func(data SeedData) sites.SeedData {
		return sites.SeedData{
			Title:        data.Title,
			Subtitle:     data.Subtitle,
			Intro:        sites.IntroData{Statement: data.Intro.Statement, Poster: data.Intro.Poster, Body: data.Intro.Body, Screenshots: data.Intro.Screenshots, RemovedARDTUDeclarations: append([]string{}, data.Intro.RemovedARDTUDeclarations...)},
			MediaInfo:    data.MediaInfo,
			SourceParams: cloneAnyMap(data.SourceParams),
			Type:         data.Type,
			Medium:       data.Medium,
			VideoCodec:   data.VideoCodec,
			AudioCodec:   data.AudioCodec,
			Resolution:   data.Resolution,
			Team:         data.Team,
			Source:       data.Source,
			Tags:         append([]string{}, data.Tags...),
			IMDbLink:     data.IMDbLink,
			DoubanLink:   data.DoubanLink,
			TMDbLink:     data.TMDbLink,
		}
	}
	engine := NewDefaultEngine(public, func(public Extractor) sites.Runtime {
		return sites.Runtime{
			ExtractWithPublic: func(input sites.Input) (sites.SeedData, error) {
				data, err := public.Extract(Input{
					SiteCode:      input.SiteCode,
					SiteNickname:  input.SiteNickname,
					BaseURL:       input.BaseURL,
					Cookie:        input.Cookie,
					TorrentID:     input.TorrentID,
					PageHTML:      input.PageHTML,
					FallbackTitle: input.FallbackTitle,
				})
				return toSiteSeedData(data), err
			},
			BuildSourceParams: func(data sites.SeedData) map[string]any {
				return BuildSourceParamsFromExtractedData(fromSiteData(data))
			},
		}
	})

	data, _ := engine.Extract(Input{
		SiteCode:      "DS",
		SiteNickname:  "\u5c0c\u4e1d",
		FallbackTitle: "Test Title",
	})

	if data.Intro.Statement != rawStatement {
		t.Fatalf("expected statement to stay separate, got=%q", data.Intro.Statement)
	}
	if strings.Contains(data.Intro.Body, "DStudio declaration") {
		t.Fatalf("expected declaration to be excluded from intro body, got=%q", data.Intro.Body)
	}
	for _, want := range []string{
		"\u25ce\u7247\u3000\u3000\u540d\u3000Test Title",
		"\u25ce\u539f\u3000\u3000\u540d\u3000Original Test",
		"\u25ce\u4ea7\u3000\u3000\u5730\u3000\u97e9\u56fd",
	} {
		if !strings.Contains(data.Intro.Body, want) {
			t.Fatalf("expected intro body to contain %q, got=%q", want, data.Intro.Body)
		}
	}
}

func TestExtractHDVideoSpecialExtractor(t *testing.T) {
	pageHTML := `<html>
<head><title>HDvideo :: Torrent Details "Test.Movie.2026.2160p.WEB-DL.H265.AAC-HDVWEB" - Powered by NexusPHP</title></head>
<body>
<h1 id="top">Test.Movie.2026.2160p.WEB-DL.H265.AAC-HDVWEB</h1>
<table>
<tr><td>` + "\u98ce\u683c" + `</td><td>` + "\u559c\u5267 / \u52a8\u4f5c" + `</td></tr>
</table>
<div id="kdescr">
[quote]` + "\u25ce\u7247\u3000\u3000\u540d\u3000\u6d4b\u8bd5\u7535\u5f71" + `[/quote]
<img src="https://img.example.test/poster.jpg">
https://movie.douban.com/subject/1234567/
https://www.imdb.com/title/tt1234567/
https://www.themoviedb.org/movie/7654321/
</div>
<div class="codemain"><pre>General
Unique ID                                : 123
Complete name                            : Test.Movie.2026.mkv
Overall bit rate                         : 23.0 Mb/s

Video
Format                                   : HEVC
Width                                    : 3 840 pixels
Height                                   : 2 160 pixels

Audio
Format                                   : AAC
</pre></div>
<div id="kscreenshots">
<img src="https://img.example.test/screen1.jpg">
<img src="https://img.example.test/screen2.png">
</div>
</body></html>`

	data, meta := NewPageExtractorEngine().Extract(Input{
		SiteCode:      "hdvideo",
		SiteNickname:  "HDvideo",
		PageHTML:      pageHTML,
		FallbackTitle: "fallback",
	})

	if meta.ExtractorName != "hdvideo_special" {
		t.Fatalf("expected HDvideo special extractor, got=%q meta=%+v", meta.ExtractorName, meta)
	}
	if !strings.Contains(data.MediaInfo, "Complete name") || strings.Contains(strings.ToLower(data.MediaInfo), "[code]") {
		t.Fatalf("expected raw mediainfo without code wrapper, got=%q", data.MediaInfo)
	}
	for _, want := range []string{"[img]https://img.example.test/screen1.jpg[/img]", "[img]https://img.example.test/screen2.png[/img]"} {
		if !strings.Contains(data.Intro.Screenshots, want) {
			t.Fatalf("expected screenshots to contain %q, got=%q", want, data.Intro.Screenshots)
		}
	}
	if data.DoubanLink != "https://movie.douban.com/subject/1234567" {
		t.Fatalf("expected douban link normalized, got=%q", data.DoubanLink)
	}
	if data.IMDbLink != "https://www.imdb.com/title/tt1234567" {
		t.Fatalf("expected imdb link normalized, got=%q", data.IMDbLink)
	}
	if data.TMDbLink != "https://www.themoviedb.org/movie/7654321" {
		t.Fatalf("expected tmdb link normalized, got=%q", data.TMDbLink)
	}
	for _, want := range []string{"tag.\u559c\u5267", "tag.\u52a8\u4f5c"} {
		if !containsString(data.Tags, want) {
			t.Fatalf("expected tags to contain %q, got=%v", want, data.Tags)
		}
	}
	if data.SourceParams == nil || data.SourceParams["\u6807\u7b7e"] == nil {
		t.Fatalf("expected source params to be rebuilt with tags, got=%v", data.SourceParams)
	}
}
