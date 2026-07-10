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

func (r *LocalQueryRepository) DistinctPaths(videoOnly bool) ([]string, error) {
	rows := make([]string, 0)
	query := `
		SELECT DISTINCT t.save_path
		FROM torrents t
		WHERE t.save_path IS NOT NULL AND TRIM(t.save_path) != '' AND (t.is_hidden = 0 OR t.is_hidden IS NULL)
	`
	if videoOnly {
		query += " AND " + videoTorrentExistsCondition("t")
	}
	query += " ORDER BY t.save_path"
	err := r.store.DB.Raw(query).Pluck("save_path", &rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *LocalQueryRepository) DownloaderPathCounts(downloaderID string, videoOnly bool) ([]LocalPathCount, error) {
	rows := make([]LocalPathCount, 0)
	query := `
		SELECT t.save_path, COUNT(*) AS count
		FROM torrents t
		WHERE t.downloader_id = ? AND t.save_path IS NOT NULL AND TRIM(t.save_path) != '' AND (t.is_hidden = 0 OR t.is_hidden IS NULL)
	`
	if videoOnly {
		query += " AND " + videoTorrentExistsCondition("t")
	}
	query += " GROUP BY t.save_path ORDER BY t.save_path"
	err := r.store.DB.Raw(query, downloaderID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *LocalQueryRepository) ListTorrents(path string, videoOnly bool) ([]LocalTorrentRecord, error) {
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
	if videoOnly {
		query += " AND " + videoTorrentExistsCondition("torrents")
	}
	if err := r.store.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *LocalQueryRepository) ListDuplicateNames(videoOnly bool) ([]DuplicateNameRow, error) {
	rows := make([]DuplicateNameRow, 0)
	query := `
		SELECT name, COUNT(*) AS count
		FROM torrents t
		WHERE t.name IS NOT NULL AND TRIM(t.name) != '' AND (t.is_hidden = 0 OR t.is_hidden IS NULL)
	`
	if videoOnly {
		query += " AND " + videoTorrentExistsCondition("t")
	}
	query += `
		GROUP BY name
		HAVING count > 1
		ORDER BY count DESC`
	err := r.store.DB.Raw(query).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *LocalQueryRepository) ListTorrentsByName(name string, videoOnly bool) ([]LocalTorrentRecord, error) {
	rows := make([]LocalTorrentRecord, 0)
	query := `
		SELECT hash, name, save_path, size, downloader_id
		FROM torrents t
		WHERE t.name = ? AND (t.is_hidden = 0 OR t.is_hidden IS NULL)
	`
	if videoOnly {
		query += " AND " + videoTorrentExistsCondition("t")
	}
	err := r.store.DB.Raw(query, name).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
