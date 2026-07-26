package repository

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const appSettingsConfigKey = "config"

// AppSettingsRepository stores the root settings JSON in the database.
type AppSettingsRepository struct {
	store *Store
}

// NewAppSettingsRepository creates a repository for application settings.
func NewAppSettingsRepository(store *Store) *AppSettingsRepository {
	return &AppSettingsRepository{store: store}
}

// LoadConfig reads the root settings snapshot from app_settings.
func (r *AppSettingsRepository) LoadConfig() (map[string]any, bool, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return nil, false, fmt.Errorf("database store is not initialized")
	}

	rows := make([]struct {
		ValueJSON string `gorm:"column:value_json"`
	}, 0, 1)
	if err := r.store.DB.Raw(
		"SELECT value_json FROM app_settings WHERE setting_key = ? LIMIT 1",
		appSettingsConfigKey,
	).Scan(&rows).Error; err != nil {
		return nil, false, err
	}
	if len(rows) == 0 || strings.TrimSpace(rows[0].ValueJSON) == "" {
		return nil, false, nil
	}

	configData := map[string]any{}
	if err := json.Unmarshal([]byte(rows[0].ValueJSON), &configData); err != nil {
		return nil, true, fmt.Errorf("parse database config failed: %w", err)
	}
	return configData, true, nil
}

// SaveConfig writes the root settings snapshot to app_settings.
func (r *AppSettingsRepository) SaveConfig(configData map[string]any) error {
	if r == nil || r.store == nil || r.store.DB == nil {
		return fmt.Errorf("database store is not initialized")
	}

	content, err := json.MarshalIndent(configData, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal database config failed: %w", err)
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	switch strings.ToLower(strings.TrimSpace(r.store.DBType)) {
	case "mysql":
		return r.store.DB.Exec(
			`INSERT INTO app_settings (setting_key, value_json, created_at, updated_at)
			 VALUES (?, ?, ?, ?)
			 ON DUPLICATE KEY UPDATE value_json = VALUES(value_json), updated_at = VALUES(updated_at)`,
			appSettingsConfigKey,
			string(content),
			now,
			now,
		).Error
	case "postgresql":
		return r.store.DB.Exec(
			`INSERT INTO app_settings (setting_key, value_json, created_at, updated_at)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT (setting_key) DO UPDATE
			 SET value_json = EXCLUDED.value_json, updated_at = EXCLUDED.updated_at`,
			appSettingsConfigKey,
			string(content),
			now,
			now,
		).Error
	default:
		return r.store.DB.Exec(
			`INSERT INTO app_settings (setting_key, value_json, created_at, updated_at)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(setting_key) DO UPDATE
			 SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
			appSettingsConfigKey,
			string(content),
			now,
			now,
		).Error
	}
}
