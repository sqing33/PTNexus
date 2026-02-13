package config

import (
	"os"
	"path/filepath"
	"strings"
)

type RuntimePaths struct {
	BaseDir            string
	DataDir            string
	StaticDir          string
	ConfigFile         string
	DatabaseConfigFile string
	SitesData          string
	GlobalMapYML       string
}

func ResolveRuntimePaths() RuntimePaths {
	isDev := os.Getenv("DEV_ENV") == "true"

	defaultBaseDir := "/app/server-go"
	defaultDataDir := "/app/server-go/data"
	if isDev {
		defaultBaseDir = detectDevBaseDir()
		defaultDataDir = filepath.Join(defaultBaseDir, "data")
	}

	baseDir := getEnvOrDefault("PTNEXUS_BASE_DIR", defaultBaseDir)
	dataDir := getEnvOrDefault("PTNEXUS_DATA_DIR", defaultDataDir)

	paths := RuntimePaths{
		BaseDir:            baseDir,
		DataDir:            dataDir,
		StaticDir:          getEnvOrDefault("PTNEXUS_STATIC_DIR", filepath.Join(baseDir, "dist")),
		ConfigFile:         filepath.Join(dataDir, "config.json"),
		DatabaseConfigFile: getEnvOrDefault("PTNEXUS_DB_CONFIG_FILE", filepath.Join(dataDir, "database.json")),
		SitesData:          getEnvOrDefault("PTNEXUS_SITES_DATA_FILE", filepath.Join(baseDir, "sites_data.json")),
		GlobalMapYML:       getEnvOrDefault("PTNEXUS_GLOBAL_MAPPINGS", filepath.Join(baseDir, "configs", "global_mappings.yaml")),
	}

	_ = os.MkdirAll(paths.DataDir, 0o755)
	_ = os.MkdirAll(filepath.Join(paths.DataDir, "tmp"), 0o755)
	return paths
}

func detectDevBaseDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	tryDirs := func(dir string) string {
		candidates := []string{
			dir,
			filepath.Join(dir, "server-go"),
		}
		for _, candidate := range candidates {
			if looksLikeServerGoRoot(candidate) {
				return candidate
			}
		}
		return ""
	}

	dir := cwd
	for i := 0; i < 10; i++ {
		if hit := tryDirs(dir); hit != "" {
			return hit
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return cwd
}

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
	return true
}

func getEnvOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
