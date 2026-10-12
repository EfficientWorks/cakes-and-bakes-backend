// Package obs holds cross-cutting observability helpers: request IDs and a
// structured logger. Sentry/OpenTelemetry wiring plugs in here later without
// touching call sites.
package obs

import (
	"context"
	"log/slog"
	"os"
)

type ctxKey int

const requestIDKey ctxKey = iota

// NewLogger returns the process-wide structured JSON logger.
func NewLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

// WithRequestID returns a context carrying the given request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFromContext extracts the request ID, or "" if absent.
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}
