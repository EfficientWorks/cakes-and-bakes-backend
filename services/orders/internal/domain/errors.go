package domain

import (
	"errors"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/services/orders/ordersapi"
)

func notFoundOrInternal(err error, msg string) error {
	if errors.Is(err, ordersapi.ErrNotFound) {
		return apperr.NotFound(msg)
	}
	return apperr.Internal(err)
}

// isUniqueViolation recognises Postgres 23505 without importing the driver's
// error type into the domain layer (same approach as the catalog module).
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
}
