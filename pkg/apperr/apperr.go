// Package apperr defines the canonical application error carried across
// layers and its category -> transport mapping.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Category classifies an error and fixes its HTTP status mapping.
type Category string

const (
	CategoryValidation     Category = "validation"
	CategoryAuthentication Category = "authentication"
	CategoryAuthorization  Category = "authorization"
	CategoryNotFound       Category = "not_found"
	// CategoryMethodNotAllowed is the route existing but not for this verb.
	// Distinct from not_found because it means the opposite thing to a
	// caller: the path is right and the method is wrong.
	CategoryMethodNotAllowed Category = "method_not_allowed"
	CategoryConflict         Category = "conflict"
	CategoryBusinessRule     Category = "business_rule"
	CategoryRateLimited      Category = "rate_limited"
	CategoryUpstream         Category = "upstream"
	CategoryInternal         Category = "internal"
)

// HTTPStatus returns the default HTTP status for a category.
func (c Category) HTTPStatus() int {
	switch c {
	case CategoryValidation:
		return http.StatusBadRequest
	case CategoryAuthentication:
		return http.StatusUnauthorized
	case CategoryAuthorization:
		return http.StatusForbidden
	case CategoryNotFound:
		return http.StatusNotFound
	case CategoryMethodNotAllowed:
		return http.StatusMethodNotAllowed
	case CategoryConflict:
		return http.StatusConflict
	case CategoryBusinessRule:
		return http.StatusUnprocessableEntity
	case CategoryRateLimited:
		return http.StatusTooManyRequests
	case CategoryUpstream:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// FieldError is a single field-level validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error is the canonical application error returned by every layer.
type Error struct {
	Code     string
	Category Category
	Message  string
	Fields   []FieldError
	// Meta carries structured data the client needs in order to act on the
	// error, as opposed to merely display it — a 3-D Secure client secret,
	// say, without which the customer simply cannot continue.
	//
	// Only ever set deliberately. It is serialised to the client verbatim, so
	// nothing internal belongs here.
	Meta  map[string]any
	cause error
}

// WithMeta attaches data the client needs to recover from this error.
func (e *Error) WithMeta(meta map[string]any) *Error {
	e.Meta = meta
	return e
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.cause)
	}
	return e.Code
}

// Unwrap exposes the wrapped cause for errors.Is/As.
func (e *Error) Unwrap() error { return e.cause }

// New builds an Error with no cause.
func New(code string, cat Category, msg string) *Error {
	return &Error{Code: code, Category: cat, Message: msg}
}

// Wrap attaches a cause to a new Error.
func Wrap(cause error, code string, cat Category, msg string) *Error {
	return &Error{Code: code, Category: cat, Message: msg, cause: cause}
}

// WithFields attaches field-level validation errors.
func (e *Error) WithFields(f ...FieldError) *Error {
	e.Fields = append(e.Fields, f...)
	return e
}

// As extracts an *Error from any error in the chain.
func As(err error) (*Error, bool) {
	var ae *Error
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}

// --- Cross-cutting constructors used throughout the codebase ---

func Validation(msg string) *Error      { return New("VALIDATION_FAILED", CategoryValidation, msg) }
func Unauthenticated(msg string) *Error { return New("UNAUTHENTICATED", CategoryAuthentication, msg) }
func PermissionDenied(msg string) *Error {
	return New("PERMISSION_DENIED", CategoryAuthorization, msg)
}
func NotFound(msg string) *Error    { return New("NOT_FOUND", CategoryNotFound, msg) }
func Conflict(msg string) *Error    { return New("CONFLICT", CategoryConflict, msg) }
func RateLimited(msg string) *Error { return New("RATE_LIMITED", CategoryRateLimited, msg) }
func BusinessRule(code, msg string) *Error {
	return New(code, CategoryBusinessRule, msg)
}

// Internal wraps an unexpected fault; the cause is logged, never returned to the client.
func Internal(cause error) *Error {
	return Wrap(cause, "INTERNAL_ERROR", CategoryInternal, "An unexpected error occurred.")
}
