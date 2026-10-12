package realtime

import (
	"fmt"
	"net/http"
	"time"
)

// Stream writes events to a Server-Sent Events response until the client goes
// away or the channel closes.
//
// Each event is sent as a named SSE event (event: <type>) carrying the event
// JSON, so a browser EventSource can listen per type. A "ready" event opens
// the stream and a comment ping every 25 s keeps proxies from closing it.
func Stream(w http.ResponseWriter, r *http.Request, events <-chan Event, readyJSON string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	if readyJSON == "" {
		readyJSON = "{}"
	}
	fmt.Fprintf(w, "retry: 3000\nevent: ready\ndata: %s\n\n", readyJSON)
	flusher.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case e, open := <-events:
			if !open {
				return
			}
			fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", e.ID, e.Type, e.JSON())
			flusher.Flush()
		}
	}
}
