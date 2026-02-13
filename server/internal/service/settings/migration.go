package settings

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type DownloaderIDMapping struct {
	OldID string `json:"old_id"`
	NewID string `json:"new_id"`
	Host  string `json:"host"`
	Name  string `json:"name"`
}

func GenerateDownloaderIDFromHost(host string) (string, error) {
	normalized, err := normalizeDownloaderHost(host)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])[:16], nil
}

func normalizeDownloaderHost(rawHost string) (string, error) {
	host := strings.TrimSpace(rawHost)
	if host == "" {
		return "", fmt.Errorf("host不能为空")
	}

	var hostname string
	var port string

	if strings.HasPrefix(strings.ToLower(host), "http://") || strings.HasPrefix(strings.ToLower(host), "https://") {
		parsed, err := url.Parse(host)
		if err != nil {
			return "", fmt.Errorf("解析host失败: %w", err)
		}
		hostname = strings.TrimSpace(parsed.Hostname())
		port = strings.TrimSpace(parsed.Port())
	} else {
		part := strings.Split(host, "/")[0]
		part = strings.TrimSpace(part)
		if part == "" {
			return "", fmt.Errorf("无法从host '%s' 中提取有效的主机名", rawHost)
		}

		if strings.Count(part, ":") == 1 {
			tokens := strings.SplitN(part, ":", 2)
			hostname = strings.TrimSpace(tokens[0])
			portCandidate := strings.TrimSpace(tokens[1])
			if portCandidate != "" {
				if _, err := strconv.Atoi(portCandidate); err == nil {
					port = portCandidate
				}
			}
		} else {
			hostname = part
		}
	}

	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return "", fmt.Errorf("无法从host '%s' 中提取有效的主机名", rawHost)
	}

	base := hostname
	if net.ParseIP(hostname) == nil {
		base = strings.ToLower(hostname)
	}
	if strings.TrimSpace(port) != "" {
		base = base + ":" + strings.TrimSpace(port)
	}
	return base, nil
}

func (s *SettingsService) BuildDownloaderIDMappings() []DownloaderIDMapping {
	cfg := s.cfg.Get()
	rawDownloaders := toSlice(cfg["downloaders"])
	result := make([]DownloaderIDMapping, 0)
	for _, raw := range rawDownloaders {
		downloader, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		oldID := strings.TrimSpace(toString(downloader["id"], ""))
		host := strings.TrimSpace(toString(downloader["host"], ""))
		name := strings.TrimSpace(toString(downloader["name"], "未命名"))
		if oldID == "" || host == "" {
			continue
		}
		newID, err := GenerateDownloaderIDFromHost(host)
		if err != nil {
			continue
		}
		if oldID != newID {
			result = append(result, DownloaderIDMapping{OldID: oldID, NewID: newID, Host: host, Name: name})
		}
	}
	return result
}

func (s *SettingsService) ApplyDownloaderIDMappings(mappings []DownloaderIDMapping) (int, error) {
	if len(mappings) == 0 {
		return 0, nil
	}

	mappingMap := map[string]DownloaderIDMapping{}
	for _, item := range mappings {
		if strings.TrimSpace(item.OldID) == "" || strings.TrimSpace(item.NewID) == "" {
			continue
		}
		mappingMap[item.OldID] = item
	}
	if len(mappingMap) == 0 {
		return 0, nil
	}

	cfg := s.cfg.Get()
	rawDownloaders := toSlice(cfg["downloaders"])
	changed := 0
	for index, raw := range rawDownloaders {
		downloader, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		oldID := strings.TrimSpace(toString(downloader["id"], ""))
		mapping, exists := mappingMap[oldID]
		if !exists {
			continue
		}
		downloader["id"] = mapping.NewID
		rawDownloaders[index] = downloader
		changed++
	}
	cfg["downloaders"] = rawDownloaders
	if err := s.cfg.Save(cfg); err != nil {
		return 0, err
	}
	return changed, nil
}
