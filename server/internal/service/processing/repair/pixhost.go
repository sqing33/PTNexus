package repair

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/pt-nexus/server/internal/platform/logx"
)

const maxPosterBytes = 25 * 1024 * 1024
const posterTransferLogModule = "杩佺Щ-娴锋姤杞瓨"
const posterTransferDownloadRetry = 2
const pixhostOutputDirectHost = "img2.pixhost.cc"
const pixhostUploadAPIURL = "https://api.pixhost.cc/images"

// PixhostUploadConfig 鎻忚堪 Pixhost 涓婁紶 API 涓庢渶缁堢洿閾惧煙鍚嶃€?
type PixhostUploadConfig struct {
	DirectHost   string
	UploadAPIURL string
}

var (
	rePixhostDirect             = regexp.MustCompile(`(\d+)/([^/]+\.(?:jpg|jpeg|png|gif|webp))`)
	rePixhostOgImage            = regexp.MustCompile(`(?is)<meta[^>]+property=["']og:image["'][^>]*content=["']([^"']+)["']`)
	rePixhostImageTag           = regexp.MustCompile(`(?is)<img[^>]+id=["']image["'][^>]*src=["']([^"']+)["']`)
	rePixhostThumbSuffix        = regexp.MustCompile(`_[^.]{1,3}\.(jpg|jpeg|png|gif|webp)$`)
	rePixhostDirectURL          = regexp.MustCompile(`^https://img\d+\.pixhost\.(?:to|cc)/images/\d+/[^/]+\.(jpg|jpeg|png|gif|webp)$`)
	posterTransferProxyPrefixes = []string{
		"http://pt-nexus-proxy.sqing33.dpdns.org/",
		"http://pt-nexus-proxy.1395251710.workers.dev/",
	}
)

// NormalizePosterBBCode 瑙勮寖鍖栨捣鎶ュ瓧娈碉紝缁熶竴杈撳嚭涓哄崟涓?[img]...[/img]銆?
func NormalizePosterBBCode(raw string) string {
	return NormalizePosterBBCodeWithConfig(raw, nil)
}

// NormalizePosterBBCodeWithConfig 鎸?rootConfig 涓殑 image_hoster 瑙勮寖鍖栨捣鎶ュ瓧娈点€?
func NormalizePosterBBCodeWithConfig(raw string, rootConfig map[string]any) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	urls := ExtractImageURLsFromText(trimmed)
	if len(urls) == 0 {
		return trimmed
	}
	primary := strings.TrimSpace(urls[0])
	if primary == "" {
		return trimmed
	}

	normalized := NormalizePosterURLWithConfig(primary, rootConfig)
	if normalized == "" {
		normalized = primary
	}
	return "[img]" + normalized + "[/img]"
}

// NormalizePosterURL 瀵规捣鎶?URL 鍋氱洿閾句慨澶嶏紝骞跺敖閲忚浆瀛樺埌 Pixhost銆?
func NormalizePosterURL(raw string) string {
	return NormalizePosterURLWithConfig(raw, nil)
}

// NormalizePosterURLWithConfig 鎸?rootConfig 涓殑 image_hoster 瀵规捣鎶?URL 鍋氱洿閾句慨澶嶅苟杞瓨銆?
func NormalizePosterURLWithConfig(raw string, rootConfig map[string]any) string {
	url := strings.TrimSpace(raw)
	if url == "" {
		return ""
	}

	hoster := GetImageHosterFromConfig(rootConfig)
	if hoster == "agsv" {
		return normalizePosterURLForChevereto(url, GetCheveretoConfigFromRootConfig(rootConfig))
	}
	return normalizePosterURLForPixhost(url, GetPixhostUploadConfigFromRootConfig(rootConfig))
}

func normalizePosterURLForChevereto(url string, cfg CheveretoUploadConfig) string {
	if cfg.BaseURL == "" {
		// 娌℃湁閰嶇疆鍩熷悕锛屽洖閫€鍒板師濮?URL
		logx.Warnf(posterTransferLogModule, "鏈棩鍥惧簥鍩熷悕鏈厤缃紝鍥為€€鍘熷URL source=%s", CompactLogText(url, 160))
		return url
	}

	token, err := CheveretoLogin(cfg)
	if err != nil {
		logx.Warnf(posterTransferLogModule, "鏈棩鍥惧簥鐧诲綍澶辫触锛屽洖閫€鍘熷URL source=%s err=%v", CompactLogText(url, 160), err)
		return url
	}

	transferred, err := TransferRemoteImageToChevereto(url, cfg, token)
	if err != nil || strings.TrimSpace(transferred) == "" {
		if err != nil {
			logx.Warnf(posterTransferLogModule, "娴锋姤杞瓨鏈棩鍥惧簥澶辫触锛屽洖閫€鍘熷URL source=%s err=%v", CompactLogText(url, 160), err)
		} else {
			logx.Warnf(posterTransferLogModule, "娴锋姤杞瓨鏈棩鍥惧簥澶辫触锛屽洖閫€鍘熷URL source=%s err=empty transfer result", CompactLogText(url, 160))
		}
		return url
	}
	logx.Infof(posterTransferLogModule, "娴锋姤杞瓨鏈棩鍥惧簥鎴愬姛 source=%s target=%s", CompactLogText(url, 160), CompactLogText(transferred, 160))
	return strings.TrimSpace(transferred)
}

func normalizePosterURLForPixhost(url string, cfg PixhostUploadConfig) string {
	lower := strings.ToLower(url)
	if strings.Contains(lower, "pixhost.to") || strings.Contains(lower, "pixhost.cc") {
		if resolved, err := ResolvePixhostImageURLWithConfig(url, cfg); err == nil && strings.TrimSpace(resolved) != "" {
			logx.Infof(posterTransferLogModule, "娴锋姤URL宸叉槸Pixhost锛岀洿閾捐В鏋愭垚鍔?source=%s resolved=%s", CompactLogText(url, 160), CompactLogText(resolved, 160))
			return strings.TrimSpace(resolved)
		} else if err != nil {
			logx.Warnf(posterTransferLogModule, "娴锋姤URL宸叉槸Pixhost浣嗙洿閾捐В鏋愬け璐?source=%s err=%v", CompactLogText(url, 160), err)
		}
		if direct := NormalizePixhostDirectHostWithConfig(url, cfg); direct != "" {
			logx.Infof(posterTransferLogModule, "娴锋姤URL宸叉槸Pixhost锛屽煙鍚嶈鑼冨寲瀹屾垚 source=%s normalized=%s", CompactLogText(url, 160), CompactLogText(direct, 160))
			return direct
		}
		logx.Warnf(posterTransferLogModule, "娴锋姤URL宸叉槸Pixhost浣嗘棤娉曡鑼冨寲锛屼繚鐣欏師濮婾RL source=%s", CompactLogText(url, 160))
		return url
	}

	transferred, err := TransferRemoteImageToPixhostWithConfig(url, cfg)
	if err != nil || strings.TrimSpace(transferred) == "" {
		if err != nil {
			logx.Warnf(posterTransferLogModule, "娴锋姤杞瓨Pixhost澶辫触锛屽洖閫€鍘熷URL source=%s err=%v", CompactLogText(url, 160), err)
		} else {
			logx.Warnf(posterTransferLogModule, "娴锋姤杞瓨Pixhost澶辫触锛屽洖閫€鍘熷URL source=%s err=empty transfer result", CompactLogText(url, 160))
		}
		return url
	}
	logx.Infof(posterTransferLogModule, "娴锋姤杞瓨Pixhost鎴愬姛 source=%s target=%s", CompactLogText(url, 160), CompactLogText(transferred, 160))
	return strings.TrimSpace(transferred)
}

// TransferRemoteImageToPixhost 灏嗚繙绋嬪浘鐗囦笅杞藉悗涓婁紶鍒?Pixhost锛屽啀杩斿洖鍙敤鐩撮摼銆?
func TransferRemoteImageToPixhost(imageURL string) (string, error) {
	return TransferRemoteImageToPixhostWithConfig(imageURL, DefaultPixhostUploadConfig())
}

// TransferRemoteImageToPixhostWithConfig 灏嗚繙绋嬪浘鐗囦笅杞藉悗涓婁紶鍒?Pixhost锛屽苟鎸夐厤缃煙鍚嶈繑鍥炵洿閾俱€?// 鍙傛暟/杩斿洖锛歩mageURL 涓鸿繙绋嬪浘鐗囧湴鍧€锛沜fg 鎸囧畾涓婁紶 API 涓庣洿閾惧煙鍚嶏紱鎴愬姛杩斿洖鍥剧墖鐩撮摼銆?// 澶辫触鍦烘櫙锛氫笅杞藉け璐ャ€佷复鏃舵枃浠跺啓鍏ュけ璐ャ€丳ixhost 涓婁紶澶辫触鎴栬繑鍥炵┖閾炬帴銆?// 鍓綔鐢細鍙戣捣 HTTP 璇锋眰銆佸啓鍏ュ苟鍒犻櫎涓存椂鏂囦欢锛屽苟杈撳嚭杞瓨鏃ュ織銆?
func TransferRemoteImageToPixhostWithConfig(imageURL string, cfg PixhostUploadConfig) (string, error) {
	cfg = normalizePixhostUploadConfig(cfg)
	trimmed := strings.TrimSpace(imageURL)
	if trimmed == "" {
		return "", fmt.Errorf("empty image url")
	}

	candidates := buildPosterDownloadCandidates(trimmed)
	logx.Infof(posterTransferLogModule, "寮€濮嬫捣鎶ヨ浆瀛?source=%s candidates=%d", CompactLogText(trimmed, 160), len(candidates))
	errMsgs := make([]string, 0, len(candidates)*posterTransferDownloadRetry)

	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		for attempt := 1; attempt <= posterTransferDownloadRetry; attempt++ {
			data, contentType, downloadErr := downloadPosterImage(candidate)
			if downloadErr != nil {
				errMsgs = append(errMsgs, fmt.Sprintf("涓嬭浇澶辫触 candidate=%s attempt=%d err=%v", CompactLogText(candidate, 120), attempt, downloadErr))
				logx.Warnf(posterTransferLogModule, "涓嬭浇娴锋姤澶辫触 candidate=%s attempt=%d/%d err=%v", CompactLogText(candidate, 160), attempt, posterTransferDownloadRetry, downloadErr)
				if attempt < posterTransferDownloadRetry {
					time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
				}
				continue
			}

			logx.Infof(
				posterTransferLogModule,
				"涓嬭浇娴锋姤鎴愬姛 candidate=%s attempt=%d bytes=%d content_type=%s",
				CompactLogText(candidate, 160),
				attempt,
				len(data),
				contentType,
			)

			tmpPath, tmpErr := writePosterTempFile(data, contentType)
			if tmpErr != nil {
				errMsgs = append(errMsgs, fmt.Sprintf("钀界洏澶辫触 candidate=%s err=%v", CompactLogText(candidate, 120), tmpErr))
				logx.Warnf(posterTransferLogModule, "娴锋姤涓存椂鏂囦欢鍐欏叆澶辫触 candidate=%s err=%v", CompactLogText(candidate, 160), tmpErr)
				continue
			}

			showURL, uploadErr := UploadImageToPixhostWithConfig(tmpPath, cfg)
			_ = os.Remove(tmpPath)
			if uploadErr != nil {
				errMsgs = append(errMsgs, fmt.Sprintf("涓婁紶澶辫触 candidate=%s attempt=%d err=%v", CompactLogText(candidate, 120), attempt, uploadErr))
				logx.Warnf(posterTransferLogModule, "涓婁紶娴锋姤鍒癙ixhost澶辫触 candidate=%s attempt=%d/%d err=%v", CompactLogText(candidate, 160), attempt, posterTransferDownloadRetry, uploadErr)
				if attempt < posterTransferDownloadRetry {
					time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
				}
				continue
			}

			if resolved, resolveErr := ResolvePixhostImageURLWithConfig(showURL, cfg); resolveErr == nil && strings.TrimSpace(resolved) != "" {
				logx.Infof(posterTransferLogModule, "Pixhost鐩撮摼瑙ｆ瀽鎴愬姛 show_url=%s resolved=%s", CompactLogText(showURL, 160), CompactLogText(resolved, 160))
				return strings.TrimSpace(resolved), nil
			} else if resolveErr != nil {
				logx.Warnf(posterTransferLogModule, "Pixhost鐩撮摼瑙ｆ瀽澶辫触 show_url=%s err=%v", CompactLogText(showURL, 160), resolveErr)
			}

			if direct := NormalizePixhostDirectHostWithConfig(showURL, cfg); direct != "" {
				logx.Infof(posterTransferLogModule, "Pixhost鐩撮摼瑙ｆ瀽鍥為€€鎴愬姛 show_url=%s direct=%s", CompactLogText(showURL, 160), CompactLogText(direct, 160))
				return direct, nil
			}
			base := strings.TrimSpace(showURL)
			if base == "" {
				errMsgs = append(errMsgs, fmt.Sprintf("Pixhost杩斿洖绌篣RL candidate=%s", CompactLogText(candidate, 120)))
				logx.Warnf(posterTransferLogModule, "Pixhost涓婁紶杩斿洖绌篣RL candidate=%s", CompactLogText(candidate, 160))
				if attempt < posterTransferDownloadRetry {
					time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
				}
				continue
			}
			logx.Warnf(posterTransferLogModule, "Pixhost杩斿洖show_url浣嗘湭瑙ｆ瀽涓虹洿閾撅紝鍥為€€show_url show_url=%s", CompactLogText(base, 160))
			return base, nil
		}
	}

	if len(errMsgs) == 0 {
		return "", fmt.Errorf("poster transfer failed without details")
	}
	return "", fmt.Errorf(strings.Join(errMsgs, " | "))
}

func buildPosterDownloadCandidates(imageURL string) []string {
	trimmed := strings.TrimSpace(imageURL)
	if trimmed == "" {
		return []string{}
	}

	candidates := make([]string, 0, 1+len(posterTransferProxyPrefixes))
	seen := map[string]struct{}{}
	appendCandidate := func(url string) {
		item := strings.TrimSpace(url)
		if item == "" {
			return
		}
		if _, exists := seen[item]; exists {
			return
		}
		seen[item] = struct{}{}
		candidates = append(candidates, item)
	}

	appendCandidate(trimmed)
	if isPosterURLProxyWrapped(trimmed) {
		return candidates
	}
	for _, prefix := range posterTransferProxyPrefixes {
		proxyURL := makeProxyWrappedPosterURL(prefix, trimmed)
		appendCandidate(proxyURL)
	}
	return candidates
}

func isPosterURLProxyWrapped(targetURL string) bool {
	trimmed := strings.TrimSpace(targetURL)
	if trimmed == "" {
		return false
	}
	for _, prefix := range posterTransferProxyPrefixes {
		p := strings.TrimSpace(prefix)
		if p == "" {
			continue
		}
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

func makeProxyWrappedPosterURL(prefix, targetURL string) string {
	p := strings.TrimSpace(prefix)
	t := strings.TrimSpace(targetURL)
	if p == "" || t == "" {
		return ""
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	if strings.HasPrefix(t, p) {
		return t
	}
	return p + t
}

func downloadPosterImage(target string) ([]byte, string, error) {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return nil, "", fmt.Errorf("empty download url")
	}

	req, err := http.NewRequest(http.MethodGet, trimmed, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("download poster http %d", resp.StatusCode)
	}

	reader := io.LimitReader(resp.Body, maxPosterBytes+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("empty poster content")
	}
	if len(data) > maxPosterBytes {
		return nil, "", fmt.Errorf("poster too large")
	}

	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if semi := strings.Index(contentType, ";"); semi >= 0 {
		contentType = strings.TrimSpace(contentType[:semi])
	}
	if contentType == "" {
		contentType = strings.ToLower(strings.TrimSpace(http.DetectContentType(data)))
	}
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", fmt.Errorf("content-type not image: %s", contentType)
	}
	return data, contentType, nil
}

func writePosterTempFile(data []byte, contentType string) (string, error) {
	ext := ".jpg"
	if extensions, extErr := mime.ExtensionsByType(contentType); extErr == nil && len(extensions) > 0 {
		ext = extensions[0]
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}

	tmpFile, err := os.CreateTemp("", "ptnexus-poster-*"+ext)
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

// UploadImageToPixhost 涓婁紶鏈湴鍥剧墖鍒?Pixhost锛岃繑鍥?show_url銆?
func UploadImageToPixhost(imagePath string) (string, error) {
	return UploadImageToPixhostWithConfig(imagePath, DefaultPixhostUploadConfig())
}

// UploadImageToPixhostWithConfig 涓婁紶鏈湴鍥剧墖鍒?Pixhost锛屾敮鎸佷娇鐢ㄩ厤缃殑 API 鍩熷悕銆?// 鍙傛暟/杩斿洖锛歩magePath 涓烘湰鍦板浘鐗囪矾寰勶紱cfg 鎸囧畾 Pixhost 涓婁紶 API锛涙垚鍔熻繑鍥?show_url銆?// 澶辫触鍦烘櫙锛氭枃浠朵笉瀛樺湪銆佺綉缁滃け璐ャ€丳ixhost 杩斿洖闈?200 鎴栧搷搴旂己灏?show_url銆?// 鍓綔鐢細璇诲彇鏈湴鏂囦欢骞跺彂璧?HTTP 涓婁紶璇锋眰銆?
func UploadImageToPixhostWithConfig(imagePath string, cfg PixhostUploadConfig) (string, error) {
	cfg = normalizePixhostUploadConfig(cfg)
	showURL, _, err := uploadToPixhostDirectStream(imagePath, cfg.UploadAPIURL, func(string, ...any) {})
	return showURL, err
}

// UploadImageToPixhostNarrative 鎸?Python 鐗堟棩蹇楅鏍间笂浼犲浘鐗囧埌 Pixhost锛屾敮鎸佷富澶囧煙鍚嶅垏鎹€?
// 鍙傛暟/杩斿洖锛歩magePath 涓烘湰鍦板浘鐗囪矾寰勶紱鎴愬姛杩斿洖 show_url锛涘け璐ヨ繑鍥為敊璇€?
// 澶辫触鍦烘櫙锛氭枃浠朵笉瀛樺湪銆佺綉缁滈敊璇€丳ixhost 闈?200銆佸搷搴?JSON 涓嶅悎娉曘€?
// 鍓綔鐢細璇诲彇鏈湴鏂囦欢骞跺彂璧?HTTP 璇锋眰锛涗細杈撳嚭鍙欎簨寮忕函鏂囨湰鏃ュ織銆?
func UploadImageToPixhostNarrative(imagePath string) (string, error) {
	return UploadImageToPixhostNarrativeWithLogger(imagePath, logx.PlainInfof)
}

// UploadImageToPixhostNarrativeWithLogger 鎸?Python 鐗堟棩蹇楅鏍间笂浼犲浘鐗囧埌 Pixhost锛屾敮鎸佷富澶囧煙鍚嶅垏鎹€?// 鍙傛暟/杩斿洖锛歭ogLine 鐢ㄤ簬杈撳嚭鍗曡鏃ュ織锛堝彲鐢ㄤ簬骞跺彂鍦烘櫙涓嬬殑鏃ュ織缂撳啿锛夈€?
func UploadImageToPixhostNarrativeWithLogger(imagePath string, logLine func(string, ...any)) (string, error) {
	return UploadImageToPixhostNarrativeWithConfigAndLogger(imagePath, DefaultPixhostUploadConfig(), logLine)
}

// UploadImageToPixhostNarrativeWithConfigAndLogger 鎸夐厤缃煙鍚嶄笂浼犲浘鐗囧埌 Pixhost锛屽苟杈撳嚭鍙欎簨寮忔棩蹇椼€?// 鍙傛暟/杩斿洖锛歝fg 鎸囧畾涓讳笂浼?API锛沴ogLine 鐢ㄤ簬杈撳嚭涓婁紶杩囩▼锛涙垚鍔熻繑鍥?show_url銆?// 澶辫触鍦烘櫙锛氭湰鍦版枃浠朵笉鍙銆佸叏閮?API 鍩熷悕璇锋眰澶辫触鎴?Pixhost 鍝嶅簲闈炴硶銆?// 鍓綔鐢細璇诲彇鏈湴鍥剧墖骞跺涓诲 API 鍙戣捣 HTTP 璇锋眰銆?
func UploadImageToPixhostNarrativeWithConfigAndLogger(imagePath string, cfg PixhostUploadConfig, logLine func(string, ...any)) (string, error) {
	cfg = normalizePixhostUploadConfig(cfg)
	apiURLs := []string{
		cfg.UploadAPIURL,
		"http://pt-nexus-proxy.sqing33.dpdns.org/" + cfg.UploadAPIURL,
		"http://pt-nexus-proxy.1395251710.workers.dev/" + cfg.UploadAPIURL,
	}

	logLine("鍑嗗涓婁紶鍥剧墖: %s", imagePath)
	if _, err := os.Stat(imagePath); err != nil {
		logLine("閿欒锛氭枃浠朵笉瀛樺湪 %s", imagePath)
		return "", err
	}

	maxRetries := 3
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		for i, apiURL := range apiURLs {
			domainName := "主域名"
			if i != 0 {
				domainName = "备用域名"
			}
			logLine("灏濊瘯浣跨敤%s: %s", domainName, apiURL)
			showURL, statusCode, err := uploadToPixhostDirectStream(imagePath, apiURL, logLine)
			if err == nil && strings.TrimSpace(showURL) != "" {
				logLine("%s涓婁紶鎴愬姛", domainName)
				return showURL, nil
			}
			lastErr = err
			if statusCode > 0 {
				logLine("   鉂?鐩存帴涓婁紶澶辫触 (鐘舵€佺爜: %d)", statusCode)
			} else if err != nil {
				logLine("   鉂?鐩存帴涓婁紶澶辫触: %s", classifyPixhostUploadError(err))
			} else {
				logLine("   鉂?鐩存帴涓婁紶澶辫触")
			}
			logLine("%s上传失败，尝试下一个", domainName)
		}
		if attempt < maxRetries {
			time.Sleep(2 * time.Second)
		}
	}

	logLine("所有API域名都上传失败")
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("Pixhost 涓婁紶澶辫触")
}

func uploadToPixhostDirectStream(imagePath string, apiURL string, logLine func(string, ...any)) (string, int, error) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	var (
		writeErr error
		once     sync.Once
	)
	closeWithErr := func(err error) {
		once.Do(func() {
			writeErr = err
			_ = pw.CloseWithError(err)
		})
	}

	go func() {
		defer func() {
			if writeErr == nil {
				_ = pw.Close()
			}
		}()
		defer writer.Close()

		file, err := os.Open(imagePath)
		if err != nil {
			closeWithErr(err)
			return
		}
		defer file.Close()

		part, err := writer.CreateFormFile("img", filepath.Base(imagePath))
		if err != nil {
			closeWithErr(err)
			return
		}
		if _, err := io.Copy(part, file); err != nil {
			closeWithErr(err)
			return
		}
		if err := writer.WriteField("content_type", "0"); err != nil {
			closeWithErr(err)
			return
		}
	}()

	logLine("姝ｅ湪鍙戦€佷笂浼犺姹傚埌 Pixhost...")
	req, err := http.NewRequest(http.MethodPost, apiURL, pr)
	if err != nil {
		_ = pr.Close()
		return "", 0, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		_ = pr.Close()
		if writeErr != nil {
			return "", 0, writeErr
		}
		return "", 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode, fmt.Errorf("Pixhost HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	parsed := map[string]any{}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", resp.StatusCode, fmt.Errorf("Pixhost 鍝嶅簲瑙ｆ瀽澶辫触: %w body=%s", err, CompactLogText(string(respBody), 240))
	}
	showURL := strings.TrimSpace(toStringAny(parsed["show_url"], ""))
	if showURL == "" {
		if dataMap, ok := parsed["data"].(map[string]any); ok {
			showURL = strings.TrimSpace(toStringAny(dataMap["show_url"], ""))
		}
	}
	if showURL == "" {
		return "", resp.StatusCode, fmt.Errorf("Pixhost 鏈繑鍥?show_url")
	}

	logLine("鐩存帴涓婁紶鎴愬姛锛佸浘鐗囬摼鎺? %s", showURL)
	return showURL, resp.StatusCode, nil
}

func classifyPixhostUploadError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(text, "x509") || strings.Contains(text, "tls") || strings.Contains(text, "certificate"):
		return "SSL杩炴帴閿欒"
	case strings.Contains(text, "timeout") || strings.Contains(text, "i/o timeout") || strings.Contains(text, "context deadline"):
		return "璇锋眰瓒呮椂"
	case strings.Contains(text, "connection reset") || strings.Contains(text, "broken pipe") || strings.Contains(text, "connection refused"):
		return "网络连接被重置"
	default:
		return "缃戠粶璇锋眰澶辫触"
	}
}

// ResolvePixhostImageURL 灏?show_url 瑙ｆ瀽涓哄彲璁块棶鐨勭洿閾?URL銆?
func ResolvePixhostImageURL(showURL string) (string, error) {
	return ResolvePixhostImageURLWithConfig(showURL, DefaultPixhostUploadConfig())
}

// ResolvePixhostImageURLWithConfig 灏?show_url 瑙ｆ瀽涓洪厤缃煙鍚嶄笅鐨勫彲璁块棶鐩撮摼 URL銆?// 鍙傛暟/杩斿洖锛歴howURL 涓?Pixhost show/th 鎴栫洿閾惧湴鍧€锛沜fg 鎸囧畾杈撳嚭鐩撮摼鍩熷悕銆?// 澶辫触鍦烘櫙锛氱┖ URL銆侀〉闈㈡姄鍙栧け璐ユ垨鎺ㄥ鐩撮摼涓嶅彲璁块棶銆?// 鍓綔鐢細鍙兘鍙戣捣 HEAD/GET 璇锋眰鏍￠獙鍥剧墖鍙闂€с€?
func ResolvePixhostImageURLWithConfig(showURL string, cfg PixhostUploadConfig) (string, error) {
	cfg = normalizePixhostUploadConfig(cfg)
	trimmed := strings.TrimSpace(showURL)
	if trimmed == "" {
		return "", fmt.Errorf("empty show_url")
	}

	direct := PixhostShowToDirectURLWithConfig(trimmed, cfg)
	if direct != "" && IsImageURLReachable(direct) {
		return direct, nil
	}

	body, err := FetchPageWithTimeout(trimmed)
	if err == nil && strings.TrimSpace(body) != "" {
		for _, re := range []*regexp.Regexp{rePixhostOgImage, rePixhostImageTag} {
			match := re.FindStringSubmatch(body)
			if len(match) < 2 {
				continue
			}
			candidate := AbsolutizeURL(trimmed, strings.TrimSpace(match[1]))
			if candidate == "" {
				continue
			}
			if isPixhostShowOrThumbURL(candidate) {
				candidate = PixhostShowToDirectURLWithConfig(candidate, cfg)
			}
			candidate = NormalizePixhostDirectHostWithConfig(candidate, cfg)
			if candidate != "" && IsImageURLReachable(candidate) {
				return candidate, nil
			}
		}
	}

	if direct != "" {
		return direct, fmt.Errorf("鐩撮摼鍙敤鎬ф牎楠屽け璐ワ紝杩斿洖鎺ㄥ鐩撮摼")
	}
	return trimmed, fmt.Errorf("鏃犳硶瑙ｆ瀽 pixhost 鐩撮摼锛岃繑鍥?show_url")
}

// PixhostShowToDirectURL 灏濊瘯灏?pixhost show/th 椤甸潰鍦板潃杞崲涓哄浘鐗囩洿閾俱€?
func PixhostShowToDirectURL(showURL string) string {
	return PixhostShowToDirectURLWithConfig(showURL, DefaultPixhostUploadConfig())
}

// PixhostShowToDirectURLWithConfig 灏濊瘯灏?pixhost show/th 鍦板潃杞崲涓洪厤缃煙鍚嶄笅鐨勫浘鐗囩洿閾俱€?// 鍙傛暟/杩斿洖锛歴howURL 涓?Pixhost 椤甸潰鎴栫洿閾撅紱cfg 鎸囧畾杈撳嚭鐩撮摼鍩熷悕锛涙棤娉曡В鏋愭椂杩斿洖绌哄瓧绗︿覆銆?// 澶辫触鍦烘櫙锛氱┖ URL銆乁RL 缁撴瀯涓嶆槸 Pixhost 鍥剧墖鍦板潃鎴栨棤娉曟彁鍙栧浘鐗囪矾寰勩€?// 鍓綔鐢細鏃犮€?
func PixhostShowToDirectURLWithConfig(showURL string, cfg PixhostUploadConfig) string {
	cfg = normalizePixhostUploadConfig(cfg)
	trimmed := strings.TrimSpace(showURL)
	if trimmed == "" {
		return ""
	}

	direct := trimmed
	if parsed, err := neturl.Parse(trimmed); err == nil && parsed != nil {
		host := strings.ToLower(strings.TrimSpace(parsed.Host))
		path := strings.TrimSpace(parsed.Path)
		for _, prefix := range []string{"/show/", "/th/"} {
			if (host == "pixhost.to" || host == "pixhost.cc") && strings.HasPrefix(path, prefix) {
				parsed.Scheme = "https"
				parsed.Host = cfg.DirectHost
				parsed.Path = "/images/" + strings.TrimPrefix(path, prefix)
				parsed.RawQuery = ""
				parsed.Fragment = ""
				direct = parsed.String()
				break
			}
		}
	}
	direct = rePixhostThumbSuffix.ReplaceAllString(direct, `.$1`)

	if rePixhostDirectURL.MatchString(direct) {
		return NormalizePixhostDirectHostWithConfig(direct, cfg)
	}
	if match := rePixhostDirect.FindStringSubmatch(direct); len(match) >= 3 {
		candidate := fmt.Sprintf("https://%s/images/%s/%s", cfg.DirectHost, match[1], match[2])
		if rePixhostDirectURL.MatchString(candidate) {
			return candidate
		}
	}
	if match := rePixhostDirect.FindStringSubmatch(trimmed); len(match) >= 3 {
		candidate := fmt.Sprintf("https://%s/images/%s/%s", cfg.DirectHost, match[1], match[2])
		if rePixhostDirectURL.MatchString(candidate) {
			return candidate
		}
	}
	return ""
}

// NormalizePixhostDirectHost 瑙勮寖鍖?Pixhost 鐩撮摼鍩熷悕鍒?img*.pixhost.to銆?
func NormalizePixhostDirectHost(value string) string {
	return NormalizePixhostDirectHostWithConfig(value, DefaultPixhostUploadConfig())
}

// NormalizePixhostDirectHostWithConfig 瑙勮寖鍖?Pixhost 鐩撮摼鍩熷悕鍒伴厤缃殑 img*.pixhost.* 鍩熷悕銆?// 鍙傛暟/杩斿洖锛歷alue 涓哄緟淇 URL锛沜fg 鎸囧畾杈撳嚭鐩撮摼鍩熷悕锛涙棤娉曡瘑鍒椂杩斿洖绌哄瓧绗︿覆銆?// 澶辫触鍦烘櫙锛歎RL 涓虹┖銆佹棤娉曡В鏋愭垨涓嶅寘鍚?Pixhost 鍥剧墖璺緞銆?// 鍓綔鐢細鏃犮€?
func NormalizePixhostDirectHostWithConfig(value string, cfg PixhostUploadConfig) string {
	cfg = normalizePixhostUploadConfig(cfg)
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	parsed, err := neturl.Parse(trimmed)
	if err != nil || parsed == nil {
		return ""
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Host))
	if strings.HasPrefix(host, "img") && (strings.Contains(host, ".pixhost.to") || strings.Contains(host, ".pixhost.cc")) && strings.Contains(parsed.Path, "/images/") {
		parsed.Host = cfg.DirectHost
		parsed.Scheme = "https"
		return parsed.String()
	}
	if (strings.Contains(host, "pixhost.to") || strings.Contains(host, "pixhost.cc")) && strings.Contains(parsed.Path, "/images/") {
		parsed.Host = cfg.DirectHost
		parsed.Scheme = "https"
		return parsed.String()
	}
	if match := rePixhostDirect.FindStringSubmatch(trimmed); len(match) >= 3 {
		return fmt.Sprintf("https://%s/images/%s/%s", cfg.DirectHost, match[1], match[2])
	}
	return ""
}

// DefaultPixhostUploadConfig 杩斿洖鍏煎鏃ч厤缃殑 Pixhost 榛樿涓婁紶閰嶇疆銆?
func DefaultPixhostUploadConfig() PixhostUploadConfig {
	return PixhostUploadConfig{
		DirectHost:   pixhostOutputDirectHost,
		UploadAPIURL: pixhostUploadAPIURL,
	}
}

// GetPixhostUploadConfigFromRootConfig 浠?rootConfig 鐨?cross_seed 鑺傛彁鍙?Pixhost 鍩熷悕閰嶇疆銆?
func GetPixhostUploadConfigFromRootConfig(rootConfig map[string]any) PixhostUploadConfig {
	if rootConfig == nil {
		return DefaultPixhostUploadConfig()
	}
	cs, ok := rootConfig["cross_seed"].(map[string]any)
	if !ok {
		return DefaultPixhostUploadConfig()
	}
	return NewPixhostUploadConfig(toStringAny(cs["pixhost_domain"], ""))
}

// NewPixhostUploadConfig 鏍规嵁鐢ㄦ埛濉啓鐨?Pixhost 鍩熷悕鐢熸垚涓婁紶 API 涓庣洿閾惧煙鍚嶃€?
func NewPixhostUploadConfig(domain string) PixhostUploadConfig {
	directHost := normalizePixhostDirectDomain(domain)
	return PixhostUploadConfig{
		DirectHost:   directHost,
		UploadAPIURL: buildPixhostUploadAPIURL(directHost),
	}
}

func normalizePixhostUploadConfig(cfg PixhostUploadConfig) PixhostUploadConfig {
	cfg.DirectHost = normalizePixhostDirectDomain(cfg.DirectHost)
	apiURL := strings.TrimSpace(cfg.UploadAPIURL)
	if apiURL == "" {
		apiURL = buildPixhostUploadAPIURL(cfg.DirectHost)
	}
	cfg.UploadAPIURL = apiURL
	return cfg
}

func normalizePixhostDirectDomain(domain string) string {
	trimmed := strings.TrimSpace(domain)
	if trimmed == "" {
		return pixhostOutputDirectHost
	}
	if parsed, err := neturl.Parse(trimmed); err == nil && parsed != nil && parsed.Host != "" {
		trimmed = parsed.Host
	}
	if i := strings.IndexAny(trimmed, "/?#"); i >= 0 {
		trimmed = trimmed[:i]
	}
	host := strings.ToLower(strings.TrimSpace(trimmed))
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	host = strings.Trim(host, ". ")
	if host == "" {
		return pixhostOutputDirectHost
	}
	if strings.HasPrefix(host, "api.") {
		host = "img2." + strings.TrimPrefix(host, "api.")
	}
	if host == "pixhost.to" || host == "pixhost.cc" {
		host = "img2." + host
	}
	return host
}

func buildPixhostUploadAPIURL(directHost string) string {
	host := normalizePixhostDirectDomain(directHost)
	if strings.HasPrefix(host, "api.") {
		return "https://" + host + "/images"
	}
	if strings.HasPrefix(host, "img") {
		if dot := strings.Index(host, "."); dot >= 0 && dot+1 < len(host) {
			root := host[dot+1:]
			if root == "pixhost.to" || root == "pixhost.cc" {
				return "https://api." + root + "/images"
			}
		}
	}
	if host == "pixhost.to" || host == "pixhost.cc" {
		return "https://api." + host + "/images"
	}
	return pixhostUploadAPIURL
}

func isPixhostShowOrThumbURL(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(lower, "pixhost.to/show/") ||
		strings.Contains(lower, "pixhost.to/th/") ||
		strings.Contains(lower, "pixhost.cc/show/") ||
		strings.Contains(lower, "pixhost.cc/th/")
}

// IsImageURLReachable 閫氳繃 HEAD/GET 鎺㈡祴 URL 鏄惁鍙闂笖鍐呭绫诲瀷涓哄浘鐗囥€?
func IsImageURLReachable(target string) bool {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return false
	}
	client := &http.Client{Timeout: 12 * time.Second}

	headReq, err := http.NewRequest(http.MethodHead, trimmed, nil)
	if err == nil {
		headReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		if resp, headErr := client.Do(headReq); headErr == nil {
			ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 && strings.HasPrefix(ct, "image/") {
				return true
			}
		}
	}

	getReq, err := http.NewRequest(http.MethodGet, trimmed, nil)
	if err != nil {
		return false
	}
	getReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	getReq.Header.Set("Range", "bytes=0-32")
	resp, err := client.Do(getReq)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	return resp.StatusCode >= 200 && resp.StatusCode < 400 && strings.HasPrefix(ct, "image/")
}
