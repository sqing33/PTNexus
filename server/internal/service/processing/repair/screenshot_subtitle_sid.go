package repair

import (
	"encoding/json"
	"os/exec"
	"sort"
	"strings"

	"github.com/pt-nexus/server/internal/platform/logx"
)

type ffprobeSubtitleStreams struct {
	Streams []struct {
		Tags map[string]string `json:"tags"`
	} `json:"streams"`
}

type subtitleCandidate struct {
	SID   int
	Score int
	Title string
	Lang  string
}

// getBestChineseSubtitleSID 复刻 Python 版 _get_best_chinese_subtitle_sid：返回 mpv 的 sid（字幕序号，从 1 开始）。
func getBestChineseSubtitleSID(ffprobePath string, videoPath string) int {
	if strings.TrimSpace(ffprobePath) == "" {
		logx.PlainInfof("   ⚠️ 未找到 ffprobe，无法分析字幕流。")
		return 0
	}

	cmd := exec.Command(
		ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-select_streams", "s",
		videoPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logx.PlainInfof("   ⚠️ 字幕分析失败: %s", strings.TrimSpace(string(out)))
		return 0
	}

	var data ffprobeSubtitleStreams
	if jsonErr := json.Unmarshal(out, &data); jsonErr != nil {
		logx.PlainInfof("   ⚠️ 字幕分析失败: %v", jsonErr)
		return 0
	}
	if len(data.Streams) == 0 {
		return 0
	}

	candidates := make([]subtitleCandidate, 0)
	for i, s := range data.Streams {
		sid := i + 1
		tags := s.Tags
		lang := strings.ToLower(strings.TrimSpace(tags["language"]))
		if lang == "" {
			lang = "und"
		}
		title := strings.ToLower(strings.TrimSpace(tags["title"]))
		score := 0

		if lang == "chi" || lang == "zho" || lang == "zh" {
			score += 10
		}
		if strings.Contains(title, "简") || strings.Contains(title, "chs") || strings.Contains(title, "sc") {
			score += 5
		} else if strings.Contains(title, "繁") || strings.Contains(title, "cht") || strings.Contains(title, "tc") {
			score += 3
		} else if strings.Contains(title, "中") || strings.Contains(title, "chinese") {
			score += 2
		}
		if strings.Contains(title, "双语") {
			score += 1
		}

		if score > 0 {
			candidates = append(candidates, subtitleCandidate{SID: sid, Score: score, Title: title, Lang: lang})
		}
	}
	if len(candidates) == 0 {
		return 0
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].SID < candidates[j].SID
	})

	best := candidates[0]
	logx.PlainInfof("   🎯 自动选中字幕: Track %d [%s] %s", best.SID, best.Lang, best.Title)
	return best.SID
}
