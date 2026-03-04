package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DEFAULT_UPDATER_PORT = "5274"
	DEFAULT_SERVER_PORT  = "5275"
	DEFAULT_BATCH_PORT   = "5276"
	DEFAULT_UPDATE_DIR   = "/app/data/updates"
)

var (
	updaterPort       = getEnv("UPDATER_PORT", getEnv("PORT", DEFAULT_UPDATER_PORT))
	serverPort        = getEnv("SERVER_PORT", DEFAULT_SERVER_PORT)
	batchEnhancerPort = getEnv(
		"BATCH_PORT",
		getEnv("BATCH_ENHANCER_PORT", DEFAULT_BATCH_PORT),
	)
	updateDir = getEnv("UPDATE_DIR", DEFAULT_UPDATE_DIR)

	localConfigFile string
	shanghaiLoc     *time.Location
	// 新增：互斥锁防止重复触发更新
	updateMutex      sync.Mutex
	isSystemUpdating bool
)

func init() {
	if os.Getenv("DEV_ENV") == "true" {
		// 开发环境
		localConfigFile = getEnv("LOCAL_CONFIG_FILE", "/home/sqing/Codes/PTNexus/CHANGELOG.json")
	} else {
		// 生产环境
		localConfigFile = getEnv("LOCAL_CONFIG_FILE", "/app/CHANGELOG.json")
	}

	// 初始化时区
	initTimezone()
}

// 初始化时区和定时配置
func initTimezone() {
	var err error
	shanghaiLoc, err = time.LoadLocation("Asia/Shanghai")
	if err != nil {
		log.Printf("警告: 无法加载上海时区: %v，使用UTC时区", err)
		shanghaiLoc = time.UTC
	}
	log.Printf("时区初始化完成: %s", shanghaiLoc.String())

	// 从环境变量读取定时配置
	if getEnv("SCHEDULE_ENABLED", "true") == "false" {
		globalScheduleConfig.Enabled = false
	}
	if scheduleTime := getEnv("SCHEDULE_TIME", "06:00"); scheduleTime != "06:00" {
		globalScheduleConfig.Time = scheduleTime
	}
	if scheduleTimezone := getEnv("SCHEDULE_TIMEZONE", "Asia/Shanghai"); scheduleTimezone != "Asia/Shanghai" {
		globalScheduleConfig.Timezone = scheduleTimezone
		// 重新加载时区
		if loc, err := time.LoadLocation(scheduleTimezone); err == nil {
			shanghaiLoc = loc
			log.Printf("使用自定义时区: %s", scheduleTimezone)
		}
	}

	log.Printf("定时更新配置: enabled=%v, time=%s, timezone=%s",
		globalScheduleConfig.Enabled, globalScheduleConfig.Time, globalScheduleConfig.Timezone)
}

// 获取下一个早上6点的时间
func getNextSixAM() time.Time {
	now := time.Now().In(shanghaiLoc)
	next := time.Date(now.Year(), now.Month(), now.Day(), 6, 0, 0, 0, shanghaiLoc)
	if next.Before(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

// 解析时间字符串为时分
func parseTime(timeStr string) (hour, min int, err error) {
	parts := strings.Split(timeStr, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("时间格式错误，期望 HH:MM，得到: %s", timeStr)
	}

	hour, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("小时解析失败: %v", err)
	}

	min, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("分钟解析失败: %v", err)
	}

	if hour < 0 || hour > 23 || min < 0 || min > 59 {
		return 0, 0, fmt.Errorf("时间值超出范围: %d:%d", hour, min)
	}

	return hour, min, nil
}

// 获取下一个指定时间
func getNextScheduledTime(timeStr string) (time.Time, error) {
	hour, min, err := parseTime(timeStr)
	if err != nil {
		return time.Time{}, err
	}

	now := time.Now().In(shanghaiLoc)
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, shanghaiLoc)
	if next.Before(now) {
		next = next.Add(24 * time.Hour)
	}
	return next, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func ensureDir(path string) {
	_ = os.MkdirAll(path, 0755)
}

func withFileLock(lockPath string, fn func() error) error {
	ensureDir(filepath.Dir(lockPath))
	lockFile := lockPath + ".lock"
	deadline := time.Now().Add(30 * time.Second)

	for {
		lockHandle, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			_, _ = lockHandle.WriteString(strconv.FormatInt(time.Now().UnixNano(), 10))
			_ = lockHandle.Close()
			defer os.Remove(lockFile)
			return fn()
		}

		if errors.Is(err, os.ErrExist) {
			if time.Now().After(deadline) {
				return fmt.Errorf("acquire file lock timeout: %s", lockFile)
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}

		return err
	}
}

func getPipSyncMode() string {
	mode := strings.ToLower(strings.TrimSpace(getEnv("UPDATE_PIP_SYNC_MODE", "always")))
	switch mode {
	case "always", "changed", "never":
		return mode
	default:
		return "always"
	}
}

func syncPythonDepsAfterUpdate() error {
	// 只在“在线更新/热更新”场景执行：让更新完成后启动服务前把依赖补齐。
	if !isTruthy(getEnv("UPDATE_INSTALL_DEPS", "true")) {
		log.Println("依赖同步已关闭（UPDATE_INSTALL_DEPS=false），跳过依赖安装。")
		return nil
	}

	mode := getPipSyncMode()
	if mode == "never" {
		log.Println("依赖同步已关闭（UPDATE_PIP_SYNC_MODE=never），跳过 pip 依赖安装。")
		return nil
	}

	reqFile := getEnv("REQUIREMENTS_FILE", "/app/requirements.txt")
	if _, err := os.Stat(reqFile); err != nil {
		log.Printf("未找到 requirements 文件（%s），跳过 pip 依赖安装。", reqFile)
		return nil
	}

	bootstrapDir := "/app/data/.bootstrap"
	ensureDir(bootstrapDir)
	hashFile := filepath.Join(bootstrapDir, "requirements.sha256")

	newHash, err := fileSHA256(reqFile)
	if err != nil {
		log.Printf("计算 requirements 哈希失败，仍尝试执行 pip install（%v）", err)
	} else if mode == "changed" {
		oldHash, _ := os.ReadFile(hashFile)
		if strings.TrimSpace(string(oldHash)) == newHash {
			log.Println("requirements 未变化（UPDATE_PIP_SYNC_MODE=changed），跳过 pip 依赖安装。")
			return nil
		}
	}

	timeoutStr := getEnv("PIP_INSTALL_TIMEOUT", "10m")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil || timeout <= 0 {
		timeout = 10 * time.Minute
	}

	log.Printf("开始安装/更新 pip 依赖（mode=%s, timeout=%s）：%s", mode, timeout.String(), reqFile)

	return withFileLock(filepath.Join(bootstrapDir, "pip.lock"), func() error {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		py := "python3"
		if _, err := exec.LookPath(py); err != nil {
			py = "python"
		}

		args := []string{"-m", "pip", "install", "-r", reqFile}
		if isTruthy(getEnv("PIP_NO_CACHE", "false")) {
			args = append(args, "--no-cache-dir")
		}

		cmd := exec.CommandContext(ctx, py, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("pip install 失败: %w", err)
		}

		if newHash != "" {
			_ = os.WriteFile(hashFile, []byte(newHash), 0644)
		}
		log.Println("pip 依赖安装完成")
		return nil
	})
}

type UpdateConfig struct {
	History  []VersionInfo `json:"history"`
	Mappings []DirMapping  `json:"mappings"`
	Preserve []string      `json:"preserve"`
}

type VersionInfo struct {
	Version       string   `json:"version"`
	Date          string   `json:"date"`
	ForceUpdate   bool     `json:"force_update"`
	DisableUpdate bool     `json:"disable_update,omitempty"`
	Changes       []string `json:"changes"`
	Note          string   `json:"note,omitempty"`
}

// 定时更新配置 - 硬编码到Go代码中
type ScheduleConfig struct {
	Enabled  bool       `json:"enabled"`
	Timezone string     `json:"timezone"`
	Time     string     `json:"time"`
	LastRun  *time.Time `json:"last_run"`
}

// 全局定时配置
var globalScheduleConfig = ScheduleConfig{
	Enabled:  true, // 默认启用，可通过环境变量SCHEDULE_ENABLED禁用
	Timezone: "Asia/Shanghai",
	Time:     "06:00",
	LastRun:  nil,
}

type DirMapping struct {
	Source     string   `json:"source"`
	Target     string   `json:"target"`
	Exclude    []string `json:"exclude"`
	Executable bool     `json:"executable"`
}

// 加载定时配置（从全局变量）
func loadScheduleConfig() ScheduleConfig {
	return globalScheduleConfig
}

// 保存定时配置（更新全局变量）
func saveScheduleConfig(now time.Time) {
	// 更新全局配置中的最后执行时间
	globalScheduleConfig.LastRun = &now
	log.Printf("更新定时配置最后执行时间: %s", now.Format("2006-01-02 15:04:05"))
}

// 检查是否应该执行更新
func shouldRunUpdate(now time.Time, schedule ScheduleConfig) bool {
	if !schedule.Enabled {
		return false
	}

	// 解析定时时间
	hour, min, err := parseTime(schedule.Time)
	if err != nil {
		log.Printf("时间配置错误: %v", err)
		return false
	}

	// 检查当前时间是否匹配定时时间
	if now.Hour() != hour || now.Minute() != min {
		return false
	}

	// 检查今天是否已经执行过
	if schedule.LastRun != nil {
		lastRun := schedule.LastRun.In(shanghaiLoc)
		if lastRun.Year() == now.Year() &&
			lastRun.Month() == now.Month() &&
			lastRun.Day() == now.Day() {
			return false
		}
	}

	return true
}

// 更新最后执行时间
func updateLastRunTime(now time.Time) {
	saveScheduleConfig(now)
}

// 执行自动更新
func runAutoUpdate() {
	log.Println("开始执行自动更新检查...")

	localVersion := getLocalVersion()

	manifest, err := getRemoteManifest()
	if err != nil {
		log.Printf("自动更新失败：无法获取更新清单: %v", err)
		return
	}

	remoteVersion := strings.TrimSpace(manifest.Latest.Version)
	shouldForce := manifest.Latest.ForceUpdate
	shouldDisable := manifest.Latest.DisableUpdate

	if !isNewerVersion(remoteVersion, localVersion) {
		log.Printf("本地版本 %s 已是最新，无需更新", localVersion)
		return
	}

	log.Printf("检测到新版本: 本地 %s -> 远程 %s", localVersion, remoteVersion)

	// 条件：(全局定时更新开启) 或者 (远程标记为强制更新 ForceUpdate)
	if !globalScheduleConfig.Enabled && !shouldForce {
		log.Println("定时更新未启用，且非强制更新版本，跳过自动更新")
		return
	}

	if shouldDisable {
		log.Printf("版本 %s 标记为 disable_update，跳过自动更新", remoteVersion)
		return
	}

	if _, err := resolveManifestArtifactForCurrentPlatform(manifest); err != nil {
		log.Printf("更新清单不完整，跳过自动更新: %v", err)
		return
	}

	log.Printf("执行更新流程 (强制更新: %v)...", shouldForce)

	err = withInMemoryUpdateFlag(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
		defer cancel()

		prepared, err := prepareUpdateBundleFromManifest(ctx, manifest)
		if err != nil {
			return err
		}
		if strings.TrimSpace(prepared.StagingDir) == "" {
			return nil
		}
		return installPreparedBundle(ctx, prepared)
	})
	if err != nil {
		log.Printf("自动更新失败: %v", err)
		return
	}

	log.Printf("自动更新完成: %s", remoteVersion)
}

// 定时检查器
func startScheduledChecker() {
	log.Println("启动定时检查器...")
	ticker := time.NewTicker(1 * time.Minute) // 每分钟检查一次
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			checkAndRunScheduledUpdate()
		}
	}
}

// 检查并执行定时更新
func checkAndRunScheduledUpdate() {
	schedule := loadScheduleConfig()
	now := time.Now().In(shanghaiLoc)

	// 这里只判断时间是否到达，是否真正执行更新在 runAutoUpdate 内部判断
	// 这样可以实现：即使本地把 SCHEDULE_ENABLED 关了，但只要时间到了且远程有 ForceUpdate，依然可以更新

	// 解析定时时间
	hour, min, err := parseTime(schedule.Time)
	if err != nil {
		return
	}

	// 简单的分钟级匹配
	if now.Hour() == hour && now.Minute() == min {
		// 检查今天是否已经执行过
		if schedule.LastRun != nil {
			lastRun := schedule.LastRun.In(shanghaiLoc)
			if lastRun.Year() == now.Year() &&
				lastRun.Month() == now.Month() &&
				lastRun.Day() == now.Day() {
				return
			}
		}

		log.Printf("触发定时检查点 (时区: %s)", now.Format("2006-01-02 15:04:05"))
		// runAutoUpdate 内部会检查 Enabled 状态或 ForceUpdate 状态
		runAutoUpdate()
		updateLastRunTime(now)
	}
}

// 检查是否有跨版本强制更新
func hasCrossVersionForceUpdate(localVersion string, remoteConfig *UpdateConfig) bool {
	if len(remoteConfig.History) == 0 {
		return false
	}

	// 找到本地版本在历史中的位置
	localVersionIndex := -1
	for i, version := range remoteConfig.History {
		if version.Version == localVersion {
			localVersionIndex = i
			break
		}
	}

	// 如果找不到本地版本，检查从最新版本开始的所有版本
	if localVersionIndex == -1 {
		log.Printf("本地版本 %s 在历史记录中未找到，检查所有版本", localVersion)
		for _, version := range remoteConfig.History {
			if version.ForceUpdate {
				log.Printf("发现跨版本强制更新: %s", version.Version)
				return true
			}
		}
		return false
	}

	// 检查从本地版本之后的所有版本
	log.Printf("本地版本 %s 在历史记录中的位置: %d", localVersion, localVersionIndex)
	for i := localVersionIndex - 1; i >= 0; i-- {
		version := remoteConfig.History[i]
		if version.ForceUpdate {
			log.Printf("发现跨版本强制更新: %s (本地版本: %s)", version.Version, localVersion)
			return true
		}
	}

	return false
}

// 检查更新
func checkUpdateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// 禁止缓存
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	localVersion := getLocalVersion()

	// CHANGELOG.json：用于跨版本强制更新判断与更新日志展示。
	var remoteVersionFromChangelog string
	var forceUpdateCrossVersion bool
	var disableUpdateFromChangelog bool
	remoteConfig, err := getRemoteConfig()
	if err != nil {
		log.Printf("检查更新时获取远程配置失败: %v", err)
	} else if len(remoteConfig.History) > 0 {
		remoteVersionFromChangelog = remoteConfig.History[0].Version
		forceUpdateCrossVersion = hasCrossVersionForceUpdate(localVersion, remoteConfig)
		disableUpdateFromChangelog = remoteConfig.History[0].DisableUpdate
	}

	// UPDATE_MANIFEST.json：产物更新清单（不依赖 git）。
	var manifestVersion string
	var forceUpdateFromManifest bool
	var disableUpdateFromManifest bool
	manifestReady := false
	if manifest, err := getRemoteManifest(); err != nil {
		log.Printf("检查更新时获取更新清单失败: %v", err)
	} else if _, err := resolveManifestArtifactForCurrentPlatform(manifest); err != nil {
		log.Printf("检查更新时更新清单不完整，跳过产物更新入口: %v", err)
	} else {
		manifestReady = true
		manifestVersion = strings.TrimSpace(manifest.Latest.Version)
		forceUpdateFromManifest = manifest.Latest.ForceUpdate
		disableUpdateFromManifest = manifest.Latest.DisableUpdate
	}

	remoteVersion := remoteVersionFromChangelog
	if manifestReady && manifestVersion != "" {
		remoteVersion = manifestVersion
	} else if remoteVersion == "" {
		remoteVersion = manifestVersion
	}

	forceUpdate := forceUpdateCrossVersion || forceUpdateFromManifest
	disableUpdate := disableUpdateFromChangelog || disableUpdateFromManifest

	hasUpdate := false
	if remoteVersion != "" && localVersion != "" {
		hasUpdate = isNewerVersion(remoteVersion, localVersion)
	}

	// 无可用产物时禁用“在线更新”，避免 UI 出现“有更新但无法安装”的误导操作。
	if hasUpdate && !manifestReady {
		disableUpdate = true
	}

	if hasUpdate {
		log.Printf("检测到新版本: %s -> %s (Force: %v, Disable: %v)", localVersion, remoteVersion, forceUpdate, disableUpdate)
	}

	// 这里不再做任何自动更新的触发逻辑，只返回数据
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"has_update":     hasUpdate,
		"local_version":  localVersion,
		"remote_version": remoteVersion,
		"update_control": map[string]interface{}{
			"force_update":   forceUpdate,
			"disable_update": disableUpdate,
			"schedule":       loadScheduleConfig(),
		},
	})
}

// compareVersions 比较两个版本号
// 如果 remote > local 返回 true，否则返回 false
func isNewerVersion(remote, local string) bool {
	// 去除前缀 v 或 V，并去除空格
	remote = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(remote), "v"))
	local = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(local), "v"))

	// 按点分割
	remoteParts := strings.Split(remote, ".")
	localParts := strings.Split(local, ".")

	// 获取最大长度
	maxLen := len(remoteParts)
	if len(localParts) > maxLen {
		maxLen = len(localParts)
	}

	for i := 0; i < maxLen; i++ {
		rVal := 0
		lVal := 0

		// 解析远程版本当前位
		if i < len(remoteParts) {
			rVal, _ = strconv.Atoi(remoteParts[i])
		}

		// 解析本地版本当前位
		if i < len(localParts) {
			lVal, _ = strconv.Atoi(localParts[i])
		}

		// 逐位比较
		if rVal > lVal {
			return true // 远程版本更大
		}
		if rVal < lVal {
			return false // 本地版本更大或已确定不需要更新
		}
		// 如果相等，继续比较下一位
	}

	// 如果所有比较的位都相等，检查版本号长度
	// 例如：3.3.2 vs 3.3，3.3.2 更新
	if len(remoteParts) > len(localParts) {
		// 检查远程版本多出的位是否都是0
		for i := len(localParts); i < len(remoteParts); i++ {
			if val, _ := strconv.Atoi(remoteParts[i]); val != 0 {
				return true // 远程版本有非零的额外位，版本更新
			}
		}
		return false // 远程版本多出的位都是0，版本相同
	}

	return false // 版本完全相同或本地版本更长
}

// 拉取代码
func pullUpdateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 新链路：基于 UPDATE_MANIFEST.json 下载构建产物（artifact），完全不依赖 git。
	// 仍保留 /update/pull + /update/install 的交互语义，前端无需改动。
	err := withInMemoryUpdateFlag(func() error {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()

		prepared, err := prepareLatestUpdateBundle(ctx)
		if err != nil {
			return err
		}
		if strings.TrimSpace(prepared.StagingDir) == "" {
			// No update needed.
			return nil
		}
		return nil
	})
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "更新包下载完成",
	})
}

// 安装更新
func installUpdateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 新链路：安装已下载的构建产物（artifact），通过切换 /app/server -> UPDATE_DIR/current -> releases/<version>/server。
	// 具备校验、可回滚、健康检查能力，并且不依赖 git。
	version := strings.TrimSpace(r.URL.Query().Get("version"))
	if version == "" {
		v, err := getPreparedVersionFromDisk()
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
			return
		}
		version = v
	}

	prepared, err := readPreparedUpdate(version)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "未找到已下载的更新包，请先执行更新拉取",
		})
		return
	}

	log.Printf("开始安装更新: %s", prepared.Version)

	// 开发环境只做解压内容校验，避免误改本机路径。
	if os.Getenv("DEV_ENV") == "true" {
		serverDir := filepath.Join(prepared.StagingDir, "server")
		if st, err := os.Stat(serverDir); err != nil || !st.IsDir() {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "【开发环境】更新包内容不完整：缺少 server/",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("【开发环境】已完成更新包校验（未安装）版本: %s", prepared.Version),
		})
		return
	}

	err = withInMemoryUpdateFlag(func() error {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		return installPreparedBundle(ctx, prepared)
	})
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("成功更新到 %s", prepared.Version),
	})
}

// 获取本地版本
func getLocalVersion() string {
	data, err := os.ReadFile(localConfigFile)
	if err != nil {
		log.Printf("读取本地配置失败: %v", err)
		return "unknown"
	}

	var config UpdateConfig
	if err := json.Unmarshal(data, &config); err != nil {
		log.Printf("解析本地配置失败: %v", err)
		return "unknown"
	}

	return config.History[0].Version
}

// 新增辅助函数：获取远程配置结构体
func getRemoteConfig() (*UpdateConfig, error) {
	config, source, err := fetchJSONFromCandidates[UpdateConfig](context.Background(), changelogCandidates(), 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("获取远程配置失败: %w", err)
	}
	log.Printf("获取更新日志成功，使用源: %s", source)
	return config, nil
}

// 获取远程版本
func getRemoteVersion() string {
	config, err := getRemoteConfig()
	if err != nil {
		log.Printf("获取远程配置失败: %v", err)
		return ""
	}

	if len(config.History) == 0 {
		return ""
	}

	return config.History[0].Version
}

// 健康检查
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "healthy",
		"service": "pt-nexus-updater",
		"time":    time.Now().Format(time.RFC3339),
	})
}

// 获取更新日志
func getChangelogHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	config, err := getRemoteConfig()
	if err != nil {
		log.Printf("获取远程配置失败: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":   false,
			"changelog": []string{},
		})
		return
	}

	log.Printf("解析成功，history 长度: %d", len(config.History))
	if len(config.History) > 0 {
		log.Printf("最新版本: %s, 更新内容数量: %d", config.History[0].Version, len(config.History[0].Changes))
		for i, change := range config.History[0].Changes {
			log.Printf("更新内容 %d: %s", i+1, change)
		}
	}

	// 检查 history 是否为空
	if len(config.History) == 0 {
		log.Printf("远程 CHANGELOG.json 中 history 数组为空")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":   false,
			"changelog": []string{},
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"changelog": config.History[0].Changes,
		"history":   config.History,
	})
}

// 代理到服务器
func proxyToServer(w http.ResponseWriter, r *http.Request) {
	targetURL, _ := url.Parse("http://localhost:" + serverPort)
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
		req.URL.Host = targetURL.Host
		req.URL.Scheme = targetURL.Scheme
	}

	// 设置 CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	proxy.ServeHTTP(w, r)
}

// 代理到 batch
func proxyToBatchEnhancer(w http.ResponseWriter, r *http.Request) {
	targetURL, _ := url.Parse("http://localhost:" + batchEnhancerPort)
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// 设置Director来修改请求
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
		req.URL.Host = targetURL.Host
		req.URL.Scheme = targetURL.Scheme
	}

	// 设置 CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	proxy.ServeHTTP(w, r)
}

func main() {
	log.Println("PT Nexus 更新器启动...")
	log.Println("监听端口:", updaterPort)
	log.Printf("更新源策略: 自动并行探测 GitHub/Gitee")

	// 检查定时配置
	schedule := loadScheduleConfig()
	if schedule.Enabled {
		log.Printf("定时更新已启用，时间: %s (%s)", schedule.Time, schedule.Timezone)
		log.Println("更新方式: 定时检查 + 手动触发")
		// 启动定时检查器
		go startScheduledChecker()
	} else {
		log.Println("更新方式: 手动触发")
		log.Printf("定时更新已禁用，可通过环境变量SCHEDULE_ENABLED=true启用")
	}

	// 注册路由
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/update/check", checkUpdateHandler)
	http.HandleFunc("/update/pull", pullUpdateHandler)
	http.HandleFunc("/update/install", installUpdateHandler)
	http.HandleFunc("/update/changelog", getChangelogHandler)

	// 代理路由
	http.Handle("/batch-enhance/", http.HandlerFunc(proxyToBatchEnhancer))
	http.Handle("/records", http.HandlerFunc(proxyToBatchEnhancer))
	http.Handle("/", http.HandlerFunc(proxyToServer))

	// 启动 HTTP 服务器
	log.Fatal(http.ListenAndServe(":"+updaterPort, nil))
}
