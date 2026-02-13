package repository

import (
	"fmt"
	"strings"

	"github.com/pt-nexus/server-go/internal/platform/logx"
)

const schemaLogModule = "数据库初始化"

type schemaColumnSpec struct {
	name       string
	definition map[string]string
}

type schemaIndexSpec struct {
	table   string
	name    string
	columns []string
}

// SchemaManager 负责初始化数据库核心表结构并补齐缺失列。
// 参数/返回：依赖 Store 访问数据库，初始化方法返回 error 表示失败原因。
// 失败场景：数据库连接异常、DDL 执行失败、信息模式查询失败。
// 副作用：会创建表、添加缺失列、创建索引并写入初始化日志。
type SchemaManager struct {
	store *Store
}

// NewSchemaManager 创建数据库结构管理器实例。
// 参数/返回：store 为数据库连接容器，返回可复用的结构管理器。
// 失败场景：无直接失败场景。
// 副作用：无副作用，仅构造对象。
func NewSchemaManager(store *Store) *SchemaManager {
	return &SchemaManager{store: store}
}

// EnsureSchema 确保核心业务表与关键字段存在，避免接口因缺表缺列返回 500。
// 参数/返回：无参数，成功返回 nil，失败返回详细错误。
// 失败场景：建表 SQL 执行失败、补列失败、索引创建失败。
// 副作用：会执行 CREATE TABLE、ALTER TABLE、CREATE INDEX。
func (m *SchemaManager) EnsureSchema() error {
	if m.store == nil || m.store.DB == nil {
		return fmt.Errorf("数据库连接未初始化")
	}

	logx.Infof(schemaLogModule, "开始校验数据库结构 db_type=%s", m.store.DBType)

	for _, sqlText := range m.createTableSQLs() {
		if err := m.store.DB.Exec(sqlText).Error; err != nil {
			return fmt.Errorf("创建核心表失败: %w", err)
		}
	}

	for tableName, columns := range m.columnSpecs() {
		if err := m.ensureTableColumns(tableName, columns); err != nil {
			return err
		}
	}

	for _, index := range m.indexSpecs() {
		if err := m.ensureIndex(index); err != nil {
			return err
		}
	}

	logx.Infof(schemaLogModule, "数据库结构校验完成 db_type=%s", m.store.DBType)
	return nil
}

func (m *SchemaManager) createTableSQLs() []string {
	switch m.store.DBType {
	case "mysql":
		return []string{
			`CREATE TABLE IF NOT EXISTS traffic_stats (
				stat_datetime DATETIME NOT NULL,
				downloader_id VARCHAR(64) NOT NULL,
				uploaded BIGINT DEFAULT 0,
				downloaded BIGINT DEFAULT 0,
				upload_speed BIGINT DEFAULT 0,
				download_speed BIGINT DEFAULT 0,
				cumulative_uploaded BIGINT NOT NULL DEFAULT 0,
				cumulative_downloaded BIGINT NOT NULL DEFAULT 0,
				PRIMARY KEY (stat_datetime, downloader_id)
			) ENGINE=InnoDB ROW_FORMAT=Dynamic`,
			`CREATE TABLE IF NOT EXISTS traffic_stats_hourly (
				stat_datetime DATETIME NOT NULL,
				downloader_id VARCHAR(64) NOT NULL,
				uploaded BIGINT DEFAULT 0,
				downloaded BIGINT DEFAULT 0,
				avg_upload_speed BIGINT DEFAULT 0,
				avg_download_speed BIGINT DEFAULT 0,
				samples INTEGER DEFAULT 0,
				cumulative_uploaded BIGINT NOT NULL DEFAULT 0,
				cumulative_downloaded BIGINT NOT NULL DEFAULT 0,
				PRIMARY KEY (stat_datetime, downloader_id)
			) ENGINE=InnoDB ROW_FORMAT=Dynamic`,
			`CREATE TABLE IF NOT EXISTS torrents (
				hash VARCHAR(64) NOT NULL,
				name TEXT NOT NULL,
				save_path TEXT,
				size BIGINT,
				progress DOUBLE,
				state VARCHAR(50),
				sites VARCHAR(255),
				` + "`group`" + ` VARCHAR(255),
				details TEXT,
				downloader_id VARCHAR(64) NOT NULL,
				last_seen DATETIME NOT NULL,
				iyuu_last_check DATETIME NULL,
				seeders INT DEFAULT 0,
				is_hidden TINYINT(1) NOT NULL DEFAULT 0,
				hidden_reason VARCHAR(64) NULL,
				hidden_at DATETIME NULL,
				PRIMARY KEY (hash, downloader_id)
			) ENGINE=InnoDB ROW_FORMAT=Dynamic`,
			`CREATE TABLE IF NOT EXISTS torrent_upload_stats (
				hash VARCHAR(64) NOT NULL,
				downloader_id VARCHAR(64) NOT NULL,
				uploaded BIGINT DEFAULT 0,
				is_hidden TINYINT(1) NOT NULL DEFAULT 0,
				hidden_reason VARCHAR(64) NULL,
				hidden_at DATETIME NULL,
				PRIMARY KEY (hash, downloader_id)
			) ENGINE=InnoDB ROW_FORMAT=Dynamic`,
			`CREATE TABLE IF NOT EXISTS sites (
				id BIGINT NOT NULL AUTO_INCREMENT,
				site VARCHAR(255) UNIQUE DEFAULT NULL,
				nickname VARCHAR(255) DEFAULT NULL,
				base_url VARCHAR(255) DEFAULT NULL,
				special_tracker_domain VARCHAR(255) DEFAULT NULL,
				` + "`group`" + ` VARCHAR(255) DEFAULT NULL,
				description VARCHAR(255) DEFAULT NULL,
				cookie TEXT DEFAULT NULL,
				passkey TEXT DEFAULT NULL,
				migration INT NOT NULL DEFAULT 1,
				speed_limit INT NOT NULL DEFAULT 0,
				ratio_threshold DOUBLE NOT NULL DEFAULT 3.0,
				seed_speed_limit INT NOT NULL DEFAULT 5,
				PRIMARY KEY (id)
			) ENGINE=InnoDB ROW_FORMAT=Dynamic`,
			`CREATE TABLE IF NOT EXISTS seed_parameters (
				hash VARCHAR(64) NOT NULL,
				torrent_id VARCHAR(255) NOT NULL,
				site_name VARCHAR(255) NOT NULL,
				nickname VARCHAR(255),
				name TEXT,
				title TEXT,
				subtitle TEXT,
				imdb_link TEXT,
				douban_link TEXT,
				tmdb_link TEXT,
				type VARCHAR(100),
				medium VARCHAR(100),
				video_codec VARCHAR(100),
				audio_codec VARCHAR(100),
				resolution VARCHAR(100),
				team VARCHAR(100),
				source VARCHAR(100),
				tags TEXT,
				poster TEXT,
				screenshots TEXT,
				statement TEXT,
				body TEXT,
				mediainfo TEXT,
				title_components TEXT,
				removed_ardtudeclarations TEXT,
				is_reviewed TINYINT(1) NOT NULL DEFAULT 0,
				mediainfo_status VARCHAR(20) DEFAULT 'pending',
				bdinfo_task_id VARCHAR(36),
				bdinfo_started_at DATETIME,
				bdinfo_completed_at DATETIME,
				bdinfo_error TEXT,
				created_at DATETIME NOT NULL,
				updated_at DATETIME NOT NULL,
				PRIMARY KEY (hash, torrent_id, site_name)
			) ENGINE=InnoDB ROW_FORMAT=Dynamic`,
			`CREATE TABLE IF NOT EXISTS batch_enhance_records (
				id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
				title TEXT,
				batch_id VARCHAR(255) NOT NULL,
				torrent_id VARCHAR(255) NOT NULL,
				source_site VARCHAR(255) NOT NULL,
				target_site VARCHAR(255) NOT NULL,
				video_size_gb DECIMAL(8,2),
				status VARCHAR(50) NOT NULL,
				success_url TEXT,
				error_detail TEXT,
				downloader_add_result TEXT,
				processed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				progress VARCHAR(20)
			) ENGINE=InnoDB ROW_FORMAT=Dynamic`,
		}
	case "postgresql":
		return []string{
			`CREATE TABLE IF NOT EXISTS traffic_stats (
				stat_datetime TIMESTAMP NOT NULL,
				downloader_id VARCHAR(64) NOT NULL,
				uploaded BIGINT DEFAULT 0,
				downloaded BIGINT DEFAULT 0,
				upload_speed BIGINT DEFAULT 0,
				download_speed BIGINT DEFAULT 0,
				cumulative_uploaded BIGINT NOT NULL DEFAULT 0,
				cumulative_downloaded BIGINT NOT NULL DEFAULT 0,
				PRIMARY KEY (stat_datetime, downloader_id)
			)`,
			`CREATE TABLE IF NOT EXISTS traffic_stats_hourly (
				stat_datetime TIMESTAMP NOT NULL,
				downloader_id VARCHAR(64) NOT NULL,
				uploaded BIGINT DEFAULT 0,
				downloaded BIGINT DEFAULT 0,
				avg_upload_speed BIGINT DEFAULT 0,
				avg_download_speed BIGINT DEFAULT 0,
				samples INTEGER DEFAULT 0,
				cumulative_uploaded BIGINT NOT NULL DEFAULT 0,
				cumulative_downloaded BIGINT NOT NULL DEFAULT 0,
				PRIMARY KEY (stat_datetime, downloader_id)
			)`,
			`CREATE TABLE IF NOT EXISTS torrents (
				hash VARCHAR(64) NOT NULL,
				name TEXT NOT NULL,
				save_path TEXT,
				size BIGINT,
				progress DOUBLE PRECISION,
				state VARCHAR(50),
				sites VARCHAR(255),
				"group" VARCHAR(255),
				details TEXT,
				downloader_id VARCHAR(64) NOT NULL,
				last_seen TIMESTAMP NOT NULL,
				iyuu_last_check TIMESTAMP NULL,
				seeders INTEGER DEFAULT 0,
				is_hidden INTEGER NOT NULL DEFAULT 0,
				hidden_reason VARCHAR(64),
				hidden_at TIMESTAMP NULL,
				PRIMARY KEY (hash, downloader_id)
			)`,
			`CREATE TABLE IF NOT EXISTS torrent_upload_stats (
				hash VARCHAR(64) NOT NULL,
				downloader_id VARCHAR(64) NOT NULL,
				uploaded BIGINT DEFAULT 0,
				is_hidden INTEGER NOT NULL DEFAULT 0,
				hidden_reason VARCHAR(64),
				hidden_at TIMESTAMP NULL,
				PRIMARY KEY (hash, downloader_id)
			)`,
			`CREATE TABLE IF NOT EXISTS sites (
				id BIGSERIAL PRIMARY KEY,
				site VARCHAR(255) UNIQUE,
				nickname VARCHAR(255),
				base_url VARCHAR(255),
				special_tracker_domain VARCHAR(255),
				"group" VARCHAR(255),
				description VARCHAR(255),
				cookie TEXT,
				passkey TEXT,
				migration INTEGER NOT NULL DEFAULT 1,
				speed_limit INTEGER NOT NULL DEFAULT 0,
				ratio_threshold DOUBLE PRECISION NOT NULL DEFAULT 3.0,
				seed_speed_limit INTEGER NOT NULL DEFAULT 5
			)`,
			`CREATE TABLE IF NOT EXISTS seed_parameters (
				hash VARCHAR(64) NOT NULL,
				torrent_id VARCHAR(255) NOT NULL,
				site_name VARCHAR(255) NOT NULL,
				nickname VARCHAR(255),
				name TEXT,
				title TEXT,
				subtitle TEXT,
				imdb_link TEXT,
				douban_link TEXT,
				tmdb_link TEXT,
				type VARCHAR(100),
				medium VARCHAR(100),
				video_codec VARCHAR(100),
				audio_codec VARCHAR(100),
				resolution VARCHAR(100),
				team VARCHAR(100),
				source VARCHAR(100),
				tags TEXT,
				poster TEXT,
				screenshots TEXT,
				statement TEXT,
				body TEXT,
				mediainfo TEXT,
				title_components TEXT,
				removed_ardtudeclarations TEXT,
				is_reviewed BOOLEAN NOT NULL DEFAULT FALSE,
				mediainfo_status VARCHAR(20) DEFAULT 'pending',
				bdinfo_task_id VARCHAR(36),
				bdinfo_started_at TIMESTAMP,
				bdinfo_completed_at TIMESTAMP,
				bdinfo_error TEXT,
				created_at TIMESTAMP NOT NULL,
				updated_at TIMESTAMP NOT NULL,
				PRIMARY KEY (hash, torrent_id, site_name)
			)`,
			`CREATE TABLE IF NOT EXISTS batch_enhance_records (
				id BIGSERIAL PRIMARY KEY,
				title TEXT,
				batch_id VARCHAR(255) NOT NULL,
				torrent_id VARCHAR(255) NOT NULL,
				source_site VARCHAR(255) NOT NULL,
				target_site VARCHAR(255) NOT NULL,
				video_size_gb DECIMAL(8,2),
				status VARCHAR(50) NOT NULL,
				success_url TEXT,
				error_detail TEXT,
				downloader_add_result TEXT,
				processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				progress VARCHAR(20)
			)`,
		}
	default:
		return []string{
			`CREATE TABLE IF NOT EXISTS traffic_stats (
				stat_datetime TEXT NOT NULL,
				downloader_id TEXT NOT NULL,
				uploaded INTEGER DEFAULT 0,
				downloaded INTEGER DEFAULT 0,
				upload_speed INTEGER DEFAULT 0,
				download_speed INTEGER DEFAULT 0,
				cumulative_uploaded INTEGER NOT NULL DEFAULT 0,
				cumulative_downloaded INTEGER NOT NULL DEFAULT 0,
				PRIMARY KEY (stat_datetime, downloader_id)
			)`,
			`CREATE TABLE IF NOT EXISTS traffic_stats_hourly (
				stat_datetime TEXT NOT NULL,
				downloader_id TEXT NOT NULL,
				uploaded INTEGER DEFAULT 0,
				downloaded INTEGER DEFAULT 0,
				avg_upload_speed INTEGER DEFAULT 0,
				avg_download_speed INTEGER DEFAULT 0,
				samples INTEGER DEFAULT 0,
				cumulative_uploaded INTEGER NOT NULL DEFAULT 0,
				cumulative_downloaded INTEGER NOT NULL DEFAULT 0,
				PRIMARY KEY (stat_datetime, downloader_id)
			)`,
			`CREATE TABLE IF NOT EXISTS torrents (
				hash TEXT NOT NULL,
				name TEXT NOT NULL,
				save_path TEXT,
				size INTEGER,
				progress REAL,
				state TEXT,
				sites TEXT,
				` + "`group`" + ` TEXT,
				details TEXT,
				downloader_id TEXT NOT NULL,
				last_seen TEXT NOT NULL,
				iyuu_last_check TEXT NULL,
				seeders INTEGER DEFAULT 0,
				is_hidden INTEGER NOT NULL DEFAULT 0,
				hidden_reason TEXT,
				hidden_at TEXT NULL,
				PRIMARY KEY (hash, downloader_id)
			)`,
			`CREATE TABLE IF NOT EXISTS torrent_upload_stats (
				hash TEXT NOT NULL,
				downloader_id TEXT NOT NULL,
				uploaded INTEGER DEFAULT 0,
				is_hidden INTEGER NOT NULL DEFAULT 0,
				hidden_reason TEXT,
				hidden_at TEXT NULL,
				PRIMARY KEY (hash, downloader_id)
			)`,
			`CREATE TABLE IF NOT EXISTS sites (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				site TEXT UNIQUE,
				nickname TEXT,
				base_url TEXT,
				special_tracker_domain TEXT,
				` + "`group`" + ` TEXT,
				description TEXT,
				cookie TEXT,
				passkey TEXT,
				migration INTEGER NOT NULL DEFAULT 1,
				speed_limit INTEGER NOT NULL DEFAULT 0,
				ratio_threshold REAL NOT NULL DEFAULT 3.0,
				seed_speed_limit INTEGER NOT NULL DEFAULT 5
			)`,
			`CREATE TABLE IF NOT EXISTS seed_parameters (
				hash TEXT NOT NULL,
				torrent_id TEXT NOT NULL,
				site_name TEXT NOT NULL,
				nickname TEXT,
				name TEXT,
				title TEXT,
				subtitle TEXT,
				imdb_link TEXT,
				douban_link TEXT,
				tmdb_link TEXT,
				type TEXT,
				medium TEXT,
				video_codec TEXT,
				audio_codec TEXT,
				resolution TEXT,
				team TEXT,
				source TEXT,
				tags TEXT,
				poster TEXT,
				screenshots TEXT,
				statement TEXT,
				body TEXT,
				mediainfo TEXT,
				title_components TEXT,
				removed_ardtudeclarations TEXT,
				is_reviewed INTEGER NOT NULL DEFAULT 0,
				mediainfo_status TEXT DEFAULT 'pending',
				bdinfo_task_id TEXT,
				bdinfo_started_at TEXT,
				bdinfo_completed_at TEXT,
				bdinfo_error TEXT,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				PRIMARY KEY (hash, torrent_id, site_name)
			)`,
			`CREATE TABLE IF NOT EXISTS batch_enhance_records (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				title TEXT,
				batch_id TEXT NOT NULL,
				torrent_id TEXT NOT NULL,
				source_site TEXT NOT NULL,
				target_site TEXT NOT NULL,
				video_size_gb REAL,
				status TEXT NOT NULL,
				success_url TEXT,
				error_detail TEXT,
				downloader_add_result TEXT,
				processed_at TEXT DEFAULT CURRENT_TIMESTAMP,
				progress TEXT
			)`,
		}
	}
}

func (m *SchemaManager) columnSpecs() map[string][]schemaColumnSpec {
	return map[string][]schemaColumnSpec{
		"traffic_stats": {
			{name: "stat_datetime", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "DATETIME NOT NULL", "postgresql": "TIMESTAMP NOT NULL"}},
			{name: "downloader_id", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "VARCHAR(64) NOT NULL", "postgresql": "VARCHAR(64) NOT NULL"}},
			{name: "uploaded", definition: map[string]string{"sqlite": "INTEGER DEFAULT 0", "mysql": "BIGINT DEFAULT 0", "postgresql": "BIGINT DEFAULT 0"}},
			{name: "downloaded", definition: map[string]string{"sqlite": "INTEGER DEFAULT 0", "mysql": "BIGINT DEFAULT 0", "postgresql": "BIGINT DEFAULT 0"}},
			{name: "upload_speed", definition: map[string]string{"sqlite": "INTEGER DEFAULT 0", "mysql": "BIGINT DEFAULT 0", "postgresql": "BIGINT DEFAULT 0"}},
			{name: "download_speed", definition: map[string]string{"sqlite": "INTEGER DEFAULT 0", "mysql": "BIGINT DEFAULT 0", "postgresql": "BIGINT DEFAULT 0"}},
			{name: "cumulative_uploaded", definition: map[string]string{"sqlite": "INTEGER NOT NULL DEFAULT 0", "mysql": "BIGINT NOT NULL DEFAULT 0", "postgresql": "BIGINT NOT NULL DEFAULT 0"}},
			{name: "cumulative_downloaded", definition: map[string]string{"sqlite": "INTEGER NOT NULL DEFAULT 0", "mysql": "BIGINT NOT NULL DEFAULT 0", "postgresql": "BIGINT NOT NULL DEFAULT 0"}},
		},
		"traffic_stats_hourly": {
			{name: "stat_datetime", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "DATETIME NOT NULL", "postgresql": "TIMESTAMP NOT NULL"}},
			{name: "downloader_id", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "VARCHAR(64) NOT NULL", "postgresql": "VARCHAR(64) NOT NULL"}},
			{name: "uploaded", definition: map[string]string{"sqlite": "INTEGER DEFAULT 0", "mysql": "BIGINT DEFAULT 0", "postgresql": "BIGINT DEFAULT 0"}},
			{name: "downloaded", definition: map[string]string{"sqlite": "INTEGER DEFAULT 0", "mysql": "BIGINT DEFAULT 0", "postgresql": "BIGINT DEFAULT 0"}},
			{name: "avg_upload_speed", definition: map[string]string{"sqlite": "INTEGER DEFAULT 0", "mysql": "BIGINT DEFAULT 0", "postgresql": "BIGINT DEFAULT 0"}},
			{name: "avg_download_speed", definition: map[string]string{"sqlite": "INTEGER DEFAULT 0", "mysql": "BIGINT DEFAULT 0", "postgresql": "BIGINT DEFAULT 0"}},
			{name: "samples", definition: map[string]string{"sqlite": "INTEGER DEFAULT 0", "mysql": "INTEGER DEFAULT 0", "postgresql": "INTEGER DEFAULT 0"}},
			{name: "cumulative_uploaded", definition: map[string]string{"sqlite": "INTEGER NOT NULL DEFAULT 0", "mysql": "BIGINT NOT NULL DEFAULT 0", "postgresql": "BIGINT NOT NULL DEFAULT 0"}},
			{name: "cumulative_downloaded", definition: map[string]string{"sqlite": "INTEGER NOT NULL DEFAULT 0", "mysql": "BIGINT NOT NULL DEFAULT 0", "postgresql": "BIGINT NOT NULL DEFAULT 0"}},
		},
		"torrents": {
			{name: "hash", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "VARCHAR(64) NOT NULL", "postgresql": "VARCHAR(64) NOT NULL"}},
			{name: "name", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "TEXT NOT NULL", "postgresql": "TEXT NOT NULL"}},
			{name: "save_path", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "size", definition: map[string]string{"sqlite": "INTEGER", "mysql": "BIGINT", "postgresql": "BIGINT"}},
			{name: "progress", definition: map[string]string{"sqlite": "REAL", "mysql": "DOUBLE", "postgresql": "DOUBLE PRECISION"}},
			{name: "state", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(50)", "postgresql": "VARCHAR(50)"}},
			{name: "sites", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(255)", "postgresql": "VARCHAR(255)"}},
			{name: "group", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(255)", "postgresql": "VARCHAR(255)"}},
			{name: "details", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "downloader_id", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "VARCHAR(64) NOT NULL", "postgresql": "VARCHAR(64) NOT NULL"}},
			{name: "last_seen", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "DATETIME NOT NULL", "postgresql": "TIMESTAMP NOT NULL"}},
			{name: "iyuu_last_check", definition: map[string]string{"sqlite": "TEXT NULL", "mysql": "DATETIME NULL", "postgresql": "TIMESTAMP NULL"}},
			{name: "seeders", definition: map[string]string{"sqlite": "INTEGER DEFAULT 0", "mysql": "INT DEFAULT 0", "postgresql": "INTEGER DEFAULT 0"}},
			{name: "is_hidden", definition: map[string]string{"sqlite": "INTEGER NOT NULL DEFAULT 0", "mysql": "TINYINT(1) NOT NULL DEFAULT 0", "postgresql": "INTEGER NOT NULL DEFAULT 0"}},
			{name: "hidden_reason", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(64)", "postgresql": "VARCHAR(64)"}},
			{name: "hidden_at", definition: map[string]string{"sqlite": "TEXT NULL", "mysql": "DATETIME NULL", "postgresql": "TIMESTAMP NULL"}},
		},
		"torrent_upload_stats": {
			{name: "hash", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "VARCHAR(64) NOT NULL", "postgresql": "VARCHAR(64) NOT NULL"}},
			{name: "downloader_id", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "VARCHAR(64) NOT NULL", "postgresql": "VARCHAR(64) NOT NULL"}},
			{name: "uploaded", definition: map[string]string{"sqlite": "INTEGER DEFAULT 0", "mysql": "BIGINT DEFAULT 0", "postgresql": "BIGINT DEFAULT 0"}},
			{name: "is_hidden", definition: map[string]string{"sqlite": "INTEGER NOT NULL DEFAULT 0", "mysql": "TINYINT(1) NOT NULL DEFAULT 0", "postgresql": "INTEGER NOT NULL DEFAULT 0"}},
			{name: "hidden_reason", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(64)", "postgresql": "VARCHAR(64)"}},
			{name: "hidden_at", definition: map[string]string{"sqlite": "TEXT NULL", "mysql": "DATETIME NULL", "postgresql": "TIMESTAMP NULL"}},
		},
		"sites": {
			{name: "site", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(255)", "postgresql": "VARCHAR(255)"}},
			{name: "nickname", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(255)", "postgresql": "VARCHAR(255)"}},
			{name: "base_url", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(255)", "postgresql": "VARCHAR(255)"}},
			{name: "special_tracker_domain", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(255)", "postgresql": "VARCHAR(255)"}},
			{name: "group", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(255)", "postgresql": "VARCHAR(255)"}},
			{name: "description", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(255)", "postgresql": "VARCHAR(255)"}},
			{name: "cookie", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "passkey", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "migration", definition: map[string]string{"sqlite": "INTEGER NOT NULL DEFAULT 1", "mysql": "INT NOT NULL DEFAULT 1", "postgresql": "INTEGER NOT NULL DEFAULT 1"}},
			{name: "speed_limit", definition: map[string]string{"sqlite": "INTEGER NOT NULL DEFAULT 0", "mysql": "INT NOT NULL DEFAULT 0", "postgresql": "INTEGER NOT NULL DEFAULT 0"}},
			{name: "ratio_threshold", definition: map[string]string{"sqlite": "REAL NOT NULL DEFAULT 3.0", "mysql": "DOUBLE NOT NULL DEFAULT 3.0", "postgresql": "DOUBLE PRECISION NOT NULL DEFAULT 3.0"}},
			{name: "seed_speed_limit", definition: map[string]string{"sqlite": "INTEGER NOT NULL DEFAULT 5", "mysql": "INT NOT NULL DEFAULT 5", "postgresql": "INTEGER NOT NULL DEFAULT 5"}},
		},
		"seed_parameters": {
			{name: "hash", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "VARCHAR(64) NOT NULL", "postgresql": "VARCHAR(64) NOT NULL"}},
			{name: "torrent_id", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "VARCHAR(255) NOT NULL", "postgresql": "VARCHAR(255) NOT NULL"}},
			{name: "site_name", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "VARCHAR(255) NOT NULL", "postgresql": "VARCHAR(255) NOT NULL"}},
			{name: "nickname", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(255)", "postgresql": "VARCHAR(255)"}},
			{name: "name", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "title", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "subtitle", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "imdb_link", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "douban_link", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "tmdb_link", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "type", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(100)", "postgresql": "VARCHAR(100)"}},
			{name: "medium", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(100)", "postgresql": "VARCHAR(100)"}},
			{name: "video_codec", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(100)", "postgresql": "VARCHAR(100)"}},
			{name: "audio_codec", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(100)", "postgresql": "VARCHAR(100)"}},
			{name: "resolution", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(100)", "postgresql": "VARCHAR(100)"}},
			{name: "team", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(100)", "postgresql": "VARCHAR(100)"}},
			{name: "source", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(100)", "postgresql": "VARCHAR(100)"}},
			{name: "tags", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "poster", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "screenshots", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "statement", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "body", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "mediainfo", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "title_components", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "removed_ardtudeclarations", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "is_reviewed", definition: map[string]string{"sqlite": "INTEGER NOT NULL DEFAULT 0", "mysql": "TINYINT(1) NOT NULL DEFAULT 0", "postgresql": "BOOLEAN NOT NULL DEFAULT FALSE"}},
			{name: "mediainfo_status", definition: map[string]string{"sqlite": "TEXT DEFAULT 'pending'", "mysql": "VARCHAR(20) DEFAULT 'pending'", "postgresql": "VARCHAR(20) DEFAULT 'pending'"}},
			{name: "bdinfo_task_id", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(36)", "postgresql": "VARCHAR(36)"}},
			{name: "bdinfo_started_at", definition: map[string]string{"sqlite": "TEXT", "mysql": "DATETIME", "postgresql": "TIMESTAMP"}},
			{name: "bdinfo_completed_at", definition: map[string]string{"sqlite": "TEXT", "mysql": "DATETIME", "postgresql": "TIMESTAMP"}},
			{name: "bdinfo_error", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "created_at", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "DATETIME NOT NULL", "postgresql": "TIMESTAMP NOT NULL"}},
			{name: "updated_at", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "DATETIME NOT NULL", "postgresql": "TIMESTAMP NOT NULL"}},
		},
		"batch_enhance_records": {
			{name: "title", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "batch_id", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "VARCHAR(255) NOT NULL", "postgresql": "VARCHAR(255) NOT NULL"}},
			{name: "torrent_id", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "VARCHAR(255) NOT NULL", "postgresql": "VARCHAR(255) NOT NULL"}},
			{name: "source_site", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "VARCHAR(255) NOT NULL", "postgresql": "VARCHAR(255) NOT NULL"}},
			{name: "target_site", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "VARCHAR(255) NOT NULL", "postgresql": "VARCHAR(255) NOT NULL"}},
			{name: "video_size_gb", definition: map[string]string{"sqlite": "REAL", "mysql": "DECIMAL(8,2)", "postgresql": "DECIMAL(8,2)"}},
			{name: "status", definition: map[string]string{"sqlite": "TEXT NOT NULL", "mysql": "VARCHAR(50) NOT NULL", "postgresql": "VARCHAR(50) NOT NULL"}},
			{name: "success_url", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "error_detail", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "downloader_add_result", definition: map[string]string{"sqlite": "TEXT", "mysql": "TEXT", "postgresql": "TEXT"}},
			{name: "processed_at", definition: map[string]string{"sqlite": "TEXT DEFAULT CURRENT_TIMESTAMP", "mysql": "DATETIME DEFAULT CURRENT_TIMESTAMP", "postgresql": "TIMESTAMP DEFAULT CURRENT_TIMESTAMP"}},
			{name: "progress", definition: map[string]string{"sqlite": "TEXT", "mysql": "VARCHAR(20)", "postgresql": "VARCHAR(20)"}},
		},
	}
}

func (m *SchemaManager) indexSpecs() []schemaIndexSpec {
	return []schemaIndexSpec{
		{table: "batch_enhance_records", name: "idx_batch_records_batch_id", columns: []string{"batch_id"}},
		{table: "batch_enhance_records", name: "idx_batch_records_torrent_id", columns: []string{"torrent_id"}},
		{table: "batch_enhance_records", name: "idx_batch_records_status", columns: []string{"status"}},
		{table: "batch_enhance_records", name: "idx_batch_records_processed_at", columns: []string{"processed_at"}},
	}
}

func (m *SchemaManager) ensureTableColumns(tableName string, specs []schemaColumnSpec) error {
	columnSet, err := m.listColumnSet(tableName)
	if err != nil {
		return fmt.Errorf("读取表字段失败 table=%s err=%w", tableName, err)
	}

	for _, spec := range specs {
		if _, exists := columnSet[strings.ToLower(spec.name)]; exists {
			continue
		}
		definition, ok := spec.definition[m.store.DBType]
		if !ok || strings.TrimSpace(definition) == "" {
			definition = spec.definition["sqlite"]
		}
		if strings.TrimSpace(definition) == "" {
			return fmt.Errorf("字段定义缺失 table=%s column=%s db=%s", tableName, spec.name, m.store.DBType)
		}

		sqlText := fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s %s",
			quoteIdentifierByDB(m.store.DBType, tableName),
			quoteIdentifierByDB(m.store.DBType, spec.name),
			definition,
		)
		if err := m.store.DB.Exec(sqlText).Error; err != nil {
			return fmt.Errorf("补齐字段失败 table=%s column=%s err=%w", tableName, spec.name, err)
		}
		logx.Infof(schemaLogModule, "补齐缺失字段 table=%s column=%s", tableName, spec.name)
	}
	return nil
}

func (m *SchemaManager) listColumnSet(tableName string) (map[string]struct{}, error) {
	result := map[string]struct{}{}

	switch m.store.DBType {
	case "mysql":
		// NOTE: MySQL driver returns metadata column names as "COLUMN_NAME" (upper-case).
		// Use an explicit alias + named struct so GORM scans into the field instead of
		// attempting to scan into the struct itself.
		type columnNameRow struct {
			ColumnName string `gorm:"column:column_name"`
		}
		rows := make([]columnNameRow, 0)
		if err := m.store.DB.Raw(
			`SELECT COLUMN_NAME AS column_name
			 FROM information_schema.columns
			 WHERE table_schema = DATABASE()
			   AND table_name = ?`,
			tableName,
		).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[strings.ToLower(strings.TrimSpace(row.ColumnName))] = struct{}{}
		}
	case "postgresql":
		type columnNameRow struct {
			ColumnName string `gorm:"column:column_name"`
		}
		rows := make([]columnNameRow, 0)
		if err := m.store.DB.Raw(
			`SELECT column_name
			 FROM information_schema.columns
			 WHERE table_schema = CURRENT_SCHEMA()
			   AND table_name = ?`,
			tableName,
		).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[strings.ToLower(strings.TrimSpace(row.ColumnName))] = struct{}{}
		}
	default:
		if !isSafeIdentifier(tableName) {
			return nil, fmt.Errorf("非法表名: %s", tableName)
		}
		rows := make([]struct {
			Name string `gorm:"column:name"`
		}, 0)
		pragmaSQL := fmt.Sprintf("PRAGMA table_info(%s)", quoteIdentifierByDB("sqlite", tableName))
		if err := m.store.DB.Raw(pragmaSQL).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[strings.ToLower(strings.TrimSpace(row.Name))] = struct{}{}
		}
	}
	return result, nil
}

func (m *SchemaManager) ensureIndex(spec schemaIndexSpec) error {
	if len(spec.columns) == 0 {
		return nil
	}

	columnList := make([]string, 0, len(spec.columns))
	for _, column := range spec.columns {
		columnList = append(columnList, quoteIdentifierByDB(m.store.DBType, column))
	}

	switch m.store.DBType {
	case "mysql":
		exists, err := m.mysqlIndexExists(spec.table, spec.name)
		if err != nil {
			return fmt.Errorf("检查索引失败 table=%s index=%s err=%w", spec.table, spec.name, err)
		}
		if exists {
			return nil
		}
		sqlText := fmt.Sprintf(
			"CREATE INDEX %s ON %s(%s)",
			quoteIdentifierByDB(m.store.DBType, spec.name),
			quoteIdentifierByDB(m.store.DBType, spec.table),
			strings.Join(columnList, ", "),
		)
		if err := m.store.DB.Exec(sqlText).Error; err != nil {
			return fmt.Errorf("创建索引失败 table=%s index=%s err=%w", spec.table, spec.name, err)
		}
	default:
		sqlText := fmt.Sprintf(
			"CREATE INDEX IF NOT EXISTS %s ON %s(%s)",
			quoteIdentifierByDB(m.store.DBType, spec.name),
			quoteIdentifierByDB(m.store.DBType, spec.table),
			strings.Join(columnList, ", "),
		)
		if err := m.store.DB.Exec(sqlText).Error; err != nil {
			return fmt.Errorf("创建索引失败 table=%s index=%s err=%w", spec.table, spec.name, err)
		}
	}
	return nil
}

func (m *SchemaManager) mysqlIndexExists(tableName, indexName string) (bool, error) {
	rows := make([]struct {
		Count int64 `gorm:"column:cnt"`
	}, 0)
	if err := m.store.DB.Raw(
		`SELECT COUNT(1) AS cnt
		 FROM information_schema.statistics
		 WHERE table_schema = DATABASE()
		   AND table_name = ?
		   AND index_name = ?`,
		tableName,
		indexName,
	).Scan(&rows).Error; err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	return rows[0].Count > 0, nil
}

func quoteIdentifierByDB(dbType string, name string) string {
	switch dbType {
	case "postgresql":
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	default:
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
}

func isSafeIdentifier(identifier string) bool {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return false
	}
	for _, ch := range trimmed {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return false
	}
	return true
}
