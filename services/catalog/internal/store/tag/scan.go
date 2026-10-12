package tag

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

func scanTag(row pgx.Row) (*catalogapi.SeasonalTag, error) {
	var t catalogapi.SeasonalTag
	err := row.Scan(&t.ID, &t.Name, &t.Slug, &t.Description, &t.Status, &t.Restricted, &t.RestrictedFrom, &t.RestrictedTo,
		&t.RepeatYearly, &t.CreatedAt, &t.UpdatedAt, &t.ProductCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	return &t, err
}

func scanTags(rows pgx.Rows) ([]catalogapi.SeasonalTag, error) {
	defer rows.Close()
	out := []catalogapi.SeasonalTag{}
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}
