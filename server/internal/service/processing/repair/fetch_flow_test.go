package repair

import (
	"strings"
	"testing"

	parser "github.com/pt-nexus/server/internal/service/acquire/extract"
)

func TestRunIntroRepairTaskSkipsNovaHDRefetch(t *testing.T) {
	sourceIntro := "NovaHD 源站简介正文\n这里保留源站原始简介内容"
	var emitted []string

	result := runIntroRepairTask(
		ParallelFetchRepairInput{
			TaskID:      "auto-seed-1",
			SourceSite:  "NovaHD",
			TorrentName: "示例标题",
			ReviewData: parser.ReviewExtractedData{
				Body: sourceIntro,
			},
			IMDbLink:   "https://www.imdb.com/title/tt1234567/",
			DoubanLink: "https://movie.douban.com/subject/123456/",
		},
		FetchRepairDeps{
			EmitLog: func(taskID, step, message, status string) {
				emitted = append(emitted, step+" "+message+" "+status)
			},
		},
	)

	if result.Body != sourceIntro {
		t.Fatalf("expected NovaHD source intro to be kept, got=%q", result.Body)
	}
	if result.IMDbLink != "https://www.imdb.com/title/tt1234567/" {
		t.Fatalf("expected imdb link to be kept, got=%q", result.IMDbLink)
	}
	joinedLogs := strings.Join(emitted, "\n")
	if !strings.Contains(joinedLogs, "已跳过二次补全") {
		t.Fatalf("expected skip log to be emitted, got=%q", joinedLogs)
	}
}

func TestIsNovaHDSourceSite(t *testing.T) {
	cases := []string{"NovaHD", "nova hd", "nova-hd", "pt.NovaHD.top"}
	for _, item := range cases {
		if !isNovaHDSourceSite(item) {
			t.Fatalf("expected %q to be recognized as NovaHD", item)
		}
	}
	if isNovaHDSourceSite("HDHome") {
		t.Fatalf("expected non NovaHD source to be ignored")
	}
}
