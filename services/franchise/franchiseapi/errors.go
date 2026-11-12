package franchiseapi

import "errors"

// ErrNotFound is returned when a store does not exist or belongs to another
// business (another business's store is not found, not forbidden), so
// callers can tell "no such store" from a failure.
var ErrNotFound = errors.New("not found")
