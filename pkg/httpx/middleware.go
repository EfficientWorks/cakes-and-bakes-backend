package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"bakehouse/pkg/obs"
)

// RequestID assigns a request ID — chi's own generator if the standard
// middleware.RequestID ran first (it does, in NewRouter), otherwise a fresh
// random one — and stores it on the context under the obs package's key so
// WriteError can surface it.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := middleware.GetReqID(r.Context())
		if id == "" {
			id = newRandomID()
		}
		ctx := obs.WithRequestID(r.Context(), id)
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRandomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Logger logs one structured line per request.
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", obs.RequestIDFromContext(r.Context()),
			)
		})
	}
}

// CORS applies the configured cross-origin policy.
//
// Setting CORS_ALLOWED_ORIGINS to "*" allows every origin. Note that the
// literal header value stays the caller's own origin rather than "*": a
// browser rejects the wildcard whenever the response also carries
// Access-Control-Allow-Credentials, so reflecting the origin is the only form
// of "allow everything" that actually works.
//
// This API authenticates with a Bearer token in a header, not with cookies, so
// a hostile page cannot ride an existing session the way it could with cookie
// auth — it has no way to read a token held in another origin's storage. That
// is what makes the open policy tolerable here; it would not be with cookies.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = true
	}
	allowAll := allowed["*"]

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (allowAll || allowed[origin]) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,PUT,DELETE,OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,Idempotency-Key,X-Branch-Id,X-Device-Id,X-Cart-Token")
				// Without this, browser JavaScript cannot read either header.
				// X-Cart-Token is how a guest cart is handed back to the
				// client, so guest checkout is broken cross-origin without it.
				w.Header().Set("Access-Control-Expose-Headers", "X-Cart-Token,X-Request-Id")
				// Cache the preflight so every mutation is not preceded by a
				// second round trip.
				w.Header().Set("Access-Control-Max-Age", "600")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
