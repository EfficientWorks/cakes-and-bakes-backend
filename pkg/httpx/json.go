package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/obs"
)

// Envelope is the response shape used by every endpoint:
//
//	{"success": true,  "statusCode": 200, "data": {...}, "meta": {...}}
//	{"success": false, "statusCode": 401, "error": {"code","message","field"}}
//
// It is exported so the generated OpenAPI spec can describe real response
// bodies (httpx.Envelope{data=...}) rather than a hand-copied duplicate
// that drifts from what the server actually writes.
type Envelope struct {
	Success bool `json:"success" example:"true"`
	// StatusCode repeats the HTTP status in the body. Redundant over HTTP,
	// and deliberately so: clients that reach the API through something which
	// does not hand them the status line — a wrapper that returns only the
	// parsed body, a log of a response, a mobile SDK — could otherwise only
	// tell 401 from 409 by string-matching the error code.
	//
	// Never set by hand. writeEnvelope stamps it from the status actually
	// being written, so a body that disagrees with its own status line is not
	// something a caller can cause.
	StatusCode int         `json:"statusCode" example:"200"`
	Data       interface{} `json:"data,omitempty"`
	Meta       interface{} `json:"meta,omitempty"`
	Stats      interface{} `json:"stats,omitempty"`
	Error      *ErrorBody  `json:"error,omitempty"`
}

// Meta carries pagination for list responses.
type Meta struct {
	Page       int `json:"page" example:"1"`
	PerPage    int `json:"per_page" example:"50"`
	Total      int `json:"total" example:"234"`
	TotalPages int `json:"total_pages" example:"5"`
}

// ErrorBody is the error half of the Envelope. Clients switch on Code (see
// apperr codes); Message is human-facing copy and may be reworded.
type ErrorBody struct {
	Code    string              `json:"code" example:"SLOT_CAPACITY_EXCEEDED"`
	Message string              `json:"message" example:"No remaining capacity for the requested slot"`
	Field   string              `json:"field,omitempty"`
	Fields  []apperr.FieldError `json:"fields,omitempty"`
	// Meta carries data the client needs to act on the error rather than just
	// show it — the 3-D Secure client secret, for one.
	Meta      map[string]any `json:"meta,omitempty"`
	RequestID string         `json:"requestId,omitempty"`
}

// WriteJSON writes an arbitrary payload with a status code, no envelope wrapping.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// writeEnvelope is the one place an Envelope reaches the wire.
//
// It stamps StatusCode from the status being written rather than trusting the
// caller, so the number in the body and the number on the status line cannot
// drift apart — including for any envelope added later, which is the point of
// funnelling all four helpers through here.
func writeEnvelope(w http.ResponseWriter, status int, env Envelope) {
	env.StatusCode = status
	WriteJSON(w, status, env)
}

// OK writes a 200 success envelope.
func OK(w http.ResponseWriter, data interface{}) {
	writeEnvelope(w, http.StatusOK, Envelope{Success: true, Data: data})
}

// OKWithMeta writes a 200 success envelope including pagination/meta info.
func OKWithMeta(w http.ResponseWriter, data, meta interface{}) {
	writeEnvelope(w, http.StatusOK, Envelope{Success: true, Data: data, Meta: meta})
}

// OKWithMeta writes a 200 success envelope including pagination/meta stats info.
func OKWithMetaAndStats(w http.ResponseWriter, data, meta interface{}, stats interface{}) {
	writeEnvelope(w, http.StatusOK, Envelope{Success: true, Data: data, Meta: meta, Stats: stats})
}

// Created writes a 201 success envelope.
func Created(w http.ResponseWriter, data interface{}) {
	writeEnvelope(w, http.StatusCreated, Envelope{Success: true, Data: data})
}

// NoContent writes a 204 with an empty body.
//
// No envelope, and so no statusCode: 204 means "no body", and a client that
// reads one anyway is reading something no proxy is obliged to forward. A
// caller that needs a body in the response wants 200, not this.
func NoContent(w http.ResponseWriter) {
	WriteJSON(w, http.StatusNoContent, nil)
}

// WriteError maps an error to the correct HTTP status and the standard
// {"success":false,"error":{...}} envelope. If err is not an *apperr.Error
// it becomes a 500 INTERNAL_ERROR and the real cause is never leaked.
// StatusClientClosedRequest is the de-facto code (nginx's 499) for a request
// the caller abandoned before the server answered.
const StatusClientClosedRequest = 499

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	/*
	 * The caller hung up.
	 *
	 * A browser that navigates away, or a storefront that re-requests slots as
	 * the customer changes a picker, cancels the request; every query after
	 * that fails with "context canceled". That is not a server fault, and
	 * reporting it as a 500 buried the real failures in the error log. Only
	 * the request's own cancellation counts — a timeout or cancellation from
	 * anywhere else is still an error.
	 */
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		slog.Info("request cancelled by client",
			"path", r.URL.Path,
			"request_id", obs.RequestIDFromContext(r.Context()),
		)
		w.WriteHeader(StatusClientClosedRequest)
		return
	}

	ae, ok := apperr.As(err)
	if !ok {
		ae = apperr.Internal(err)
	}

	env := Envelope{
		Success: false,
		Error: &ErrorBody{
			Code:      ae.Code,
			Message:   ae.Message,
			Fields:    ae.Fields,
			Meta:      ae.Meta,
			RequestID: obs.RequestIDFromContext(r.Context()),
		},
	}
	if len(ae.Fields) == 1 {
		env.Error.Field = ae.Fields[0].Field
	}

	status := ae.Category.HTTPStatus()

	// Log the underlying cause for anything that is our problem or an
	// upstream's.
	//
	// The client is deliberately told only `ae.Message` — "Payment could not
	// be completed", "Could not send verification email" — and the real reason
	// lives on the wrapped cause, which nothing read. So a declined card, an
	// expired API key and a provider outage were indistinguishable from both
	// sides: the customer saw one sentence and the log had nothing at all.
	//
	// Client errors (400/404/409) are not logged here: they are the normal
	// result of a bad request and would drown the log without adding anything.
	if status >= 500 {
		slog.Error("request failed",
			"code", ae.Code,
			"status", status,
			"path", r.URL.Path,
			"cause", ae.Error(),
			"request_id", obs.RequestIDFromContext(r.Context()),
		)
	}

	writeEnvelope(w, status, env)
}

// DecodeJSON decodes the request body into v, rejecting unknown fields.
func DecodeJSON(r *http.Request, v interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return apperr.Validation("Request body is not valid JSON: " + err.Error())
	}
	return nil
}
