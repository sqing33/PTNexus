package media

import (
	"regexp"
	"sort"
	"strings"
)

func ApplyMediaInfoOverrides(components []map[string]any, hdr HDRInfo, audio AudioInfo) []map[string]any {
	if len(components) == 0 {
		return components
	}

	if strings.TrimSpace(hdr.StandardTag) != "" {
		for idx := range components {
			if strings.TrimSpace(toStringAny(components[idx]["key"])) != "HDR格式" {
				continue
			}
			components[idx]["value"] = hdr.StandardTag
			break
		}
	}

	if strings.TrimSpace(audio.Codec) == "" && len(audio.AllTracks) == 0 {
		return components
	}

	existingAudio := ""
	for _, item := range components {
		if strings.TrimSpace(toStringAny(item["key"])) != "音频编码" {
			continue
		}
		existingAudio = strings.TrimSpace(toStringAny(item["value"]))
		break
	}

	if existingAudio != "" && len(audio.AllTracks) > 0 {
		best := findBestMatchingAudioTrack(existingAudio, audio.AllTracks)
		supplemented := supplementAudioInfo(existingAudio, best)
		if strings.TrimSpace(supplemented) != "" && supplemented != existingAudio {
			for idx := range components {
				if strings.TrimSpace(toStringAny(components[idx]["key"])) != "音频编码" {
					continue
				}
				components[idx]["value"] = supplemented
				break
			}
		}
		return components
	}

	if existingAudio == "" && strings.TrimSpace(audio.Codec) != "" {
		channelLayout := strings.TrimSpace(audio.Channels)
		audioCount := ""
		if strings.Contains(channelLayout, "Audios") {
			parts := strings.Fields(channelLayout)
			if len(parts) > 0 {
				channelLayout = parts[0]
			}
			if len(parts) > 1 {
				audioCount = strings.Join(parts[1:], " ")
			}
		}

		audioInfo := strings.TrimSpace(audio.Codec)
		if strings.TrimSpace(channelLayout) != "" {
			audioInfo += " " + strings.TrimSpace(channelLayout)
		}
		if audio.HasAtmos {
			audioInfo += " Atmos"
		}
		if strings.TrimSpace(audioCount) != "" {
			audioInfo += " " + strings.TrimSpace(audioCount)
		}

		audioInfo = strings.TrimSpace(audioInfo)
		if audioInfo != "" {
			for idx := range components {
				if strings.TrimSpace(toStringAny(components[idx]["key"])) != "音频编码" {
					continue
				}
				components[idx]["value"] = audioInfo
				break
			}
		}
	}

	return components
}

func findBestMatchingAudioTrack(sourceAudio string, tracks []AudioTrack) AudioTrack {
	if len(tracks) == 0 {
		return AudioTrack{}
	}

	sourceCodec := ""
	sourceChannels := ""
	sourceHasAtmos := regexp.MustCompile(`(?i)\bAtmos\b`).MatchString(sourceAudio)

	codecRe := regexp.MustCompile(`(?i)\b(DTS-?HD\s*MA|DTS-?HD\s*HR|DTS-?HD|DTS:X|DTS:D|DTS|TrueHD|DDP|E-AC-?3|AC3|FLAC|Opus|AAC|OGG|WAV|APE|ALAC|DSD|MP3|LPCM|PCM)\b|\b(DD\+)\b|\b(DD)\b`)
	if m := codecRe.FindStringSubmatch(sourceAudio); len(m) > 0 {
		// mimic Python's "lastindex" behavior: pick the last non-empty capture group
		for i := len(m) - 1; i >= 1; i-- {
			if strings.TrimSpace(m[i]) != "" {
				sourceCodec = strings.TrimSpace(m[i])
				break
			}
		}
	}

	if m := regexp.MustCompile(`\b(\d{1,2}\.\d(?:\.\d)?)\b`).FindStringSubmatch(sourceAudio); len(m) == 2 {
		sourceChannels = m[1]
	}

	best := tracks[0]
	bestScore := -1

	for _, track := range tracks {
		score := 0

		trackCodec := strings.TrimSpace(track.Codec)
		trackChannels := strings.TrimSpace(track.Channels)

		// 1) codec match
		if trackCodec != "" && strings.EqualFold(trackCodec, sourceCodec) {
			score += 50
		} else if isCodecCompatible(sourceCodec, trackCodec) {
			score += 30
		}

		// 2) channel match
		if sourceChannels != "" && trackChannels != "" {
			if sourceChannels == trackChannels {
				score += 30
			} else {
				diff := absFloat(parseChannelsForScore(sourceChannels) - parseChannelsForScore(trackChannels))
				if diff <= 1 {
					score += 20
				} else if diff <= 2 {
					score += 10
				} else if diff <= 4 {
					score += 5
				}
			}
		} else if sourceChannels == "" && trackChannels != "" {
			chNum := parseChannelsForScore(trackChannels)
			switch {
			case chNum >= 71:
				score += 20
			case chNum >= 51:
				score += 15
			case chNum >= 31:
				score += 10
			default:
				score += 5
			}
		}

		// 3) Atmos match
		if sourceHasAtmos == track.HasAtmos {
			score += 20
		}

		if score > bestScore {
			bestScore = score
			best = track
		}
	}

	if bestScore == 0 {
		return tracks[0]
	}
	return best
}

func isCodecCompatible(codec1 string, codec2 string) bool {
	normalize := func(codec string) string {
		upper := strings.ToUpper(codec)
		upper = regexp.MustCompile(`[-\s]`).ReplaceAllString(upper, "")
		return upper
	}
	n1 := normalize(codec1)
	n2 := normalize(codec2)

	dtsVariants := []string{"DTS", "DTSHDMA", "DTSHDHR", "DTSHD", "DTSX", "DTS:D"}
	if containsAny(n1, dtsVariants) && containsAny(n2, dtsVariants) {
		return true
	}

	ddVariants := []string{"DD", "DDP", "DD+", "EAC3", "AC3"}
	if containsAny(n1, ddVariants) && containsAny(n2, ddVariants) {
		return true
	}

	if strings.Contains(n1, "TRUEHD") && strings.Contains(n2, "TRUEHD") {
		return true
	}
	return false
}

func containsAny(s string, parts []string) bool {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func parseChannelsForScore(ch string) float64 {
	if strings.TrimSpace(ch) == "" {
		return 0
	}
	// "7.1" -> 71, "5.1.2" -> 512
	normalized := strings.ReplaceAll(ch, ".", "")
	var out float64
	for _, r := range normalized {
		if r < '0' || r > '9' {
			continue
		}
		out = out*10 + float64(r-'0')
	}
	return out
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func supplementAudioInfo(sourceAudio string, track AudioTrack) string {
	if strings.TrimSpace(sourceAudio) == "" {
		return sourceAudio
	}
	if strings.TrimSpace(track.Codec) == "" && strings.TrimSpace(track.Channels) == "" && !track.HasAtmos && strings.TrimSpace(track.AudioCount) == "" {
		return sourceAudio
	}

	parts := map[string]string{
		"codec":       "",
		"channels":    "",
		"atmos":       "",
		"audio_count": "",
	}

	codecRe := regexp.MustCompile(`(?i)\b(DTS-?HD\s*MA|DTS-?HD\s*HR|DTS-?HD|DTS:X|DTS:D|DTS|TrueHD|DDP|E-AC-?3|AC3|FLAC|Opus|AAC|OGG|WAV|APE|ALAC|DSD|MP3|LPCM|PCM)\b|\b(DD\+)\b|\b(DD)\b`)
	if m := codecRe.FindStringSubmatch(sourceAudio); len(m) > 0 {
		for i := len(m) - 1; i >= 1; i-- {
			if strings.TrimSpace(m[i]) != "" {
				parts["codec"] = strings.TrimSpace(m[i])
				break
			}
		}
	}

	if m := regexp.MustCompile(`\b(\d{1,2}\.\d(?:\.\d)?)\b`).FindStringSubmatch(sourceAudio); len(m) == 2 {
		parts["channels"] = m[1]
	}

	if m := regexp.MustCompile(`(?i)\b(Atmos|X)\b`).FindStringSubmatch(sourceAudio); len(m) == 2 {
		parts["atmos"] = m[1]
	}

	if m := regexp.MustCompile(`(?i)\b(\d+Audios?)\b`).FindStringSubmatch(sourceAudio); len(m) == 2 {
		parts["audio_count"] = m[1]
	}

	// supplement missing
	if parts["channels"] == "" && strings.TrimSpace(track.Channels) != "" {
		parts["channels"] = strings.TrimSpace(track.Channels)
	}
	if parts["atmos"] == "" && track.HasAtmos {
		parts["atmos"] = "Atmos"
	}
	if parts["audio_count"] == "" && strings.TrimSpace(track.AudioCount) != "" {
		parts["audio_count"] = strings.TrimSpace(track.AudioCount)
	}

	if strings.EqualFold(parts["codec"], "DD+") {
		parts["codec"] = "DDP"
	}

	out := make([]string, 0, 4)
	if strings.TrimSpace(parts["codec"]) != "" {
		out = append(out, strings.TrimSpace(parts["codec"]))
	}
	if strings.TrimSpace(parts["channels"]) != "" {
		out = append(out, strings.TrimSpace(parts["channels"]))
	}
	if strings.TrimSpace(parts["atmos"]) != "" {
		out = append(out, strings.TrimSpace(parts["atmos"]))
	}
	if strings.TrimSpace(parts["audio_count"]) != "" {
		out = append(out, strings.TrimSpace(parts["audio_count"]))
	}

	result := strings.TrimSpace(strings.Join(out, " "))
	if result == "" {
		return sourceAudio
	}
	return result
}

// keep deterministic ordering if we ever need it
func sortUniqueStrings(items []string) []string {
	uniq := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := uniq[key]; ok {
			continue
		}
		uniq[key] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
