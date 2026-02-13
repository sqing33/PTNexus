package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const desktopRuntime = "desktop"

type DatabaseConfig struct {
	Type       string                 `json:"type"`
	SQLitePath string                 `json:"sqlite_path"`
	MySQL      DatabaseMySQLConfig    `json:"mysql"`
	PostgreSQL DatabasePostgresConfig `json:"postgresql"`
}

type DatabaseMySQLConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
}

type DatabasePostgresConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
	SSLMode  string `json:"sslmode"`
}

// LoadDesktopDatabaseConfig 仅在桌面模式下读取 database.json。
// 非桌面模式直接返回 found=false，避免影响 Docker/服务端环境。
func LoadDesktopDatabaseConfig(paths RuntimePaths) (cfg DatabaseConfig, found bool, err error) {
	if strings.ToLower(strings.TrimSpace(os.Getenv("PTNEXUS_RUNTIME"))) != desktopRuntime {
		return DatabaseConfig{}, false, nil
	}

	configPath := strings.TrimSpace(paths.DatabaseConfigFile)
	if configPath == "" {
		return DatabaseConfig{}, false, nil
	}

	payload, readErr := os.ReadFile(configPath)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return DatabaseConfig{}, false, nil
		}
		return DatabaseConfig{}, false, fmt.Errorf("read database config failed: %w", readErr)
	}

	cfg = DatabaseConfig{}
	if unmarshalErr := json.Unmarshal(payload, &cfg); unmarshalErr != nil {
		return DatabaseConfig{}, false, fmt.Errorf("parse database config failed: %w", unmarshalErr)
	}

	cfg.Type = strings.ToLower(strings.TrimSpace(cfg.Type))
	if cfg.MySQL.Port <= 0 {
		cfg.MySQL.Port = 3306
	}
	if cfg.PostgreSQL.Port <= 0 {
		cfg.PostgreSQL.Port = 5432
	}
	if strings.TrimSpace(cfg.PostgreSQL.SSLMode) == "" {
		cfg.PostgreSQL.SSLMode = "disable"
	}

	return cfg, true, nil
}
