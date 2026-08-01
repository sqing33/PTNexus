package reversemapping

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pt-nexus/server/internal/config"
	"gopkg.in/yaml.v3"
)

func Build(configData map[string]any) map[string]any {
	reverse := newEmptyReverseMap()
	if applyReverseMappingsFromYAML(reverse) {
		addFallbackMappings(reverse)
		return toAnyMap(reverse)
	}

	globalStandardKeys := loadGlobalStandardKeys(configData)

	categorySources := map[string]string{
		"type":        "type",
		"medium":      "medium",
		"video_codec": "video_codec",
		"audio_codec": "audio_codec",
		"resolution":  "resolution",
		"source":      "source",
		"team":        "team",
		"tags":        "tag",
	}

	for targetCategory, sourceCategory := range categorySources {
		mappings := toStringMap(globalStandardKeys[sourceCategory])
		for displayName, standardValue := range mappings {
			standard := strings.TrimSpace(standardValue)
			if standard == "" {
				continue
			}
			display := strings.TrimSpace(displayName)
			// Python baseline uses last-wins for tags (overwrite), but first-wins for other categories.
			if targetCategory == "tags" {
				reverse[targetCategory][standard] = display
				continue
			}
			if _, exists := reverse[targetCategory][standard]; exists {
				continue
			}
			reverse[targetCategory][standard] = display
		}
	}

	addFallbackMappings(reverse)
	return toAnyMap(reverse)
}

func applyReverseMappingsFromYAML(reverse map[string]map[string]string) bool {
	paths := config.ResolveRuntimePaths()
	mappingPath := strings.TrimSpace(paths.GlobalMapYML)
	if mappingPath == "" {
		return false
	}

	content, err := os.ReadFile(mappingPath)
	if err != nil {
		return false
	}

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return false
	}
	if len(root.Content) == 0 {
		return false
	}

	doc := root.Content[0]
	if doc == nil || doc.Kind != yaml.MappingNode {
		return false
	}

	globalStandardKeys := mappingValueNode(doc, "global_standard_keys")
	if globalStandardKeys == nil || globalStandardKeys.Kind != yaml.MappingNode {
		return false
	}

	categoryNodes := map[string]string{
		"type":        "type",
		"medium":      "medium",
		"video_codec": "video_codec",
		"audio_codec": "audio_codec",
		"resolution":  "resolution",
		"source":      "source",
		"team":        "team",
		"tags":        "tag",
	}

	for targetCategory, sourceCategory := range categoryNodes {
		categoryNode := mappingValueNode(globalStandardKeys, sourceCategory)
		if categoryNode == nil || categoryNode.Kind != yaml.MappingNode {
			continue
		}
		if targetCategory == "tags" {
			applyCategoryNodeLastWins(reverse[targetCategory], categoryNode)
		} else {
			applyCategoryNodeFirstWins(reverse[targetCategory], categoryNode)
		}
	}

	return true
}

func mappingValueNode(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for idx := 0; idx+1 < len(mapping.Content); idx += 2 {
		k := mapping.Content[idx]
		v := mapping.Content[idx+1]
		if k == nil {
			continue
		}
		if strings.TrimSpace(k.Value) == key {
			return v
		}
	}
	return nil
}

func applyCategoryNodeFirstWins(out map[string]string, mappingsNode *yaml.Node) {
	if out == nil || mappingsNode == nil || mappingsNode.Kind != yaml.MappingNode {
		return
	}
	for idx := 0; idx+1 < len(mappingsNode.Content); idx += 2 {
		k := mappingsNode.Content[idx]
		v := mappingsNode.Content[idx+1]
		if k == nil || v == nil {
			continue
		}
		displayName := strings.TrimSpace(k.Value)
		// YAML null values should be skipped (Python baseline filters out null/empty standard values).
		if strings.EqualFold(strings.TrimSpace(v.Tag), "!!null") {
			continue
		}
		standard := strings.TrimSpace(v.Value)
		if displayName == "" || standard == "" {
			continue
		}
		if _, exists := out[standard]; exists {
			continue
		}
		out[standard] = displayName
	}
}

func applyCategoryNodeLastWins(out map[string]string, mappingsNode *yaml.Node) {
	if out == nil || mappingsNode == nil || mappingsNode.Kind != yaml.MappingNode {
		return
	}
	for idx := 0; idx+1 < len(mappingsNode.Content); idx += 2 {
		k := mappingsNode.Content[idx]
		v := mappingsNode.Content[idx+1]
		if k == nil || v == nil {
			continue
		}
		displayName := strings.TrimSpace(k.Value)
		// YAML null values should be skipped (Python baseline filters out null/empty standard values).
		if strings.EqualFold(strings.TrimSpace(v.Tag), "!!null") {
			continue
		}
		standard := strings.TrimSpace(v.Value)
		if displayName == "" || standard == "" {
			continue
		}
		out[standard] = displayName
	}
}

func newEmptyReverseMap() map[string]map[string]string {
	return map[string]map[string]string{
		"type":        {},
		"medium":      {},
		"video_codec": {},
		"audio_codec": {},
		"resolution":  {},
		"source":      {},
		"team":        {},
		"tags":        {},
	}
}

func toAnyMap(input map[string]map[string]string) map[string]any {
	result := map[string]any{}
	for key, value := range input {
		result[key] = value
	}
	return result
}

func loadGlobalStandardKeys(configData map[string]any) map[string]any {
	if yamlMappings := loadGlobalStandardKeysFromYAML(); len(yamlMappings) > 0 {
		return yamlMappings
	}
	if configData == nil {
		return map[string]any{}
	}
	return toAnyMapString(configData["global_standard_keys"])
}

func loadGlobalStandardKeysFromYAML() map[string]any {
	paths := config.ResolveRuntimePaths()
	mappingPath := strings.TrimSpace(paths.GlobalMapYML)
	if mappingPath == "" {
		return map[string]any{}
	}
	if filepath.Clean(mappingPath) == "." {
		return map[string]any{}
	}
	content, err := os.ReadFile(mappingPath)
	if err != nil {
		return map[string]any{}
	}
	parsed := map[string]any{}
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		return map[string]any{}
	}
	return toAnyMapString(parsed["global_standard_keys"])
}

func toAnyMapString(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[any]any:
		result := map[string]any{}
		for key, item := range typed {
			result[strings.TrimSpace(toString(key))] = item
		}
		return result
	default:
		return map[string]any{}
	}
}

func toStringMap(value any) map[string]string {
	result := map[string]string{}
	source := toAnyMapString(value)
	for key, item := range source {
		name := strings.TrimSpace(key)
		if name == "" {
			continue
		}
		standard := strings.TrimSpace(toString(item))
		if standard == "" {
			continue
		}
		result[name] = standard
	}
	return result
}

func toString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func addFallbackMappings(reverse map[string]map[string]string) {
	if len(reverse["type"]) == 0 {
		reverse["type"]["category.movie"] = "电影"
		reverse["type"]["category.tv_series"] = "剧集"
		reverse["type"]["category.animation"] = "动画"
		reverse["type"]["category.documentaries"] = "纪录片"
		reverse["type"]["category.music"] = "音乐"
		reverse["type"]["category.other"] = "其他"
	}
	if len(reverse["medium"]) == 0 {
		reverse["medium"]["medium.bluray"] = "Blu-ray"
		reverse["medium"]["medium.uhd_bluray"] = "UHD Blu-ray"
		reverse["medium"]["medium.remux"] = "Remux"
		reverse["medium"]["medium.encode"] = "Encode"
		reverse["medium"]["medium.webdl"] = "WEB-DL"
		reverse["medium"]["medium.webrip"] = "WebRip"
		reverse["medium"]["medium.hdtv"] = "HDTV"
		reverse["medium"]["medium.dvd"] = "DVD"
		reverse["medium"]["medium.other"] = "其他"
	}
	if len(reverse["video_codec"]) == 0 {
		reverse["video_codec"]["video.h264"] = "H.264/AVC"
		reverse["video_codec"]["video.h265"] = "H.265/HEVC"
		reverse["video_codec"]["video.x265"] = "x265"
		reverse["video_codec"]["video.vc1"] = "VC-1"
		reverse["video_codec"]["video.mpeg2"] = "MPEG-2"
		reverse["video_codec"]["video.av1"] = "AV1"
		reverse["video_codec"]["video.other"] = "其他"
	}
	if len(reverse["audio_codec"]) == 0 {
		reverse["audio_codec"]["audio.flac"] = "FLAC"
		reverse["audio_codec"]["audio.dts"] = "DTS"
		reverse["audio_codec"]["audio.dts_hd_ma"] = "DTS-HD MA"
		reverse["audio_codec"]["audio.dtsx"] = "DTS:X"
		reverse["audio_codec"]["audio.truehd"] = "TrueHD"
		reverse["audio_codec"]["audio.truehd_atmos"] = "TrueHD Atmos"
		reverse["audio_codec"]["audio.ac3"] = "AC-3"
		reverse["audio_codec"]["audio.ddp"] = "E-AC-3"
		reverse["audio_codec"]["audio.aac"] = "AAC"
		reverse["audio_codec"]["audio.mp3"] = "MP3"
		reverse["audio_codec"]["audio.other"] = "其他"
	}
	if len(reverse["resolution"]) == 0 {
		reverse["resolution"]["resolution.r8k"] = "8K"
		reverse["resolution"]["resolution.r4k"] = "4K"
		reverse["resolution"]["resolution.r2160p"] = "2160p"
		reverse["resolution"]["resolution.r1080p"] = "1080p"
		reverse["resolution"]["resolution.r1080i"] = "1080i"
		reverse["resolution"]["resolution.r720p"] = "720p"
		reverse["resolution"]["resolution.r480p"] = "480p"
		reverse["resolution"]["resolution.other"] = "其他"
	}
	if len(reverse["source"]) == 0 {
		reverse["source"]["source.china"] = "中国"
		reverse["source"]["source.hongkong"] = "香港"
		reverse["source"]["source.taiwan"] = "台湾"
		reverse["source"]["source.western"] = "美国"
		reverse["source"]["source.uk"] = "英国"
		reverse["source"]["source.japan"] = "日本"
		reverse["source"]["source.korea"] = "韩国"
		reverse["source"]["source.other"] = "其他"
	}
	if len(reverse["team"]) == 0 {
		reverse["team"]["team.other"] = "其他"
	}
	if len(reverse["tags"]) == 0 {
		reverse["tags"]["tag.DIY"] = "DIY"
		reverse["tags"]["tag.中字"] = "中字"
		reverse["tags"]["tag.HDR"] = "HDR"
		reverse["tags"]["tag.HDR10"] = "HDR10"
		reverse["tags"]["tag.HDR10+"] = "HDR10+"
	}
}
