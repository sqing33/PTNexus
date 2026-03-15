package title

import (
	"strings"
	"testing"
)

func TestBuildSimpleTitleComponentsStandardizesHDRWithoutMediainfo(t *testing.T) {
	components := BuildSimpleTitleComponents("Scarpetta S01E01 2026 2160p AMZN WEB-DL DDP5.1 H265 HDR DV-Pure@HDSWEB", "")

	if got := findTitleComponentValueForTest(components, "HDR格式"); got != "DoVi HDR" {
		t.Fatalf("expected HDR格式 to be DoVi HDR, got %q", got)
	}

	unrecognized := findTitleComponentValueForTest(components, "无法识别")
	if strings.Contains(strings.ToUpper(unrecognized), "HDR") || strings.Contains(strings.ToUpper(unrecognized), "DV") {
		t.Fatalf("expected 无法识别 to exclude HDR aliases, got %q", unrecognized)
	}
}

func TestExtractHDRFormatFromTitleStandardizesTokenOrder(t *testing.T) {
	cases := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "HDRThenDV",
			title: "Scarpetta 2026 2160p WEB-DL HDR DV",
			want:  "DoVi HDR",
		},
		{
			name:  "DVThenHDR",
			title: "Scarpetta 2026 2160p WEB-DL DV HDR",
			want:  "DoVi HDR",
		},
		{
			name:  "DolbyVisionHDR10Plus",
			title: "Scarpetta 2026 2160p WEB-DL Dolby Vision HDR10+",
			want:  "DoVi HDR10+",
		},
		{
			name:  "DoViOnly",
			title: "Scarpetta 2026 2160p WEB-DL DV",
			want:  "DoVi",
		},
		{
			name:  "HDRVivid",
			title: "Scarpetta 2026 2160p WEB-DL HDRVivid",
			want:  "HDR Vivid",
		},
		{
			name:  "HDR10",
			title: "Scarpetta 2026 2160p WEB-DL HDR10",
			want:  "HDR",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractHDRFormatFromTitle(tc.title); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func findTitleComponentValueForTest(components []map[string]any, key string) string {
	for _, component := range components {
		if strings.TrimSpace(toStringSimple(component["key"], "")) != key {
			continue
		}
		return strings.TrimSpace(toStringSimple(component["value"], ""))
	}
	return ""
}
