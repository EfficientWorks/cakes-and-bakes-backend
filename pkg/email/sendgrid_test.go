package email

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseFromAcceptsBothWrittenForms(t *testing.T) {
	cases := []struct{ in, wantName, wantAddress string }{
		{"orders@bakedbouquet.com", "", "orders@bakedbouquet.com"},
		{"Baked Bouquet <orders@bakedbouquet.com>", "Baked Bouquet", "orders@bakedbouquet.com"},
		{"  spaced@example.com  ", "", "spaced@example.com"},
	}
	for _, c := range cases {
		name, address, err := parseFrom(c.in)
		if err != nil {
			t.Errorf("parseFrom(%q): %v", c.in, err)
			continue
		}
		if name != c.wantName || address != c.wantAddress {
			t.Errorf("parseFrom(%q) = (%q, %q), want (%q, %q)", c.in, name, address, c.wantName, c.wantAddress)
		}
	}
}

// A missing key or sender must be refused at construction, not discovered on
// the first customer registration.
func TestSendGridRefusesIncompleteConfiguration(t *testing.T) {
	if _, err := NewSendGridEmailer("", "a@b.com"); err == nil {
		t.Error("an empty API key was accepted")
	}
	if _, err := NewSendGridEmailer("SG.key", ""); err == nil {
		t.Error("an empty from address was accepted")
	}
	if _, err := NewSendGridEmailer("SG.key", "not-an-address"); err == nil {
		t.Error("a from value with no @ was accepted")
	}
}

func stubSendGrid(t *testing.T, status int, body string) (*SendGridEmailer, *sendGridPayload, *string) {
	t.Helper()
	var got sendGridPayload
	var auth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	sender, err := NewSendGridEmailer("SG.test-key", "Baked Bouquet <orders@bakedbouquet.com>")
	if err != nil {
		t.Fatalf("NewSendGridEmailer: %v", err)
	}
	sender.apiBase = server.URL
	return sender, &got, &auth
}

func TestSendGridSendsTheRenderedTemplate(t *testing.T) {
	sender, got, auth := stubSendGrid(t, http.StatusAccepted, "")

	err := sender.Send(context.Background(), Email{
		To:       "jenifer.nobel@gmail.com",
		Template: TemplateAccountVerifyOTP,
		Data:     map[string]interface{}{"otp": "418902", "expiresInMinutes": 10},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if *auth != "Bearer SG.test-key" {
		t.Errorf("Authorization = %q", *auth)
	}
	if len(got.Personalizations) != 1 || got.Personalizations[0].To[0].Email != "jenifer.nobel@gmail.com" {
		t.Errorf("recipient not set: %+v", got.Personalizations)
	}
	if got.From.Email != "orders@bakedbouquet.com" || got.From.Name != "Baked Bouquet" {
		t.Errorf("from = %+v", got.From)
	}
	// The OTP has to reach the customer, so it must be in the body — this is
	// the assertion that would have caught mail that "sent" but said nothing.
	var sawCode bool
	for _, c := range got.Content {
		if strings.Contains(c.Value, "418902") {
			sawCode = true
		}
	}
	if !sawCode {
		t.Error("the one-time code is not in the message body")
	}
	if !strings.Contains(got.Subject, "418902") && got.Subject == "" {
		t.Error("no subject was set")
	}
}

// Both parts, plain text first, so a client that cannot render HTML still
// shows the code.
func TestSendGridSendsTextAndHTML(t *testing.T) {
	sender, got, _ := stubSendGrid(t, http.StatusAccepted, "")

	if err := sender.Send(context.Background(), Email{
		To: "a@b.com", Template: TemplatePasswordResetOTP,
		Data: map[string]interface{}{"otp": "123456"},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(got.Content) != 2 {
		t.Fatalf("got %d content parts, want 2", len(got.Content))
	}
	if got.Content[0].Type != "text/plain" {
		t.Errorf("first part is %q, want text/plain first", got.Content[0].Type)
	}
	if got.Content[1].Type != "text/html" {
		t.Errorf("second part is %q, want text/html", got.Content[1].Type)
	}
}

// A refusal must name the reason. "403" alone sends someone reading logs for
// an hour; "does not match a verified Sender Identity" is a five-minute fix.
func TestSendGridSurfacesTheProvidersReason(t *testing.T) {
	sender, _, _ := stubSendGrid(t, http.StatusForbidden,
		`{"errors":[{"message":"The from address does not match a verified Sender Identity."}]}`)

	err := sender.Send(context.Background(), Email{
		To: "a@b.com", Template: TemplateAccountVerifyOTP,
		Data: map[string]interface{}{"otp": "111111"},
	})
	if err == nil {
		t.Fatal("a 403 was reported as success")
	}
	if !strings.Contains(err.Error(), "verified Sender Identity") {
		t.Errorf("the provider's explanation was dropped: %v", err)
	}
}

func TestSendGridRefusesAMessageWithNoRecipient(t *testing.T) {
	sender, _, _ := stubSendGrid(t, http.StatusAccepted, "")
	if err := sender.Send(context.Background(), Email{Template: TemplateAccountVerifyOTP}); err == nil {
		t.Error("a message with no recipient was accepted")
	}
}
