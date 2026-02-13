package repository

import (
	"database/sql"
	"fmt"
)

func rowsToMaps(rows *sql.Rows) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read columns failed: %w", err)
	}

	result := make([]map[string]any, 0)
	for rows.Next() {
		raw := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for idx := range raw {
			pointers[idx] = &raw[idx]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, fmt.Errorf("scan row failed: %w", err)
		}
		item := map[string]any{}
		for idx, col := range columns {
			value := raw[idx]
			if bytes, ok := value.([]byte); ok {
				item[col] = string(bytes)
				continue
			}
			item[col] = value
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows failed: %w", err)
	}
	return result, nil
}
