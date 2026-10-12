package shared

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// Postgres error codes worth telling apart from "something went wrong".
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
	pgInvalidTextRepr     = "22P02" // e.g. a non-UUID where a UUID is expected
)

var ErrNotFound = errors.New("not found")

var (
	// ErrDuplicate reports a row that collides with one already stored — a
	// product slug reused, a branch code taken.
	ErrDuplicate = errors.New("store: already exists")
	// ErrRelatedMissing reports a foreign key pointing at nothing: a category
	// id that names no category.
	ErrRelatedMissing = errors.New("store: related record not found")
	// ErrMalformedID reports an id the database cannot even parse.
	ErrMalformedID = errors.New("store: malformed id")
)

// Classify turns a Postgres constraint failure into one of the errors above.
//
// Without this every one of them surfaced as "an unexpected error occurred" —
// which is exactly wrong for a duplicate slug or a missing category, because
// both are things the person filling the form can fix in five seconds if
// anyone tells them.
func Classify(err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.Code {
	case pgUniqueViolation:
		return ErrDuplicate
	case pgForeignKeyViolation:
		return ErrRelatedMissing
	case pgInvalidTextRepr:
		return ErrMalformedID
	default:
		return err
	}
}
