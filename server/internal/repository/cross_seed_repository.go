package repository

import (
	"strings"

	"gorm.io/gorm"
)

type CrossSeedRepository struct {
	store *Store
}

func NewCrossSeedRepository(store *Store) *CrossSeedRepository {
	return &CrossSeedRepository{store: store}
}

func (r *CrossSeedRepository) DB() *gorm.DB {
	return r.store.DB
}

func (r *CrossSeedRepository) DBType() string {
	return r.store.DBType
}

func (r *CrossSeedRepository) RawMaps(query string, args ...any) ([]map[string]any, error) {
	rows := make([]map[string]any, 0)
	if err := r.store.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *CrossSeedRepository) RawStrings(query, column string, args ...any) ([]string, error) {
	values := make([]string, 0)
	if err := r.store.DB.Raw(query, args...).Pluck(column, &values).Error; err != nil {
		return nil, err
	}
	return values, nil
}

func (r *CrossSeedRepository) Exec(query string, args ...any) (int64, error) {
	result := r.store.DB.Exec(query, args...)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (r *CrossSeedRepository) ResolveSiteAliases(siteName string) ([]string, error) {
	if r == nil || r.store == nil || r.store.DB == nil {
		return []string{}, nil
	}

	trimmed := strings.TrimSpace(siteName)
	if trimmed == "" {
		return []string{}, nil
	}

	type siteAliasRow struct {
		Site     string `gorm:"column:site"`
		Nickname string `gorm:"column:nickname"`
	}

	rows := make([]siteAliasRow, 0)
	query := `SELECT site, nickname FROM sites WHERE LOWER(nickname) = LOWER(?) OR LOWER(site) = LOWER(?)`
	if err := r.store.DB.Raw(query, trimmed, trimmed).Scan(&rows).Error; err != nil {
		return nil, err
	}

	aliases := make([]string, 0, len(rows)*2+1)
	seen := map[string]struct{}{}
	appendAlias := func(value string) {
		name := strings.TrimSpace(value)
		if name == "" {
			return
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		aliases = append(aliases, name)
	}

	appendAlias(trimmed)
	for _, row := range rows {
		appendAlias(row.Nickname)
		appendAlias(row.Site)
	}

	return aliases, nil
}
