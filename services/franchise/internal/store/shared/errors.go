package shared

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"bakehouse/services/franchise/franchiseapi"
)

// ErrNotFound is franchiseapi.ErrNotFound, so other services can match it.
var ErrNotFound = franchiseapi.ErrNotFound

// IsDuplicateKey reports whether an error is a unique-constraint violation.
//
// Exposed so the service can turn "that code is taken" into a message naming
// the field, instead of the 500 a raw constraint error becomes.
func IsDuplicateKey(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
