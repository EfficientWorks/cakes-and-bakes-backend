package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bakehouse/pkg/apperr"
)

// statusCode in the body must always be the status on the wire.
//
// The whole value of the field is that a client can trust it without seeing
// the status line. A body claiming 200 under a 409 would be worse than not
// having the field at all, so this pins every helper that writes an envelope
// rather than trusting each to be written correctly.
func TestStatusCodeMatchesTheWire(t *testing.T) {
	cases := []struct {
		name  string
		write func(w http.ResponseWriter, r *http.Request)
		want  int
	}{
		{"OK", func(w http.ResponseWriter, _ *http.Request) { OK(w, map[string]string{"a": "b"}) }, 200},
		{"OKWithMeta", func(w http.ResponseWriter, _ *http.Request) { OKWithMeta(w, nil, Meta{Page: 1}) }, 200},
		{"Created", func(w http.ResponseWriter, _ *http.Request) { Created(w, nil) }, 201},

		{"validation", errWriter(apperr.Validation("no")), 400},
		{"authentication", errWriter(apperr.Unauthenticated("no")), 401},
		{"authorization", errWriter(apperr.PermissionDenied("no")), 403},
		{"not found", errWriter(apperr.NotFound("no")), 404},
		{"method not allowed", errWriter(apperr.New("METHOD_NOT_ALLOWED", apperr.CategoryMethodNotAllowed, "no")), 405},
		{"conflict", errWriter(apperr.Conflict("no")), 409},
		{"business rule", errWriter(apperr.BusinessRule("X", "no")), 422},
		{"rate limited", errWriter(apperr.RateLimited("no")), 429},
		{"internal", errWriter(apperr.Internal(nil)), 500},

		// Not an *apperr.Error at all — the path that becomes a 500.
		{"bare error", errWriter(errString("boom")), 500},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.write(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

			if rec.Code != tc.want {
				t.Fatalf("HTTP status = %d, want %d", rec.Code, tc.want)
			}
			var env struct {
				StatusCode int `json:"statusCode"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
			}
			if env.StatusCode != rec.Code {
				t.Errorf("body statusCode = %d, wire status = %d — they must agree",
					env.StatusCode, rec.Code)
			}
		})
	}
}

// An unrouted path and a wrong verb are answered without any handler running,
// so they are the two responses most likely to escape the envelope.
func TestUnroutedResponsesAreStillEnveloped(t *testing.T) {
	r := NewRouter(nil, nil)
	r.Get("/thing", func(w http.ResponseWriter, _ *http.Request) { OK(w, nil) })

	for _, tc := range []struct {
		name, method, path string
		want               int
	}{
		{"no such path", http.MethodGet, "/nope", 404},
		{"wrong verb", http.MethodPost, "/thing", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			if rec.Code != tc.want {
				t.Fatalf("HTTP status = %d, want %d", rec.Code, tc.want)
			}
			var env struct {
				Success    bool `json:"success"`
				StatusCode int  `json:"statusCode"`
				Error      *struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
			}
			if env.Success {
				t.Error("success = true on an error response")
			}
			if env.StatusCode != tc.want {
				t.Errorf("body statusCode = %d, want %d", env.StatusCode, tc.want)
			}
			if env.Error == nil || env.Error.Code == "" {
				t.Error("no error code")
			}
		})
	}
}

// 204 carries no body at all, so there is nothing to read a statusCode from.
func TestNoContentStaysEmpty(t *testing.T) {
	rec := httptest.NewRecorder()
	NoContent(rec)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 wrote a body: %q", rec.Body.String())
	}
}

func errWriter(err error) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) { WriteError(w, r, err) }
}

type errString string

func (e errString) Error() string { return string(e) }
