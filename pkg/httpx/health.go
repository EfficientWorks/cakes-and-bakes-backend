package httpx

import "net/http"

// Health is a liveness/readiness endpoint mounted at GET /health.
func Health(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
