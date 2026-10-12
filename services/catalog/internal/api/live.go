package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/realtime"
)

/*
 * Live catalogue updates.
 *
 * Every successful write to the master catalogue tells the admin console and
 * the website that something changed, so an open product list or product page
 * refreshes without a reload. One middleware on the write routes rather than a
 * call in every service method: a new write route cannot forget to announce
 * itself, and a refused write (4xx/5xx) announces nothing.
 *
 * The event says what kind of thing changed and which one; screens re-read.
 */

// liveWrites publishes eventType after a successful non-GET request. idParam
// names the route parameter holding the changed record's id ("" for none).
func liveWrites(eventType, idParam string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			if rec.status >= http.StatusMultipleChoices {
				return
			}
			// Read after routing: the sub-router has filled the params by now.
			id := ""
			if idParam != "" {
				id = chi.URLParam(r, idParam)
			}
			action := "updated"
			switch {
			case r.Method == http.MethodDelete:
				action = "deleted"
			case r.Method == http.MethodPost && id == "":
				action = "created"
			}
			data := map[string]any{"action": action}
			if id != "" {
				data["id"] = id
			}
			if b := chi.URLParam(r, "branchId"); b != "" {
				data["branchId"] = b
			}
			realtime.Default.Publish(realtime.Event{Type: eventType, Data: data})
		})
	}
}

// statusRecorder remembers the status a handler wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status, s.wrote = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wrote = true
	return s.ResponseWriter.Write(b)
}
