// Package shared holds what more than one business-store topic needs.
package shared

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrNotFound reports a business id that names no business.
	ErrNotFound = errors.New("store: not found")
	// ErrDuplicate reports a slug another business already uses.
	ErrDuplicate = errors.New("store: already exists")
	// ErrWebsiteTaken reports a website another business already uses.
	ErrWebsiteTaken = errors.New("store: website already in use")
)

// Classify turns a unique-key violation into ErrDuplicate or
// ErrWebsiteTaken.
func Classify(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		if pg.ConstraintName == "businesses_website_url_key" {
			return ErrWebsiteTaken
		}
		return ErrDuplicate
	}
	return err
}
