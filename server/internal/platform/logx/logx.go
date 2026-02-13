package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Level 表示日志等级，数值越大代表越重要。
type Level int32

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

type formatConfig struct {
	moduleWidth int
	actionWidth int
	hideKeys    map[string]struct{}
}

// Config 定义日志系统初始化参数。
type Config struct {
	LogDir     string
	LogFile    string
	Level      string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

var (
	initOnce      sync.Once
	initErr       error
	currentLevel  atomic.Int32
	activeLogDir  string
	activeLogFile string
	activeOutput  io.Writer    = os.Stdout
	appLogger                  = log.New(activeOutput, "", 0)
	formatCfg     atomic.Value // formatConfig
)

func init() {
	formatCfg.Store(defaultFormatConfig())
	applyFormatConfigFromEnv()
}

// InitFromEnv 从环境变量读取配置并初始化日志系统。
func InitFromEnv() error {
	return Init(loadConfigFromEnv())
}

// Init 初始化全局日志输出到控制台与滚动文件。
func Init(cfg Config) error {
	initOnce.Do(func() {
		resolved, err := normalizeConfig(cfg)
		if err != nil {
			initErr = err
			return
		}

		if err := os.MkdirAll(resolved.LogDir, 0o755); err != nil {
			initErr = fmt.Errorf("创建日志目录失败: %w", err)
			return
		}

		activeLogDir = resolved.LogDir
		activeLogFile = resolved.LogFile
		currentLevel.Store(int32(parseLevel(resolved.Level)))

		fileWriter := &lumberjack.Logger{
			Filename:   filepath.Join(resolved.LogDir, resolved.LogFile),
			MaxSize:    resolved.MaxSizeMB,
			MaxBackups: resolved.MaxBackups,
			MaxAge:     resolved.MaxAgeDays,
			Compress:   resolved.Compress,
			LocalTime:  true,
		}
		activeOutput = io.MultiWriter(os.Stdout, fileWriter)
		appLogger.SetOutput(activeOutput)
		log.SetOutput(activeOutput)
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)

		applyFormatConfigFromEnv()
	})
	return initErr
}

// loadConfigFromEnv 读取日志相关环境变量并应用默认值。
func loadConfigFromEnv() Config {
	return Config{
		LogDir:     envOrDefault("PTNEXUS_LOG_DIR", "./data/logs"),
		LogFile:    envOrDefault("PTNEXUS_LOG_FILE", "ptnexus-go.log"),
		Level:      envOrDefault("PTNEXUS_LOG_LEVEL", "info"),
		MaxSizeMB:  envIntOrDefault("PTNEXUS_LOG_MAX_SIZE_MB", 20),
		MaxBackups: envIntOrDefault("PTNEXUS_LOG_MAX_BACKUPS", 10),
		MaxAgeDays: envIntOrDefault("PTNEXUS_LOG_MAX_AGE_DAYS", 14),
		Compress:   envBoolOrDefault("PTNEXUS_LOG_COMPRESS", true),
	}
}

// normalizeConfig 校验并修正日志配置，避免非法参数导致运行失败。
func normalizeConfig(cfg Config) (Config, error) {
	logDir := strings.TrimSpace(cfg.LogDir)
	if logDir == "" {
		logDir = "./data/logs"
	}
	absDir, err := filepath.Abs(logDir)
	if err != nil {
		return Config{}, fmt.Errorf("解析日志目录失败: %w", err)
	}

	logFile := strings.TrimSpace(cfg.LogFile)
	if logFile == "" {
		logFile = "ptnexus-go.log"
	}
	if strings.Contains(logFile, string(filepath.Separator)) {
		logFile = filepath.Base(logFile)
	}

	if cfg.MaxSizeMB <= 0 {
		cfg.MaxSizeMB = 20
	}
	if cfg.MaxBackups < 0 {
		cfg.MaxBackups = 10
	}
	if cfg.MaxAgeDays < 0 {
		cfg.MaxAgeDays = 14
	}
	if strings.TrimSpace(cfg.Level) == "" {
		cfg.Level = "info"
	}

	cfg.LogDir = absDir
	cfg.LogFile = logFile
	return cfg, nil
}

// GetLogDir 返回当前日志目录绝对路径。
func GetLogDir() string {
	return activeLogDir
}

// GetPrimaryLogFile 返回当前主日志文件绝对路径。
func GetPrimaryLogFile() string {
	if activeLogDir == "" || activeLogFile == "" {
		return ""
	}
	return filepath.Join(activeLogDir, activeLogFile)
}

// Writer 返回统一日志输出目标，供 Gin 访问日志复用。
func Writer() io.Writer {
	return activeOutput
}

// Debugf 输出调试级别日志。
func Debugf(module string, format string, args ...any) {
	logf(LevelDebug, module, format, args...)
}

// Infof 输出信息级别日志。
func Infof(module string, format string, args ...any) {
	logf(LevelInfo, module, format, args...)
}

// Warnf 输出警告级别日志。
func Warnf(module string, format string, args ...any) {
	logf(LevelWarn, module, format, args...)
}

// Errorf 输出错误级别日志。
func Errorf(module string, format string, args ...any) {
	logf(LevelError, module, format, args...)
}

// PlainModuleInfof 输出带模块前缀的多行信息日志，不进行字段拆分，也不会转义换行（用于快照/对账类日志）。
func PlainModuleInfof(module string, action string, format string, args ...any) {
	plainModulef(LevelInfo, module, action, format, args...)
}

// PlainModuleWarnf 输出带模块前缀的多行警告日志，不进行字段拆分，也不会转义换行（用于快照/对账类日志）。
func PlainModuleWarnf(module string, action string, format string, args ...any) {
	plainModulef(LevelWarn, module, action, format, args...)
}

// PlainModuleErrorf 输出带模块前缀的多行错误日志，不进行字段拆分，也不会转义换行（用于快照/对账类日志）。
func PlainModuleErrorf(module string, action string, format string, args ...any) {
	plainModulef(LevelError, module, action, format, args...)
}

// PlainInfof 输出纯文本信息日志，不包含三段括号前缀与字段拆分（用于叙事式日志）。
func PlainInfof(format string, args ...any) {
	plainf(LevelInfo, format, args...)
}

// PlainWarnf 输出纯文本警告日志，不包含三段括号前缀与字段拆分（用于叙事式日志）。
func PlainWarnf(format string, args ...any) {
	plainf(LevelWarn, format, args...)
}

// PlainErrorf 输出纯文本错误日志，不包含三段括号前缀与字段拆分（用于叙事式日志）。
func PlainErrorf(format string, args ...any) {
	plainf(LevelError, format, args...)
}

// logf 根据等级过滤并统一拼接中文日志前缀。
func logf(level Level, module string, format string, args ...any) {
	if int32(level) < currentLevel.Load() {
		return
	}
	line := formatLine(time.Now(), level, module, fmt.Sprintf(format, args...))
	appLogger.Print(line)
}

// plainf 根据等级过滤并输出纯文本行，不加任何格式前缀。
func plainf(level Level, format string, args ...any) {
	if int32(level) < currentLevel.Load() {
		return
	}
	appLogger.Print(fmt.Sprintf(format, args...))
}

// plainModulef 输出“模块 + 动作”前缀，并保留消息中的真实换行（不会被转义为 \n）。
func plainModulef(level Level, module string, action string, format string, args ...any) {
	if int32(level) < currentLevel.Load() {
		return
	}
	msg := fmt.Sprintf(format, args...)
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	msg = strings.ReplaceAll(msg, "\r", "\n")
	msg = strings.TrimRight(msg, "\n")

	header := formatPrefix(time.Now(), level, module, action)
	if msg == "" {
		appLogger.Print(header)
		return
	}
	appLogger.Print(header + "\n" + msg)
}

// levelLabel 将内部等级转换为中文标签。
func levelLabel(level Level) string {
	switch level {
	case LevelDebug:
		return "调试"
	case LevelInfo:
		return "信息"
	case LevelWarn:
		return "警告"
	case LevelError:
		return "错误"
	default:
		return "信息"
	}
}

const (
	defaultModuleWidth = 22
	defaultActionWidth = 22
)

var keyPattern = regexp.MustCompile(`(^|[\s，,。.;；:：()（）\[\]{}<>《》“”‘’'"、])([A-Za-z_\p{Han}][A-Za-z0-9_\p{Han}]*)=`)

func formatPrefix(now time.Time, level Level, module string, action string) string {
	timestamp := now.Format("2006/01/02 15:04:05")
	mod := strings.TrimSpace(module)
	if mod == "" {
		mod = "通用"
	}
	cfg := currentFormatConfig()
	mod = padCenterDisplayWidth(truncateDisplayWidth(mod, cfg.moduleWidth), cfg.moduleWidth)

	act := strings.TrimSpace(action)
	if act == "" {
		act = "消息"
	}
	actAligned := act
	if cfg.actionWidth > 0 {
		if displayWidth(act) <= cfg.actionWidth {
			actAligned = padCenterDisplayWidth(act, cfg.actionWidth)
		} else {
			actAligned = act
		}
	}
	return fmt.Sprintf("%s [%s] [%s] [%s]", timestamp, levelLabel(level), mod, actAligned)
}

func formatLine(now time.Time, level Level, module string, message string) string {
	timestamp := now.Format("2006/01/02 15:04:05")
	mod := strings.TrimSpace(module)
	if mod == "" {
		mod = "通用"
	}
	cfg := currentFormatConfig()
	mod = padCenterDisplayWidth(truncateDisplayWidth(mod, cfg.moduleWidth), cfg.moduleWidth)

	msg := sanitizeMessage(message)
	action, pairs := splitActionAndPairs(msg)
	if action == "" {
		if len(pairs) > 0 {
			action = "字段"
		} else {
			action = strings.TrimSpace(msg)
		}
	}
	if action == "" {
		action = "消息"
	}

	actionAligned := action
	if cfg.actionWidth > 0 {
		if displayWidth(action) <= cfg.actionWidth {
			actionAligned = padCenterDisplayWidth(action, cfg.actionWidth)
		} else {
			actionAligned = action
		}
	}

	fields := buildVisibleFields(cfg, pairs)
	prefix := fmt.Sprintf("%s [%s] [%s] [%s]", timestamp, levelLabel(level), mod, actionAligned)
	if len(fields) > 0 {
		return prefix + " " + strings.Join(fields, " ")
	}
	return prefix
}

func sanitizeMessage(message string) string {
	msg := strings.ReplaceAll(message, "\r\n", "\n")
	msg = strings.ReplaceAll(msg, "\r", "\n")
	msg = strings.ReplaceAll(msg, "\n", `\n`)
	return msg
}

type kvPair struct {
	Key   string
	Value string
}

func splitActionAndPairs(message string) (string, []kvPair) {
	matches := keyPattern.FindAllStringSubmatchIndex(message, -1)
	if len(matches) == 0 {
		return "", nil
	}

	first := matches[0]
	action := strings.TrimSpace(message[:first[0]])
	action = strings.TrimSpace(strings.TrimRight(action, "：:"))

	pairs := make([]kvPair, 0, len(matches))
	for idx, match := range matches {
		if len(match) < 6 {
			continue
		}
		keyStart, keyEnd := match[4], match[5]
		valueStart := match[1]
		valueEnd := len(message)
		if idx+1 < len(matches) {
			valueEnd = matches[idx+1][0]
		}

		key := strings.TrimSpace(message[keyStart:keyEnd])
		if key == "" {
			continue
		}
		value := strings.TrimSpace(message[valueStart:valueEnd])
		pairs = append(pairs, kvPair{Key: key, Value: value})
	}
	return action, pairs
}

func padRightDisplayWidth(value string, width int) string {
	if width <= 0 {
		return value
	}
	current := displayWidth(value)
	if current >= width {
		return value
	}
	return value + strings.Repeat(" ", width-current)
}

func padLeftDisplayWidth(value string, width int) string {
	if width <= 0 {
		return value
	}
	current := displayWidth(value)
	if current >= width {
		return value
	}
	return strings.Repeat(" ", width-current) + value
}

func padCenterDisplayWidth(value string, width int) string {
	if width <= 0 {
		return value
	}
	current := displayWidth(value)
	if current >= width {
		return value
	}
	missing := width - current
	left := missing / 2
	right := missing - left
	return strings.Repeat(" ", left) + value + strings.Repeat(" ", right)
}

func truncateDisplayWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(value) <= width {
		return value
	}
	if width <= 3 {
		return sliceByDisplayWidth(value, width)
	}
	return sliceByDisplayWidth(value, width-3) + "..."
}

func sliceByDisplayWidth(value string, width int) string {
	if width <= 0 || value == "" {
		return ""
	}
	w := 0
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		rw := runeWidth(r)
		if rw == 0 {
			continue
		}
		if w+rw > width {
			break
		}
		builder.WriteRune(r)
		w += rw
	}
	return builder.String()
}

func displayWidth(value string) int {
	width := 0
	for _, r := range value {
		width += runeWidth(r)
	}
	return width
}

func runeWidth(r rune) int {
	switch {
	case r == '\t':
		return 4
	case r == 0:
		return 0
	case r < 32 || r == 127:
		return 0
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r):
		return 0
	case r <= 0x7e:
		return 1
	default:
		return 2
	}
}

func defaultFormatConfig() formatConfig {
	return formatConfig{
		moduleWidth: defaultModuleWidth,
		actionWidth: defaultActionWidth,
		hideKeys:    defaultHiddenKeys(),
	}
}

func currentFormatConfig() formatConfig {
	value := formatCfg.Load()
	if value == nil {
		return defaultFormatConfig()
	}
	return value.(formatConfig)
}

func applyFormatConfigFromEnv() {
	cfg := defaultFormatConfig()

	if raw, ok := os.LookupEnv("PTNEXUS_LOG_MODULE_WIDTH"); ok {
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" {
			if parsed, err := strconv.Atoi(trimmed); err == nil && parsed > 0 {
				cfg.moduleWidth = parsed
			}
		}
	}

	if raw, ok := os.LookupEnv("PTNEXUS_LOG_ACTION_WIDTH"); ok {
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" {
			if parsed, err := strconv.Atoi(trimmed); err == nil && parsed > 0 {
				cfg.actionWidth = parsed
			}
		}
	}

	if raw, ok := os.LookupEnv("PTNEXUS_LOG_HIDE_KEYS"); ok {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			cfg.hideKeys = map[string]struct{}{}
		} else {
			cfg.hideKeys = parseHiddenKeys(trimmed)
		}
	}

	formatCfg.Store(cfg)
}

func defaultHiddenKeys() map[string]struct{} {
	keys := []string{
		"task_id",
		"request_id",
		"seed_id",
		"info_hash",
		"hash",
		"context_id",
		"bdinfo_task_id",
		"请求id",
	}
	return buildHiddenKeySet(keys)
}

func parseHiddenKeys(raw string) map[string]struct{} {
	parts := strings.Split(raw, ",")
	keys := make([]string, 0, len(parts))
	for _, part := range parts {
		key := strings.TrimSpace(part)
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	return buildHiddenKeySet(keys)
}

func buildHiddenKeySet(keys []string) map[string]struct{} {
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		normalized := normalizeLogKey(key)
		if normalized == "" {
			continue
		}
		set[normalized] = struct{}{}
	}
	return set
}

func normalizeLogKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func buildVisibleFields(cfg formatConfig, pairs []kvPair) []string {
	if len(pairs) == 0 {
		return nil
	}
	fields := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		if pair.Key == "" {
			continue
		}
		if _, hidden := cfg.hideKeys[normalizeLogKey(pair.Key)]; hidden {
			continue
		}
		fields = append(fields, fmt.Sprintf("%s=%s", pair.Key, pair.Value))
	}
	return fields
}

// parseLevel 将配置字符串转换为内部日志等级。
func parseLevel(raw string) Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// envOrDefault 读取字符串环境变量，不存在时返回默认值。
func envOrDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

// envIntOrDefault 读取整数环境变量，解析失败时返回默认值。
func envIntOrDefault(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

// envBoolOrDefault 读取布尔环境变量，解析失败时返回默认值。
func envBoolOrDefault(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}
