package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stub stands in for Cloudflare so the request and response handling can be
// pinned without an account. Everything Cloudflare-shaped in here comes from
// their documented v4 envelope.
func stub(t *testing.T, handler http.HandlerFunc) *CloudflareImages {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	store := NewCloudflareImages("acct-123", "secret-token", "hash-abc")
	store.apiBase = server.URL
	return store
}

func TestCreateUploadTicket(t *testing.T) {
	var gotPath, gotAuth, gotBody string

	store := stub(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		buf := make([]byte, 256)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"errors": [],
			"result": {"id": "img-42", "uploadURL": "https://upload.imagedelivery.net/one-time/abc"}
		}`))
	})

	ticket, err := store.CreateUploadTicket(context.Background())
	if err != nil {
		t.Fatalf("CreateUploadTicket: %v", err)
	}

	if want := "/accounts/acct-123/images/v2/direct_upload"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	// The API token authorises the ticket request and must never be part of
	// what goes back to the browser.
	if want := "Bearer secret-token"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if !strings.Contains(gotBody, "requireSignedURLs=false") {
		t.Errorf("body = %q, want it to disable signed URLs", gotBody)
	}

	if ticket.ID != "img-42" {
		t.Errorf("ID = %q, want img-42", ticket.ID)
	}
	if ticket.UploadURL != "https://upload.imagedelivery.net/one-time/abc" {
		t.Errorf("UploadURL = %q", ticket.UploadURL)
	}
	if want := "https://imagedelivery.net/hash-abc/img-42/public"; ticket.DeliveryURL != want {
		t.Errorf("DeliveryURL = %q, want %q", ticket.DeliveryURL, want)
	}
	if strings.Contains(ticket.UploadURL+ticket.DeliveryURL, "secret-token") {
		t.Error("the API token leaked into a value meant for the browser")
	}
}

// Cloudflare answers 200 with success:false, so the status code alone cannot
// be trusted to mean the ticket was issued.
func TestCreateUploadTicketSurfacesCloudflareErrors(t *testing.T) {
	store := stub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": false, "errors": [{"code": 10000, "message": "Authentication error"}], "result": null}`))
	})

	_, err := store.CreateUploadTicket(context.Background())
	if err == nil {
		t.Fatal("expected an error for success:false")
	}
	if !strings.Contains(err.Error(), "Authentication error") {
		t.Errorf("error = %v, want Cloudflare's own message", err)
	}
}

// Deleting something already gone satisfies the caller's intent.
func TestDeleteTreatsMissingImageAsDone(t *testing.T) {
	store := stub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if err := store.Delete(context.Background(), "img-gone"); err != nil {
		t.Errorf("Delete on a missing image = %v, want nil", err)
	}
}

func TestDeleteReportsRealFailures(t *testing.T) {
	store := stub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1,"message":"boom"}]}`))
	})
	if err := store.Delete(context.Background(), "img-1"); err == nil {
		t.Error("Delete swallowed a 500")
	}
}

func TestDeleteEscapesTheImageID(t *testing.T) {
	var gotPath string
	store := stub(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
	})
	_ = store.Delete(context.Background(), "img/../../evil")
	if strings.Contains(gotPath, "/../") {
		t.Errorf("path = %q, want the id escaped rather than traversing", gotPath)
	}
}
