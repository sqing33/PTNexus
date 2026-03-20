package desktopapp

import (
	"os"
	"path/filepath"
	"strings"
)

// DesktopRuntimeEnv 描述桌面模式下需要注入的运行时路径与关键文件位置。
// 说明：该结构体用于桌面壳在启动 sidecar 与提供 UI 辅助能力时复用。
type DesktopRuntimeEnv struct {
	// ResourceDir 指向“随应用发布”的只读资源目录（通常是安装目录或 repo 的 server 目录）。
	// 该目录下应包含：sites_data.json、完整的 configs/*.yaml（含 global_mappings.yaml）以及可选 dist。
	ResourceDir string

	// UserBaseDir 指向“用户可写”目录（默认 %APPDATA%/pt-nexus 或同等位置）。
	UserBaseDir string

	DataDir       string
	DBConfigFile  string
	LogDir        string
	StaticDir     string
	SitesDataFile string
	GlobalMapYML  string
}

// EnsureDesktopRuntimeEnv 为桌面模式注入默认运行路径并创建必要目录/文件。
// 副作用：会写入环境变量与创建 data/tmp、data/logs、database.json。
func EnsureDesktopRuntimeEnv() DesktopRuntimeEnv {
	configRoot, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(configRoot) == "" {
		configRoot = "."
	}

	userBaseDir := filepath.Join(configRoot, "pt-nexus")
	dataDir := filepath.Join(userBaseDir, "data")
	dbConfigFile := filepath.Join(dataDir, "database.json")
	logDir := filepath.Join(dataDir, "logs")

	resourceDir := resolveDesktopResourceDir(userBaseDir)
	staticDir := filepath.Join(resourceDir, "dist")
	sitesDataFile := filepath.Join(resourceDir, "sites_data.json")
	globalMapYML := filepath.Join(resourceDir, "configs", "global_mappings.yaml")

	if strings.TrimSpace(os.Getenv("PTNEXUS_BASE_DIR")) == "" {
		_ = os.Setenv("PTNEXUS_BASE_DIR", resourceDir)
	}
	if strings.TrimSpace(os.Getenv("PTNEXUS_DATA_DIR")) == "" {
		_ = os.Setenv("PTNEXUS_DATA_DIR", dataDir)
	}
	if strings.TrimSpace(os.Getenv("PTNEXUS_RUNTIME")) == "" {
		_ = os.Setenv("PTNEXUS_RUNTIME", "desktop")
	}
	if strings.TrimSpace(os.Getenv("PTNEXUS_DB_CONFIG_FILE")) == "" {
		_ = os.Setenv("PTNEXUS_DB_CONFIG_FILE", dbConfigFile)
	}
	if strings.TrimSpace(os.Getenv("PTNEXUS_STATIC_DIR")) == "" {
		_ = os.Setenv("PTNEXUS_STATIC_DIR", staticDir)
	}
	if strings.TrimSpace(os.Getenv("PTNEXUS_LOG_DIR")) == "" {
		_ = os.Setenv("PTNEXUS_LOG_DIR", logDir)
	}
	if strings.TrimSpace(os.Getenv("PTNEXUS_SITES_DATA_FILE")) == "" {
		_ = os.Setenv("PTNEXUS_SITES_DATA_FILE", sitesDataFile)
	}
	if strings.TrimSpace(os.Getenv("PTNEXUS_GLOBAL_MAPPINGS")) == "" {
		_ = os.Setenv("PTNEXUS_GLOBAL_MAPPINGS", globalMapYML)
	}

	_ = os.MkdirAll(dataDir, 0o755)
	_ = os.MkdirAll(filepath.Join(dataDir, "tmp"), 0o755)
	_ = os.MkdirAll(logDir, 0o755)

	EnsureDatabaseConfigFile()

	return DesktopRuntimeEnv{
		ResourceDir:   resourceDir,
		UserBaseDir:   userBaseDir,
		DataDir:       dataDir,
		DBConfigFile:  dbConfigFile,
		LogDir:        logDir,
		StaticDir:     staticDir,
		SitesDataFile: sitesDataFile,
		GlobalMapYML:  globalMapYML,
	}
}

// resolveDesktopResourceDir 选择桌面版“资源目录”：
// - 优先使用 PTNEXUS_RESOURCE_DIR（便于调试/覆盖）
// - 开发模式：如果存在 ../server/sites_data.json，则以 ../server 为资源目录
// - 生产模式：默认使用可执行文件所在目录（安装目录）
// - 兜底：使用用户目录（避免返回空）
func resolveDesktopResourceDir(fallback string) string {
	if explicit := strings.TrimSpace(os.Getenv("PTNEXUS_RESOURCE_DIR")); explicit != "" {
		return explicit
	}

	if cwd, err := os.Getwd(); err == nil && strings.TrimSpace(cwd) != "" {
		devServerDir := filepath.Clean(filepath.Join(cwd, "..", "server"))
		if fileExists(filepath.Join(devServerDir, "sites_data.json")) {
			return devServerDir
		}
	}

	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		exeDir := filepath.Dir(exe)
		if fileExists(filepath.Join(exeDir, "sites_data.json")) {
			return exeDir
		}
		return exeDir
	}

	return fallback
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir()
}

// DatabaseConfigFilePath 返回桌面端数据库配置文件路径。
func DatabaseConfigFilePath() string {
	if configured := strings.TrimSpace(os.Getenv("PTNEXUS_DB_CONFIG_FILE")); configured != "" {
		return configured
	}
	dataDir := strings.TrimSpace(os.Getenv("PTNEXUS_DATA_DIR"))
	if dataDir == "" {
		configRoot, err := os.UserConfigDir()
		if err != nil || strings.TrimSpace(configRoot) == "" {
			configRoot = "."
		}
		dataDir = filepath.Join(configRoot, "pt-nexus", "data")
	}
	return filepath.Join(dataDir, "database.json")
}

// EnsureDatabaseConfigFile 确保 database.json 存在，便于桌面端首次启动后用户直接编辑数据库连接信息。
func EnsureDatabaseConfigFile() {
	path := DatabaseConfigFilePath()
	if strings.TrimSpace(path) == "" {
		return
	}
	if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
		return
	}

	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	// 默认保持 sqlite：桌面开发阶段可先用 sqlite；后续用户可改为 mysql/pg。
	template := `{
  "type": "sqlite",
  "sqlite_path": "",
  "mysql": {
    "host": "",
    "port": 3306,
    "database": "",
    "user": "",
    "password": ""
  },
  "postgresql": {
    "host": "",
    "port": 5432,
    "database": "",
    "user": "",
    "password": "",
    "sslmode": "disable"
  }
}
`
	_ = os.WriteFile(path, []byte(template), 0o644)
}
