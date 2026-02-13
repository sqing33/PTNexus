package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pt-nexus/server-go/internal/config"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Store struct {
	DB     *gorm.DB
	DBType string
}

func NewStore(paths config.RuntimePaths) (*Store, error) {
	dbType := strings.ToLower(strings.TrimSpace(os.Getenv("DB_TYPE")))
	desktopCfg := config.DatabaseConfig{}
	if dbType == "" {
		loadedCfg, found, loadErr := config.LoadDesktopDatabaseConfig(paths)
		if loadErr != nil {
			return nil, loadErr
		}
		if found {
			desktopCfg = loadedCfg
			if loadedCfg.Type != "" {
				dbType = loadedCfg.Type
			}
		}
	}
	if dbType == "" {
		dbType = "sqlite"
	}

	var (
		db  *gorm.DB
		err error
	)

	switch dbType {
	case "mysql":
		host := firstNonEmpty(os.Getenv("MYSQL_HOST"), desktopCfg.MySQL.Host)
		user := firstNonEmpty(os.Getenv("MYSQL_USER"), desktopCfg.MySQL.User)
		password := firstNonEmpty(os.Getenv("MYSQL_PASSWORD"), desktopCfg.MySQL.Password)
		database := firstNonEmpty(os.Getenv("MYSQL_DATABASE"), desktopCfg.MySQL.Database)
		port := firstNonEmpty(os.Getenv("MYSQL_PORT"), intToString(desktopCfg.MySQL.Port), "3306")
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", user, password, host, port, database)
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	case "postgresql":
		host := firstNonEmpty(os.Getenv("POSTGRES_HOST"), desktopCfg.PostgreSQL.Host)
		user := firstNonEmpty(os.Getenv("POSTGRES_USER"), desktopCfg.PostgreSQL.User)
		password := firstNonEmpty(os.Getenv("POSTGRES_PASSWORD"), desktopCfg.PostgreSQL.Password)
		database := firstNonEmpty(os.Getenv("POSTGRES_DATABASE"), desktopCfg.PostgreSQL.Database)
		port := firstNonEmpty(os.Getenv("POSTGRES_PORT"), intToString(desktopCfg.PostgreSQL.Port), "5432")
		sslMode := firstNonEmpty(os.Getenv("POSTGRES_SSLMODE"), desktopCfg.PostgreSQL.SSLMode, "disable")
		dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=Asia/Shanghai", host, user, password, database, port, sslMode)
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	case "sqlite":
		fallthrough
	default:
		dbType = "sqlite"
		dbPath := filepath.Join(paths.DataDir, "pt_stats.db")
		if value := strings.TrimSpace(os.Getenv("SQLITE_PATH")); value != "" {
			dbPath = value
		} else if value := strings.TrimSpace(desktopCfg.SQLitePath); value != "" {
			dbPath = value
		}
		db, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	}

	if err != nil {
		return nil, fmt.Errorf("open database failed: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB failed: %w", err)
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(10)

	return &Store{DB: db, DBType: dbType}, nil
}

func (s *Store) GroupColumn() string {
	if s.DBType == "postgresql" {
		return `"group"`
	}
	return "`group`"
}

func getEnvOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func intToString(value int) string {
	if value <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", value)
}
