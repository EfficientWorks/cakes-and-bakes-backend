package content

import (
	"context"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/strx"
	"bakehouse/services/catalog/catalogapi"
)

func (s *Repo) GetContent(ctx context.Context, keys []string) ([]catalogapi.ContentItem, error) {
	var rows pgx.Rows
	var err error
	if len(keys) == 0 {
		rows, err = s.pool.Query(ctx, `SELECT key, value, type, updated_at FROM site_content ORDER BY key`)
	} else {
		rows, err = s.pool.Query(ctx, `SELECT key, value, type, updated_at FROM site_content WHERE key = ANY($1) ORDER BY key`, keys)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []catalogapi.ContentItem
	for rows.Next() {
		var c catalogapi.ContentItem
		if err := rows.Scan(&c.Key, &c.Value, &c.Type, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Repo) GetContentByKey(ctx context.Context, key string) (*catalogapi.ContentItem, error) {
	var item catalogapi.ContentItem

	err := s.pool.QueryRow(ctx, `
		SELECT key, value, type, updated_at
		FROM site_content
		WHERE key = $1
	`, key).Scan(
		&item.Key,
		&item.Value,
		&item.Type,
		&item.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &item, nil
}

func (s *Repo) SetContent(ctx context.Context, key, value, contentType, updatedBy string) error {
	const q = `
		INSERT INTO site_content (key, value, type, updated_by, updated_at)
		VALUES ($1,$2,$3,$4,NOW())
		ON CONFLICT (key) DO UPDATE SET value = $2, type = $3, updated_by = $4, updated_at = NOW()`
	_, err := s.pool.Exec(ctx, q, key, value, strx.OrDefault(contentType, "text"), strx.NilIfEmpty(updatedBy))
	return err
}
