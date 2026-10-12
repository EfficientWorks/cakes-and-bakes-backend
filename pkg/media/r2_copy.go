package media

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CopyReport says what CopyDatabaseToR2 moved.
type CopyReport struct {
	Images int
	Videos int
}

// CopyDatabaseToR2 copies every image (media_objects) and video
// (media_videos) stored in Postgres into the R2 bucket under the same ids,
// so images/<id> and videos/<id> serve exactly what /v1/media/<id> served.
//
// Images are copied as stored: they were already checked and resized on the
// way in. Rows are read one at a time, so a large video library does not have
// to fit in memory. Re-running is safe; objects are simply overwritten.
// Nothing is deleted from the database.
func CopyDatabaseToR2(ctx context.Context, pool *pgxpool.Pool, cfg R2Config, dryRun bool) (CopyReport, error) {
	var report CopyReport
	bucket := newR2Bucket(cfg)

	for _, kind := range []struct {
		table string
		key   func(string) string
		count *int
	}{
		{"media_objects", imageKey, &report.Images},
		{"media_videos", videoKey, &report.Videos},
	} {
		ids, err := listIDs(ctx, pool, kind.table)
		if err != nil {
			return report, err
		}
		for _, id := range ids {
			if !dryRun {
				var contentType string
				var data []byte
				if err := pool.QueryRow(ctx,
					`SELECT content_type, bytes FROM `+kind.table+` WHERE id = $1`, id).Scan(&contentType, &data); err != nil {
					return report, fmt.Errorf("media: read %s %s: %w", kind.table, id, err)
				}
				if err := bucket.put(ctx, kind.key(id), contentType, data); err != nil {
					return report, err
				}
			}
			*kind.count++
		}
	}
	return report, nil
}

func listIDs(ctx context.Context, pool *pgxpool.Pool, table string) ([]string, error) {
	rows, err := pool.Query(ctx, `SELECT id FROM `+table+` ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("media: list %s: %w", table, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
