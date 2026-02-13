package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pt-nexus/server-go/internal/bootstrap"
	"github.com/pt-nexus/server-go/internal/platform/logx"
)

// main 是服务启动入口，负责加载环境变量、初始化日志、创建并运行 HTTP 服务。
func main() {
	bootMessages := loadEnvFiles()

	// Ensure log directory is stable regardless of current working directory.
	// Without this, logx defaults to "./data/logs" which depends on where the process is started.
	ensureDefaultLogDir()

	if err := logx.InitFromEnv(); err != nil {
		log.Fatalf("初始化日志系统失败: %v", err)
	}
	for _, message := range bootMessages {
		logx.Infof("启动", "%s", message)
	}

	app, err := bootstrap.NewApp()
	if err != nil {
		logx.Errorf("启动", "初始化应用失败: %v", err)
		log.Fatalf("初始化应用失败: %v", err)
	}

	host := envOrDefault("SERVER_HOST", "0.0.0.0")
	port := envOrDefault("SERVER_PORT", "5275")
	addr := host + ":" + port

	logx.Infof("启动", "PT Nexus Go 服务启动完成，监听地址: http://%s", addr)
	if err := app.Engine.Run(addr); err != nil {
		logx.Errorf("启动", "服务运行失败: %v", err)
		log.Fatalf("服务运行失败: %v", err)
	}
}

// loadEnvFiles 自动加载可用的 .env 文件并返回中文启动提示。
func loadEnvFiles() []string {
	messages := make([]string, 0, 4)

	explicit := strings.TrimSpace(os.Getenv("PTNEXUS_ENV_FILE"))
	if explicit != "" {
		if err := godotenv.Load(explicit); err != nil {
			messages = append(messages, fmt.Sprintf("加载环境变量文件失败（%s）：%v", explicit, err))
		} else {
			messages = append(messages, fmt.Sprintf("已加载环境变量文件：%s", explicit))
		}
		return messages
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	root, found := detectServerGoRoot(cwd)
	if !found {
		messages = append(messages, fmt.Sprintf("未检测到 server-go 根目录（cwd=%s），将仅使用系统环境变量", cwd))
		return messages
	}

	envPath := filepath.Join(root, ".env")
	if _, err := os.Stat(envPath); err != nil {
		messages = append(messages, fmt.Sprintf("未检测到 server-go/.env 文件（path=%s cwd=%s），将仅使用系统环境变量", envPath, cwd))
		return messages
	}
	if err := godotenv.Load(envPath); err != nil {
		messages = append(messages, fmt.Sprintf("加载环境变量文件失败（%s）：%v", envPath, err))
		return messages
	}
	messages = append(messages, fmt.Sprintf("已加载环境变量文件：%s", envPath))
	return messages
}

func ensureDefaultLogDir() {
	if strings.TrimSpace(os.Getenv("PTNEXUS_LOG_DIR")) != "" {
		return
	}

	// Prefer explicit base dir if present (dev scripts set it).
	if baseDir := strings.TrimSpace(os.Getenv("PTNEXUS_BASE_DIR")); baseDir != "" && looksLikeServerGoRoot(baseDir) {
		_ = os.Setenv("PTNEXUS_LOG_DIR", filepath.Join(baseDir, "data", "logs"))
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	root, found := detectServerGoRoot(cwd)
	if !found {
		return
	}
	_ = os.Setenv("PTNEXUS_LOG_DIR", filepath.Join(root, "data", "logs"))
}

// detectServerGoRoot 从起始目录向上定位 server-go 根目录，兼容在仓库根或子目录启动服务。
func detectServerGoRoot(start string) (string, bool) {
	dir := start
	for i := 0; i < 10; i++ {
		candidates := []string{
			dir,
			filepath.Join(dir, "server-go"),
		}
		for _, candidate := range candidates {
			if looksLikeServerGoRoot(candidate) {
				return candidate, true
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

// looksLikeServerGoRoot 判断目录是否符合 server-go 根目录结构。
func looksLikeServerGoRoot(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return false
	}
	if stat, err := os.Stat(filepath.Join(dir, "internal")); err != nil || !stat.IsDir() {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "cmd", "server", "main.go")); err != nil {
		return false
	}
	return true
}

// envOrDefault 读取环境变量，不存在时返回指定默认值。
func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
