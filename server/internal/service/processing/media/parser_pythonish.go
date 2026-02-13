package media

import (
	"regexp"
	"strconv"
	"strings"
)

type HDRInfo struct {
	StandardTag string
}

type AudioTrack struct {
	Codec      string
	Channels   string
	HasAtmos   bool
	AudioCount string
}

type AudioInfo struct {
	Codec     string
	Channels  string
	HasAtmos  bool
	AllTracks []AudioTrack
}

func ExtractHDRInfoFromMediaText(text string, isBDInfo bool) HDRInfo {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return HDRInfo{}
	}
	if isBDInfo {
		return HDRInfo{StandardTag: determineBDInfoHDRStandardTag(extractBDInfoVideoLines(trimmed))}
	}
	params := extractMediaInfoVideoParams(trimmed)
	return HDRInfo{StandardTag: determineMediaInfoHDRStandardTag(params)}
}

type mediaInfoVideoParams struct {
	HDRFormat string
	Transfer  string
	Primaries string
}

func extractMediaInfoVideoParams(text string) mediaInfoVideoParams {
	lines := strings.Split(text, "\n")
	currentSection := "General"
	params := mediaInfoVideoParams{}

	reSectionVideo := regexp.MustCompile(`(?i)^Video(\s*#\d+)?$`)
	reSectionOther := regexp.MustCompile(`(?i)^(Audio|Text|Menu|Chapters|General)(\s*#\d+)?$`)

	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}

		if strings.HasPrefix(stripped, "Video") && reSectionVideo.MatchString(stripped) {
			currentSection = "Video"
			continue
		}
		if reSectionOther.MatchString(stripped) {
			currentSection = strings.Fields(stripped)[0]
			continue
		}

		if currentSection != "Video" {
			continue
		}
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		rawKey := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		switch rawKey {
		case "hdr format":
			params.HDRFormat = val
		case "color primaries":
			params.Primaries = val
		case "transfer characteristics":
			params.Transfer = val
		}
	}

	return params
}

func determineMediaInfoHDRStandardTag(params mediaInfoVideoParams) string {
	hdrFormat := strings.ToUpper(params.HDRFormat)
	transfer := strings.ToUpper(params.Transfer)
	primaries := strings.ToUpper(params.Primaries)

	// 1. Dolby Vision
	if strings.Contains(hdrFormat, "DOLBY VISION") {
		if strings.Contains(hdrFormat, "HDR10+") {
			return "DoVi HDR10+"
		}
		if strings.Contains(hdrFormat, "HDR10") {
			return "DoVi HDR"
		}
		return "DoVi"
	}
	// 2. HDR10+
	if strings.Contains(hdrFormat, "HDR10+") || strings.Contains(hdrFormat, "SMPTE ST 2094") {
		return "HDR10+"
	}
	// 3. HDR Vivid
	if strings.Contains(hdrFormat, "VIVID") {
		return "HDR Vivid"
	}
	// 4. HDR10
	if strings.Contains(hdrFormat, "HDR10") || strings.Contains(hdrFormat, "SMPTE ST 2086") {
		return "HDR"
	}
	// 5. HLG
	if strings.Contains(transfer, "HLG") || strings.Contains(transfer, "ARIB STD-B67") {
		return "HLG"
	}
	// 6. implicit HDR10
	if strings.Contains(primaries, "BT.2020") && (strings.Contains(transfer, "PQ") || strings.Contains(transfer, "SMPTE ST 2084")) {
		return "HDR"
	}
	return ""
}

func extractBDInfoVideoLines(text string) []string {
	lines := strings.Split(text, "\n")
	videoLines := make([]string, 0, 32)
	currentSection := ""

	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}

		switch stripped {
		case "VIDEO:", "AUDIO:", "SUBTITLES:", "FILES:", "DISC INFO:", "PLAYLIST REPORT:":
			currentSection = strings.TrimSuffix(stripped, ":")
			continue
		}

		if currentSection != "VIDEO" {
			continue
		}
		upper := strings.ToUpper(stripped)
		if strings.Contains(upper, "-----") || strings.HasPrefix(upper, "CODEC") {
			continue
		}
		if strings.Contains(upper, "KBPS") || strings.Contains(upper, "MBPS") {
			videoLines = append(videoLines, stripped)
		}
	}
	return videoLines
}

func determineBDInfoHDRStandardTag(videoLines []string) string {
	combined := strings.ToUpper(strings.Join(videoLines, " "))
	hasDV := strings.Contains(combined, "DOLBY VISION")
	hasHDR10Plus := strings.Contains(combined, "HDR10+")
	hasHDR10 := strings.Contains(combined, "HDR10")
	hasHLG := strings.Contains(combined, "HLG")

	if hasDV {
		if hasHDR10Plus {
			return "DoVi HDR10+"
		}
		if hasHDR10 {
			return "DoVi HDR"
		}
		return "DoVi"
	}
	if hasHDR10Plus {
		return "HDR10+"
	}
	if hasHDR10 {
		return "HDR"
	}
	if hasHLG {
		return "HLG"
	}
	if strings.Contains(combined, "BT.2020") && !hasHDR10 {
		return "HDR"
	}
	return ""
}

func ExtractAudioInfoFromMediaText(text string, isBDInfo bool) AudioInfo {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return AudioInfo{}
	}
	if isBDInfo {
		return extractAudioInfoFromBDInfo(trimmed)
	}
	return extractAudioInfoFromMediaInfo(trimmed)
}

type parsedAudioTrack struct {
	BaseCodec     string
	SuffixTag     string
	ChannelLayout string
}

func extractAudioInfoFromMediaInfo(text string) AudioInfo {
	lines := strings.Split(text, "\n")
	tracks := make([]parsedAudioTrack, 0, 4)

	reAudioHeader := regexp.MustCompile(`(?i)^Audio(\s*#\d+)?$`)
	reSectionHeader := regexp.MustCompile(`(?i)^(Video|Text|Menu|General|Chapters|Audio)(\s*#\d+)?$`)

	for i := 0; i < len(lines); i++ {
		if !reAudioHeader.MatchString(strings.TrimSpace(lines[i])) {
			continue
		}

		curr := map[string]string{
			"fmt":        "",
			"commercial": "",
			"codec":      "",
			"profile":    "",
			"ch_count":   "",
			"ch_layout":  "",
			"title":      "",
		}

		j := i + 1
		for ; j < len(lines) && j < i+30; j++ {
			line := lines[j]
			stripped := strings.TrimSpace(line)

			if m := regexp.MustCompile(`(?i)^\s*Format\s*:\s*(.+?)\s*$`).FindStringSubmatch(line); len(m) == 2 {
				curr["fmt"] = m[1]
			} else if m := regexp.MustCompile(`(?i)^\s*Commercial\s+name\s*:\s*(.+?)\s*$`).FindStringSubmatch(line); len(m) == 2 {
				curr["commercial"] = m[1]
			} else if m := regexp.MustCompile(`(?i)^\s*Codec\s+ID\s*:\s*(.+?)\s*$`).FindStringSubmatch(line); len(m) == 2 {
				curr["codec"] = m[1]
			} else if m := regexp.MustCompile(`(?i)^\s*Format\s+profile\s*:\s*(.+?)\s*$`).FindStringSubmatch(line); len(m) == 2 {
				curr["profile"] = m[1]
			} else if m := regexp.MustCompile(`(?i)^\s*Title\s*:\s*(.+?)\s*$`).FindStringSubmatch(line); len(m) == 2 {
				curr["title"] = m[1]
			} else if m := regexp.MustCompile(`(?i)^\s*Channel\\(s\\)\s*:\s*(.+?)\s*$`).FindStringSubmatch(line); len(m) == 2 {
				curr["ch_count"] = m[1]
			} else if m := regexp.MustCompile(`(?i)^\s*Channel\s+layout\s*:\s*(.+?)\s*$`).FindStringSubmatch(line); len(m) == 2 {
				curr["ch_layout"] = m[1]
			}

			if stripped != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				if reSectionHeader.MatchString(stripped) && !strings.EqualFold(stripped, "Audio") {
					break
				}
			}
		}

		chLayout := channelLayoutFromMediaInfo(curr["ch_layout"], curr["ch_count"])
		if chLayout == "" && strings.TrimSpace(curr["title"]) != "" {
			if m := regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`).FindStringSubmatch(curr["title"]); len(m) == 2 {
				chLayout = m[1]
			}
		}

		if strings.TrimSpace(curr["fmt"]) != "" || strings.TrimSpace(curr["commercial"]) != "" || strings.TrimSpace(curr["codec"]) != "" || strings.TrimSpace(curr["profile"]) != "" {
			if base, suffix, ok := standardAudioCode(curr["fmt"], curr["commercial"], curr["codec"], curr["profile"]); ok {
				tracks = append(tracks, parsedAudioTrack{
					BaseCodec:     base,
					SuffixTag:     suffix,
					ChannelLayout: chLayout,
				})
			}
		}

		i = j - 1
	}

	return buildMediaInfoAudioFromTracks(tracks, true)
}

func extractAudioInfoFromBDInfo(text string) AudioInfo {
	lines := strings.Split(text, "\n")
	tracks := make([]parsedAudioTrack, 0, 4)

	audioStart := -1
	for i, line := range lines {
		clean := strings.ToUpper(cleanBBCode(line))
		if strings.Contains(clean, "AUDIO:") || strings.Contains(clean, "* AUDIO") {
			audioStart = i
			break
		}
	}
	if audioStart == -1 {
		return buildMediaInfoAudioFromTracks(tracks, false)
	}

	i := audioStart
	first := strings.ToUpper(strings.TrimSpace(cleanBBCode(lines[i])))
	if first == "AUDIO:" || first == "AUDIO" {
		i++
	}

	for ; i < len(lines); i++ {
		clean := cleanBBCode(lines[i])
		upper := strings.ToUpper(strings.TrimSpace(clean))
		if upper == "" {
			continue
		}
		if strings.HasPrefix(upper, "SUBTITLES") || strings.HasPrefix(upper, "FILES") || strings.HasPrefix(upper, "VIDEO") || strings.HasPrefix(upper, "DISC INFO") {
			break
		}
		if regexp.MustCompile(`(?i)^(Codec|Language|Bitrate|Description|-+)`).MatchString(strings.TrimSpace(clean)) {
			continue
		}

		rawStr := clean
		if strings.Contains(upper, "AUDIO") {
			rawStr = regexp.MustCompile(`(?i)^(\*|\s)*Audio:\s*`).ReplaceAllString(rawStr, "")
		}
		rawStr = strings.TrimSpace(rawStr)
		if rawStr == "" {
			continue
		}

		base, suffix, ok := standardAudioCode(rawStr, "", "", "")
		if !ok {
			continue
		}

		chStr := "2.0"
		if m := regexp.MustCompile(`(\d+\.\d+)`).FindStringSubmatch(rawStr); len(m) == 2 {
			chStr = m[1]
		}

		tracks = append(tracks, parsedAudioTrack{
			BaseCodec:     base,
			SuffixTag:     suffix,
			ChannelLayout: chStr,
		})
	}

	return buildMediaInfoAudioFromTracks(tracks, false)
}

func buildMediaInfoAudioFromTracks(tracks []parsedAudioTrack, isMediaInfo bool) AudioInfo {
	if len(tracks) == 0 {
		return AudioInfo{}
	}

	best := tracks[0]
	total := len(tracks)
	audioCount := ""
	if isMediaInfo && total > 1 {
		audioCount = strconv.Itoa(total) + "Audios"
	}

	allTracks := make([]AudioTrack, 0, total)
	for _, t := range tracks {
		allTracks = append(allTracks, AudioTrack{
			Codec:      t.BaseCodec,
			Channels:   defaultString(strings.TrimSpace(t.ChannelLayout), "2.0"),
			HasAtmos:   strings.EqualFold(t.SuffixTag, "Atmos"),
			AudioCount: audioCount,
		})
	}

	channels := defaultString(strings.TrimSpace(best.ChannelLayout), "2.0")
	if audioCount != "" {
		channels = channels + " " + audioCount
	}

	return AudioInfo{
		Codec:     best.BaseCodec,
		Channels:  channels,
		HasAtmos:  strings.EqualFold(best.SuffixTag, "Atmos"),
		AllTracks: allTracks,
	}
}

func cleanBBCode(text string) string {
	return regexp.MustCompile(`\\[/?\\w+\\]`).ReplaceAllString(strings.TrimSpace(text), "")
}

func channelLayoutFromMediaInfo(layoutLine string, countLine string) string {
	layout := strings.TrimSpace(layoutLine)
	if layout != "" {
		components := strings.Fields(strings.ToUpper(layout))
		if len(components) == 0 {
			return ""
		}
		num := len(components)
		for _, item := range components {
			if item == "LFE" {
				return strconv.Itoa(num-1) + ".1"
			}
		}
		return strconv.Itoa(num) + ".0"
	}

	count := strings.TrimSpace(countLine)
	if count != "" {
		if m := regexp.MustCompile(`(\d+)`).FindStringSubmatch(count); len(m) == 2 {
			n, _ := strconv.Atoi(m[1])
			switch n {
			case 8:
				return "7.1"
			case 6:
				return "5.1"
			case 2:
				return "2.0"
			case 1:
				return "1.0"
			default:
				return strconv.Itoa(n) + ".0"
			}
		}
	}
	return "2.0"
}

func standardAudioCode(fmt string, commercial string, codecID string, profile string) (string, string, bool) {
	f := strings.ToUpper(strings.TrimSpace(fmt))
	c := strings.ToUpper(strings.TrimSpace(commercial))
	cid := strings.ToUpper(strings.TrimSpace(codecID))
	p := strings.ToUpper(strings.TrimSpace(profile))
	full := strings.TrimSpace(strings.Join([]string{f, c, cid, p}, " "))

	if full == "" {
		return "", "", false
	}

	forbidden := []string{"JPEG", "PNG", "COVER", "ASS", "SSA", "S_TEXT", "TIMECODE", "MENU", "PGS"}
	for _, k := range forbidden {
		if strings.Contains(full, k) {
			return "", "", false
		}
	}
	if strings.Contains(full, "VIDEO") && !(strings.Contains(full, "AUDIO") || strings.Contains(full, "DTS") || strings.Contains(full, "DOLBY") || strings.Contains(full, "LPCM") || strings.Contains(full, "AAC") || strings.Contains(full, "FLAC") || strings.Contains(full, "PCM") || strings.Contains(full, "OPUS") || strings.Contains(full, "MPEG") || strings.Contains(full, "AV3A") || strings.Contains(full, "VIVID")) {
		return "", "", false
	}

	if strings.Contains(full, "AV3A") || strings.Contains(full, "AUDIO VIVID") || cid == "AV3A" {
		return "AV3A", "", true
	}
	if strings.Contains(full, "DTS") {
		if strings.Contains(full, "DTS:X") || strings.Contains(full, "DTSX") {
			return "DTS:X", "", true
		}
		if strings.Contains(p, "MA") || strings.Contains(full, "MASTER AUDIO") || strings.Contains(full, "XLL") {
			return "DTS-HD MA", "", true
		}
		if strings.Contains(p, "HRA") || strings.Contains(full, "HIGH RESOLUTION") {
			return "DTS-HD HR", "", true
		}
		return "DTS", "", true
	}
	if strings.Contains(full, "TRUEHD") || strings.Contains(f, "MLP") || strings.Contains(cid, "MLPA") {
		if strings.Contains(full, "ATMOS") {
			return "TrueHD", "Atmos", true
		}
		return "TrueHD", "", true
	}
	if strings.Contains(full, "E-AC-3") || strings.Contains(full, "EC-3") || strings.Contains(full, "DDP") || strings.Contains(full, "DIGITAL PLUS") {
		if strings.Contains(full, "ATMOS") || strings.Contains(full, "JOC") {
			return "DDP", "Atmos", true
		}
		return "DDP", "", true
	}
	if strings.Contains(full, "AC-3") || strings.Contains(full, "AC3") || strings.Contains(full, "DOLBY DIGITAL") {
		return "DD", "", true
	}
	if strings.Contains(full, "LPCM") {
		return "LPCM", "", true
	}
	if strings.Contains(full, "PCM") {
		return "PCM", "", true
	}
	if strings.Contains(full, "FLAC") {
		return "FLAC", "", true
	}
	if strings.Contains(full, "APE") {
		return "APE", "", true
	}
	if strings.Contains(full, "WAV") {
		return "WAV", "", true
	}
	if strings.Contains(full, "ALAC") {
		return "ALAC", "", true
	}
	if strings.Contains(full, "DSD") {
		return "DSD", "", true
	}
	if strings.Contains(full, "AAC") || strings.Contains(cid, "MP4A") {
		return "AAC", "", true
	}
	if strings.Contains(full, "OPUS") {
		return "Opus", "", true
	}
	if strings.Contains(full, "VORBIS") || strings.Contains(full, "OGG") {
		return "Vorbis", "", true
	}
	if strings.Contains(full, "MPEG AUDIO") || strings.Contains(full, "MP3") {
		return "MP3", "", true
	}
	return "", "", false
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
