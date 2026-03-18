package repair

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

type subtitleSelectionStream struct {
	SubtitleSID             int
	StreamIndex             int
	StreamOrdinal           int
	CodecName               string
	Language                string
	Title                   string
	DisplayName             string
	ConfidenceScore         int
	IsConfidentChinese      bool
	IsDefault               bool
	IsNormal                bool
	SupportsEventExtraction bool
}

// resolveScreenshotSubtitleSelection 统一本地截图链路的字幕流探测与选择逻辑。
func resolveScreenshotSubtitleSelection(
	ffprobePath string,
	videoPath string,
	requestedSID int,
	hasRequested bool,
) (ScreenshotSubtitleInspection, *subtitleSelectionStream, error) {
	inspection, candidates, err := inspectLocalScreenshotSubtitleStreams(ffprobePath, videoPath)
	if err != nil {
		return ScreenshotSubtitleInspection{}, nil, err
	}

	if hasRequested {
		inspection.CurrentSubtitleSID = requestedSID
		if requestedSID <= 0 {
			return inspection, nil, nil
		}
		candidate, ok := findSubtitleSelectionBySID(candidates, requestedSID)
		if !ok {
			return inspection, nil, fmt.Errorf("所选字幕流不存在: sid=%d", requestedSID)
		}
		return inspection, &candidate, nil
	}

	if inspection.CurrentSubtitleSID <= 0 {
		return inspection, nil, nil
	}

	candidate, ok := findSubtitleSelectionBySID(candidates, inspection.CurrentSubtitleSID)
	if !ok {
		return inspection, nil, nil
	}
	return inspection, &candidate, nil
}

func inspectLocalScreenshotSubtitleStreams(ffprobePath, videoPath string) (ScreenshotSubtitleInspection, []subtitleSelectionStream, error) {
	if strings.TrimSpace(ffprobePath) == "" {
		return ScreenshotSubtitleInspection{}, nil, fmt.Errorf("ffprobe 不能为空")
	}

	cmd := exec.Command(
		ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_entries", "stream=index,codec_name,disposition:stream_tags=language,title",
		"-select_streams", "s",
		videoPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = err.Error()
		}
		return ScreenshotSubtitleInspection{}, nil, fmt.Errorf("探测字幕流失败: %s", text)
	}

	var probeResult subtitleStreamProbe
	if err := json.Unmarshal(out, &probeResult); err != nil {
		return ScreenshotSubtitleInspection{}, nil, fmt.Errorf("解析字幕 JSON 失败: %w", err)
	}

	candidates := make([]subtitleSelectionStream, 0, len(probeResult.Streams))
	for i, stream := range probeResult.Streams {
		language := normalizeSubtitleLanguage(stream.Tags["language"])
		title := strings.TrimSpace(stream.Tags["title"])
		score := subtitleChineseScore(language, title)
		candidate := subtitleSelectionStream{
			SubtitleSID:        i + 1,
			StreamIndex:        stream.Index,
			StreamOrdinal:      i,
			CodecName:          strings.ToLower(strings.TrimSpace(stream.CodecName)),
			Language:           language,
			Title:              title,
			ConfidenceScore:    score,
			IsConfidentChinese: score > 0,
			IsNormal: !(toBoolAny(stream.Disposition["comment"]) ||
				toBoolAny(stream.Disposition["hearing_impaired"]) ||
				toBoolAny(stream.Disposition["visual_impaired"])),
		}
		candidate.SupportsEventExtraction = isSupportedSubtitleCodec(candidate.CodecName)
		candidate.DisplayName = buildSubtitleDisplayName(candidate)
		candidates = append(candidates, candidate)
	}

	if defaultCandidate, ok := selectDefaultSubtitleSelection(candidates); ok {
		for i := range candidates {
			candidates[i].IsDefault = candidates[i].SubtitleSID == defaultCandidate.SubtitleSID
			candidates[i].DisplayName = buildSubtitleDisplayName(candidates[i])
		}
	}

	streams := make([]ScreenshotSubtitleStream, 0, len(candidates))
	for _, candidate := range candidates {
		streams = append(streams, ScreenshotSubtitleStream{
			SubtitleSID:        candidate.SubtitleSID,
			StreamIndex:        candidate.StreamIndex,
			CodecName:          candidate.CodecName,
			Language:           candidate.Language,
			Title:              candidate.Title,
			DisplayName:        candidate.DisplayName,
			IsConfidentChinese: candidate.IsConfidentChinese,
			IsDefault:          candidate.IsDefault,
		})
	}

	inspection := ScreenshotSubtitleInspection{
		SubtitleState:   ScreenshotSubtitleStateNoUsableSubtitle,
		SubtitleStreams: streams,
	}
	if bestChinese, ok := selectBestChineseSubtitleSelection(candidates); ok {
		inspection.SubtitleState = ScreenshotSubtitleStateConfirmedChinese
		inspection.CurrentSubtitleSID = bestChinese.SubtitleSID
		return inspection, candidates, nil
	}
	if defaultCandidate, ok := selectDefaultSubtitleSelection(candidates); ok {
		inspection.SubtitleState = ScreenshotSubtitleStateUsableButUnconfirmed
		inspection.CurrentSubtitleSID = defaultCandidate.SubtitleSID
	}
	return inspection, candidates, nil
}

func findSubtitleSelectionBySID(candidates []subtitleSelectionStream, sid int) (subtitleSelectionStream, bool) {
	for _, candidate := range candidates {
		if candidate.SubtitleSID == sid {
			return candidate, true
		}
	}
	return subtitleSelectionStream{}, false
}

func selectBestChineseSubtitleSelection(candidates []subtitleSelectionStream) (subtitleSelectionStream, bool) {
	ranked := make([]subtitleSelectionStream, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.ConfidenceScore <= 0 {
			continue
		}
		ranked = append(ranked, candidate)
	}
	if len(ranked) == 0 {
		return subtitleSelectionStream{}, false
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].ConfidenceScore != ranked[j].ConfidenceScore {
			return ranked[i].ConfidenceScore > ranked[j].ConfidenceScore
		}
		if ranked[i].IsNormal != ranked[j].IsNormal {
			return ranked[i].IsNormal
		}
		if subtitleCodecPriority(ranked[i].CodecName) != subtitleCodecPriority(ranked[j].CodecName) {
			return subtitleCodecPriority(ranked[i].CodecName) < subtitleCodecPriority(ranked[j].CodecName)
		}
		return ranked[i].SubtitleSID < ranked[j].SubtitleSID
	})
	return ranked[0], true
}

func selectDefaultSubtitleSelection(candidates []subtitleSelectionStream) (subtitleSelectionStream, bool) {
	if len(candidates) == 0 {
		return subtitleSelectionStream{}, false
	}

	tryPick := func(match func(subtitleSelectionStream) bool) (subtitleSelectionStream, bool) {
		best := subtitleSelectionStream{}
		found := false
		for _, candidate := range candidates {
			if !match(candidate) {
				continue
			}
			if !found {
				best = candidate
				found = true
				continue
			}
			if subtitleCodecPriority(candidate.CodecName) < subtitleCodecPriority(best.CodecName) {
				best = candidate
				continue
			}
			if subtitleCodecPriority(candidate.CodecName) == subtitleCodecPriority(best.CodecName) &&
				candidate.SubtitleSID < best.SubtitleSID {
				best = candidate
			}
		}
		return best, found
	}

	if candidate, ok := tryPick(func(item subtitleSelectionStream) bool {
		return item.IsNormal && item.SupportsEventExtraction
	}); ok {
		return candidate, true
	}
	if candidate, ok := tryPick(func(item subtitleSelectionStream) bool {
		return item.SupportsEventExtraction
	}); ok {
		return candidate, true
	}
	if candidate, ok := tryPick(func(item subtitleSelectionStream) bool {
		return item.IsNormal
	}); ok {
		return candidate, true
	}

	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.SubtitleSID < best.SubtitleSID {
			best = candidate
		}
	}
	return best, true
}

func subtitleChineseScore(language string, title string) int {
	score := 0
	lang := strings.ToLower(strings.TrimSpace(language))
	titleText := strings.ToLower(strings.TrimSpace(title))

	switch {
	case lang == "chi", lang == "zho", lang == "zh", lang == "cmn":
		score += 10
	case strings.HasPrefix(lang, "zh-"), strings.HasPrefix(lang, "zh_"):
		score += 10
	}

	switch {
	case strings.Contains(titleText, "简体"),
		strings.Contains(titleText, "简中"),
		strings.Contains(titleText, "chs"),
		strings.Contains(titleText, "sc"),
		strings.Contains(titleText, "simplified"):
		score += 5
	case strings.Contains(titleText, "繁体"),
		strings.Contains(titleText, "繁中"),
		strings.Contains(titleText, "cht"),
		strings.Contains(titleText, "tc"),
		strings.Contains(titleText, "traditional"):
		score += 3
	case strings.Contains(titleText, "中文"),
		strings.Contains(titleText, "中字"),
		strings.Contains(titleText, "chinese"):
		score += 2
	}

	if strings.Contains(titleText, "双语") || strings.Contains(titleText, "bilingual") {
		score++
	}
	return score
}

func normalizeSubtitleLanguage(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func subtitleCodecPriority(codec string) int {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass":
		return 0
	case "subrip":
		return 1
	case "hdmv_pgs_subtitle":
		return 2
	default:
		return 9
	}
}

func isSupportedSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass", "subrip", "hdmv_pgs_subtitle":
		return true
	default:
		return false
	}
}

func buildSubtitleDisplayName(candidate subtitleSelectionStream) string {
	parts := make([]string, 0, 5)
	parts = append(parts, fmt.Sprintf("字幕 %d", candidate.SubtitleSID))
	if candidate.IsDefault {
		parts = append(parts, "默认")
	}
	if candidate.IsConfidentChinese {
		parts = append(parts, "疑似中文")
	}
	if candidate.CodecName != "" {
		parts = append(parts, strings.ToUpper(candidate.CodecName))
	}
	if candidate.Language != "" {
		parts = append(parts, candidate.Language)
	}
	if candidate.Title != "" {
		parts = append(parts, candidate.Title)
	}
	return strings.Join(parts, " · ")
}
