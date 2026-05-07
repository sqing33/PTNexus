package repository

type LocalPathCount struct {
	SavePath string
	Count    int64
}

type LocalTorrentRecord struct {
	Hash         string
	Name         string
	SavePath     string
	Size         int64
	DownloaderID string
}

type DuplicateNameRow struct {
	Name  string
	Count int64
}

type LocalQueryRepository struct {
	store *Store
}

func NewLocalQueryRepository(store *Store) *LocalQueryRepository {
	return &LocalQueryRepository{store: store}
}

func (r *LocalQueryRepository) DistinctPaths() ([]string, error) {
	rows := make([]string, 0)
	err := r.store.DB.Raw(`
		SELECT DISTINCT t.save_path
		FROM torrents t
		WHERE t.save_path IS NOT NULL AND TRIM(t.save_path) != '' AND (t.is_hidden = 0 OR t.is_hidden IS NULL)
		  AND EXISTS (
			SELECT 1 FROM seed_parameters sp
			WHERE sp.hash = t.hash
			  AND sp.type IN ('category.movie', 'category.tv_series', 'category.animation', 'category.documentaries', 'category.tv_shows')
		  )
		ORDER BY t.save_path
	`).Pluck("save_path", &rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *LocalQueryRepository) DownloaderPathCounts(downloaderID string) ([]LocalPathCount, error) {
	rows := make([]LocalPathCount, 0)
	err := r.store.DB.Raw(`
		SELECT t.save_path, COUNT(*) AS count
		FROM torrents t
		WHERE t.downloader_id = ? AND t.save_path IS NOT NULL AND TRIM(t.save_path) != '' AND (t.is_hidden = 0 OR t.is_hidden IS NULL)
		  AND EXISTS (
			SELECT 1 FROM seed_parameters sp
			WHERE sp.hash = t.hash
			  AND sp.type IN ('category.movie', 'category.tv_series', 'category.animation', 'category.documentaries', 'category.tv_shows')
		  )
		GROUP BY t.save_path
		ORDER BY t.save_path
	`, downloaderID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *LocalQueryRepository) ListTorrents(path string) ([]LocalTorrentRecord, error) {
	rows := make([]LocalTorrentRecord, 0)
	query := `
		SELECT hash, name, save_path, size, downloader_id
		FROM torrents
		WHERE save_path IS NOT NULL AND TRIM(save_path) != '' AND (is_hidden = 0 OR is_hidden IS NULL)
	`
	args := []any{}
	if path != "" {
		query += " AND save_path = ?"
		args = append(args, path)
	}
	if err := r.store.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *LocalQueryRepository) ListDuplicateNames() ([]DuplicateNameRow, error) {
	rows := make([]DuplicateNameRow, 0)
	err := r.store.DB.Raw(`
		SELECT name, COUNT(*) AS count
		FROM torrents
		WHERE name IS NOT NULL AND TRIM(name) != '' AND (is_hidden = 0 OR is_hidden IS NULL)
		GROUP BY name
		HAVING count > 1
		ORDER BY count DESC
	`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *LocalQueryRepository) ListTorrentsByName(name string) ([]LocalTorrentRecord, error) {
	rows := make([]LocalTorrentRecord, 0)
	err := r.store.DB.Raw(`
		SELECT hash, name, save_path, size, downloader_id
		FROM torrents
		WHERE name = ? AND (is_hidden = 0 OR is_hidden IS NULL)
	`, name).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
