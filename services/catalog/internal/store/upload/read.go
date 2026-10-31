package upload

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/internal/store/shared"
)

// CustomerPhotoURL returns the stored url for an upload named by its id or
// its url, within the request's business.
func (s *Repo) CustomerPhotoURL(ctx context.Context, ref string) (string, error) {
	var url string
	err := s.pool.QueryRow(ctx, `
		SELECT url FROM customer_photo_uploads
		 WHERE (id = $2 OR url = $2) AND business_id IS NOT DISTINCT FROM $1`,
		tenant.BusinessParam(ctx), ref).Scan(&url)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", shared.ErrNotFound
	}
	return url, err
}
