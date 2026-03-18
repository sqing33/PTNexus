package repair

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
)

type subtitleStreamProbe struct {
	Streams []subtitleProbeStream `json:"streams"`
}

type subtitleProbeStream struct {
	Index       int               `json:"index"`
	CodecName   string            `json:"codec_name"`
	Disposition map[string]any    `json:"disposition"`
	Tags        map[string]string `json:"tags"`
}

type ffprobePackets struct {
	Packets []struct {
		PTSTime      string `json:"pts_time"`
		DurationTime string `json:"duration_time"`
	} `json:"packets"`
}

type subtitleEvent struct {
	Start float64
	End   float64
}

// getSmartScreenshotPoints 复刻 Python 版 _get_smart_screenshot_points：优先基于字幕 packet 选点，失败回退由调用方处理。
func getSmartScreenshotPoints(ffprobePath string, videoPath string, want int) []float64 {
	return getSmartScreenshotPointsForSubtitle(ffprobePath, videoPath, want, 0, false)
}

func getSmartScreenshotPointsForSubtitle(ffprobePath string, videoPath string, want int, selectedSID int, hasSelected bool) []float64 {
	logx.PlainInfof("")
	logx.PlainInfof("--- 开始智能截图时间点分析 (快速扫描模式) ---")
	if strings.TrimSpace(ffprobePath) == "" {
		logx.PlainWarnf("警告: 未找到 ffprobe，无法进行智能分析。")
		return nil
	}

	duration, err := probeDurationSeconds(videoPath)
	if err != nil || duration <= 0 {
		logx.PlainWarnf("错误：使用 ffprobe 获取视频时长失败。%v", err)
		return nil
	}
	logx.PlainInfof("视频总时长: %.2f 秒", duration)

	inspection, chosenStream, err := resolveScreenshotSubtitleSelection(ffprobePath, videoPath, selectedSID, hasSelected)
	if err != nil {
		logx.PlainWarnf("探测字幕流失败: %v", err)
		return nil
	}
	if hasSelected && selectedSID == 0 {
		logx.PlainInfof("   -> 当前选择无字幕模式，回退到均匀选点。")
		return nil
	}
	if chosenStream == nil {
		if inspection.SubtitleState == ScreenshotSubtitleStateNoUsableSubtitle {
			logx.PlainInfof("未找到合适的字幕流。")
		}
		return nil
	}
	if !chosenStream.SupportsEventExtraction {
		logx.PlainInfof("   -> 当前字幕流格式 %s 不支持智能事件分析，将回退到均匀选点。", strings.ToUpper(chosenStream.CodecName))
		return nil
	}

	if hasSelected {
		logx.PlainInfof("   ✅ 已切换到字幕流 SID=%d (格式: %s)，流索引: %d", chosenStream.SubtitleSID, strings.ToUpper(chosenStream.CodecName), chosenStream.StreamIndex)
	} else {
		logx.PlainInfof("   ✅ 找到最优字幕流 (格式: %s)，流索引: %d", strings.ToUpper(chosenStream.CodecName), chosenStream.StreamIndex)
	}

	events := extractSubtitleEventsByReadIntervals(
		ffprobePath,
		videoPath,
		duration,
		chosenStream.StreamIndex,
		chosenStream.StreamOrdinal,
		chosenStream.CodecName,
	)
	if len(events) == 0 {
		return nil
	}
	if len(events) < want {
		logx.PlainInfof("有效字幕数量不足，无法启动智能选择。")
		return nil
	}

	goldenStart, goldenEnd := duration*0.30, duration*0.80
	golden := make([]subtitleEvent, 0, len(events))
	for _, e := range events {
		if e.Start >= goldenStart && e.End <= goldenEnd {
			golden = append(golden, e)
		}
	}
	logx.PlainInfof("   -> 在视频中部 (%s - %s) 找到 %d 个黄金字幕事件。", formatSecondClock(goldenStart), formatSecondClock(goldenEnd), len(golden))

	target := golden
	if len(target) < want {
		logx.PlainInfof("   -> 黄金字幕数量不足，将从所有字幕事件中随机选择。")
		target = events
	}
	sort.SliceStable(target, func(i, j int) bool { return target[i].Start < target[j].Start })

	chosen := selectWellDistributedEvents(target, want)
	if len(chosen) < want {
		return nil
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	points := make([]float64, 0, want)
	for i, ev := range chosen {
		d := ev.End - ev.Start
		if d <= 0 {
			continue
		}
		offset := d*0.1 + r.Float64()*(d*0.8)
		point := ev.Start + offset
		points = append(points, point)
		logx.PlainInfof("   -> 选中时间段 [%s - %s], 截图点: %s (第%d张)", formatSecondClock(ev.Start), formatSecondClock(ev.End), formatSecondClock(point), i+1)
	}
	sort.Float64s(points)
	return points
}

func extractSubtitleEventsByReadIntervals(ffprobePath, videoPath string, duration float64, streamIndex int, streamOrdinal int, codec string) []subtitleEvent {
	probePoints := []float64{0.2, 0.4, 0.6, 0.8}
	probeDuration := 60.0

	intervals := make([]string, 0, len(probePoints))
	for _, p := range probePoints {
		start := duration * p
		end := start + probeDuration
		if end > duration {
			end = duration
		}
		intervals = append(intervals, fmt.Sprintf("%g%%%g", start, end))
	}
	readIntervalsArg := strings.Join(intervals, ",")

	// Python 版这里用的是 stream index；为了兼容不同 ffprobe 版本，这里两种写法都尝试。
	streamSpecs := []string{strconv.Itoa(streamIndex), fmt.Sprintf("s:%d", streamOrdinal)}
	var lastErr error
	for _, spec := range streamSpecs {
		cmd := exec.Command(
			ffprobePath,
			"-v", "quiet",
			"-read_intervals", readIntervalsArg,
			"-print_format", "json",
			"-show_packets",
			"-select_streams", spec,
			videoPath,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			lastErr = fmt.Errorf("%s", strings.TrimSpace(string(out)))
			continue
		}

		var packets ffprobePackets
		if jsonErr := json.Unmarshal(out, &packets); jsonErr != nil {
			lastErr = jsonErr
			continue
		}

		events := make([]subtitleEvent, 0, len(packets.Packets))
		switch strings.ToLower(strings.TrimSpace(codec)) {
		case "ass", "subrip":
			for _, p := range packets.Packets {
				start, err1 := strconv.ParseFloat(strings.TrimSpace(p.PTSTime), 64)
				dur, err2 := strconv.ParseFloat(strings.TrimSpace(p.DurationTime), 64)
				if err1 != nil || err2 != nil {
					continue
				}
				if dur > 0.1 {
					events = append(events, subtitleEvent{Start: start, End: start + dur})
				}
			}
		case "hdmv_pgs_subtitle":
			for i := 0; i+1 < len(packets.Packets); i += 2 {
				start, err1 := strconv.ParseFloat(strings.TrimSpace(packets.Packets[i].PTSTime), 64)
				end, err2 := strconv.ParseFloat(strings.TrimSpace(packets.Packets[i+1].PTSTime), 64)
				if err1 != nil || err2 != nil {
					continue
				}
				if end > start && (end-start) > 0.1 {
					events = append(events, subtitleEvent{Start: start, End: end})
				}
			}
		}

		if len(events) == 0 {
			lastErr = fmt.Errorf("在指定区间内未能提取到任何有效的时间事件。")
			continue
		}
		logx.PlainInfof("   ✅ 成功从指定区间提取到 %d 条有效字幕事件。", len(events))
		return events
	}

	if lastErr != nil {
		logx.PlainWarnf("智能提取时间事件失败: %v", lastErr)
	}
	return nil
}

func selectWellDistributedEvents(sortedEvents []subtitleEvent, want int) []subtitleEvent {
	if len(sortedEvents) <= want {
		return sortedEvents
	}
	n := len(sortedEvents)
	selected := make([]subtitleEvent, 0, want)

	if want == 1 {
		selected = append(selected, sortedEvents[n/2])
	} else if want <= 3 {
		indices := []int{0, n / 2, n - 1}
		for i := 0; i < want; i++ {
			selected = append(selected, sortedEvents[indices[i]])
		}
	} else {
		interval := n / (want + 1)
		for i := 0; i < want; i++ {
			idx := interval * (i + 1)
			if idx > n-1 {
				idx = n - 1
			}
			selected = append(selected, sortedEvents[idx])
		}
	}

	// 至少 30 秒间隔。
	minInterval := 30.0
	filtered := make([]subtitleEvent, 0, want)
	for _, ev := range selected {
		ok := true
		for _, ex := range filtered {
			if absFloat(ev.Start-ex.Start) < minInterval {
				ok = false
				break
			}
		}
		if ok {
			filtered = append(filtered, ev)
			continue
		}
		for _, alt := range sortedEvents {
			allGood := true
			for _, ex := range filtered {
				if absFloat(alt.Start-ex.Start) < minInterval {
					allGood = false
					break
				}
			}
			if allGood {
				filtered = append(filtered, alt)
				break
			}
		}
	}

	if len(filtered) < want {
		remaining := make([]subtitleEvent, 0, len(sortedEvents))
		for _, ev := range sortedEvents {
			found := false
			for _, ex := range filtered {
				if ev.Start == ex.Start && ev.End == ex.End {
					found = true
					break
				}
			}
			if !found {
				remaining = append(remaining, ev)
			}
		}
		needed := want - len(filtered)
		if len(remaining) > 0 && needed > 0 {
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			r.Shuffle(len(remaining), func(i, j int) { remaining[i], remaining[j] = remaining[j], remaining[i] })
			if needed > len(remaining) {
				needed = len(remaining)
			}
			filtered = append(filtered, remaining[:needed]...)
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Start < filtered[j].Start })
	if len(filtered) > want {
		return filtered[:want]
	}
	return filtered
}

func formatSecondClock(second float64) string {
	total := int(second)
	if total < 0 {
		total = 0
	}
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func toBoolAny(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case int:
		return t != 0
	case string:
		trimmed := strings.TrimSpace(strings.ToLower(t))
		return trimmed == "1" || trimmed == "true" || trimmed == "yes"
	default:
		return false
	}
}
