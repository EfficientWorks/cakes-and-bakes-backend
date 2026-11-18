package flavour

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

func scanFlavour(row pgx.Row) (*catalogapi.Flavour, error) {
	var f catalogapi.Flavour
	err := row.Scan(&f.ID, &f.Name, &f.Slug, &f.Description, &f.ImageURL, &f.ImageAltText, &f.BackgroundColour,
		&f.SortOrder, &f.Status, &f.CreatedAt, &f.UpdatedAt, &f.ProductCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	return &f, err
}

func scanFlavours(rows pgx.Rows) ([]catalogapi.Flavour, error) {
	defer rows.Close()
	out := []catalogapi.Flavour{}
	for rows.Next() {
		f, err := scanFlavour(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}
