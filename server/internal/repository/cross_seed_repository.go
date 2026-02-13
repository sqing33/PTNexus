package repository

import "gorm.io/gorm"

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
