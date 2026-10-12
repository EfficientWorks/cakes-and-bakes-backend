package category

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

func scanCategoryAdmin(row pgx.Row) (*catalogapi.Category, *catalogapi.CategoryWrite, error) {
	var c catalogapi.Category
	var w catalogapi.CategoryWrite
	err := row.Scan(&c.ID, &c.Name, &c.Slug, &c.Description, &c.ImageURL, &c.ImageAltText,
		&c.SortOrder, &c.IsActive, &w.AvailableFrom, &w.AvailableUntil, &c.ColorCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, shared.ErrNotFound
	}
	w.ColorCode = c.ColorCode
	return &c, &w, err
}
