package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequirePlatformKey(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

	cases := []struct {
		name, key, header string
		want              int
	}{
		{"no key configured keeps the API closed", "", "anything", http.StatusServiceUnavailable},
		{"missing header", "s3cret-key", "", http.StatusUnauthorized},
		{"wrong key", "s3cret-key", "s3cret-kez", http.StatusUnauthorized},
		{"right key", "s3cret-key", "s3cret-key", http.StatusNoContent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/platform/countries", nil)
			if c.header != "" {
				req.Header.Set(PlatformKeyHeader, c.header)
			}
			rec := httptest.NewRecorder()
			RequirePlatformKey(c.key)(ok).ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d", rec.Code, c.want)
			}
		})
	}
}
