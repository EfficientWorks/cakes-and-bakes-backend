package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"bakehouse/pkg/apperr"
)

// A request the caller abandoned is not a server failure.
func TestWriteErrorClientCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest(http.MethodGet, "/v1/storefront/slots", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	WriteError(w, r, apperr.Internal(context.Canceled))
	if w.Code != StatusClientClosedRequest {
		t.Fatalf("status = %d, want %d", w.Code, StatusClientClosedRequest)
	}
}

// A cancellation that is not the caller's own is still a 500.
func TestWriteErrorOtherCancellationIs500(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/storefront/slots", nil)
	w := httptest.NewRecorder()
	WriteError(w, r, apperr.Internal(errors.Join(errors.New("upstream"), context.Canceled)))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}
