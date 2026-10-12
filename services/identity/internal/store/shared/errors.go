package shared

import "errors"

var ErrNotFound = errors.New("not found")

// ErrEmailTaken: another customer already has this email.
var ErrEmailTaken = errors.New("email already belongs to another customer")
