package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

// SendGridEmailer sends transactional mail through SendGrid's v3 API.
//
// The HTTP API rather than SendGrid's SMTP relay, for two reasons. Outbound
// port 587 is blocked or throttled on a good many hosts, which turns a mail
// problem into a mysterious timeout; and SMTP's failure modes arrive as a
// three-digit code where the API returns a JSON body naming the field it
// objected to — the difference between "550 unauthenticated" and "the from
// address is not a verified sender".
//
// Written against net/http rather than SendGrid's SDK, which is how this
// codebase treats every other provider: one file, no dependency, and the
// wire format visible at the call site.
type SendGridEmailer struct {
	apiKey    string
	fromEmail string
	fromName  string
	client    *http.Client
	// apiBase is injectable so the tests can point it at a stub rather than at
	// SendGrid.
	apiBase string
}

// ErrSendGridNotConfigured reports a missing API key or sender.
var ErrSendGridNotConfigured = errors.New("email: SendGrid is not configured")

// NewSendGridEmailer builds the sender.
//
// `from` accepts either a bare address or the RFC 5322 "Name <address>" form,
// because that is how a sender is usually written down and pasted into an
// environment variable. The address itself must be a verified sender on the
// SendGrid account or every send is refused with a 403.
func NewSendGridEmailer(apiKey, from string) (*SendGridEmailer, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("%w: no API key", ErrSendGridNotConfigured)
	}
	name, address, err := parseFrom(from)
	if err != nil {
		return nil, err
	}
	return &SendGridEmailer{
		apiKey:    strings.TrimSpace(apiKey),
		fromEmail: address,
		fromName:  name,
		apiBase:   "https://api.sendgrid.com",
		// A generous but finite timeout: a hung mail call must not hold a
		// customer's registration request open indefinitely.
		client: &http.Client{Timeout: 20 * time.Second},
	}, nil
}

// parseFrom splits "Name <address>" into its parts, and accepts a bare address.
func parseFrom(from string) (name, address string, err error) {
	from = strings.TrimSpace(from)
	if from == "" {
		return "", "", fmt.Errorf("%w: no from address", ErrSendGridNotConfigured)
	}
	if parsed, perr := mail.ParseAddress(from); perr == nil {
		return parsed.Name, parsed.Address, nil
	}
	if !strings.Contains(from, "@") {
		return "", "", fmt.Errorf("%w: %q is not an email address", ErrSendGridNotConfigured, from)
	}
	return "", from, nil
}

// FromAddress reports the sender every message will be sent as, so the caller
// can log it at boot — a wrong sender is the most common reason SendGrid
// refuses a whole account's mail, and it is worth being visible.
func (s *SendGridEmailer) FromAddress() string {
	if s.fromName != "" {
		return s.fromName + " <" + s.fromEmail + ">"
	}
	return s.fromEmail
}

// sendGridPayload is the subset of the v3 /mail/send body this needs.
type sendGridPayload struct {
	Personalizations []sendGridPersonalization `json:"personalizations"`
	From             sendGridAddress           `json:"from"`
	Subject          string                    `json:"subject"`
	Content          []sendGridContent         `json:"content"`
}

type sendGridPersonalization struct {
	To []sendGridAddress `json:"to"`
}

type sendGridAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type sendGridContent struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Send implements Emailer.
//
// The template alias is rendered locally — the same HTML the SMTP sender
// produces — rather than handed to SendGrid as a dynamic template, so the
// message a customer receives does not depend on a template existing in
// somebody's SendGrid dashboard.
func (s *SendGridEmailer) Send(ctx context.Context, e Email) error {
	if e.To == "" {
		return errors.New("email: no recipient")
	}

	brand := BrandFor(ctx, e)
	subject := e.Subject
	var htmlBody, textBody string

	if e.Template != "" {
		rendered, err := RenderBranded(brand, e.Template, e.Data)
		if err != nil {
			return fmt.Errorf("sendgrid: render template %q: %w", e.Template, err)
		}
		if subject == "" {
			subject = rendered.Subject
		}
		htmlBody = rendered.HTMLBody
		textBody = rendered.TextBody
	}

	// SendGrid rejects a message with no content at all, and orders the
	// content array plain-text first by specification.
	content := make([]sendGridContent, 0, 2)
	if textBody != "" {
		content = append(content, sendGridContent{Type: "text/plain", Value: textBody})
	}
	if htmlBody != "" {
		content = append(content, sendGridContent{Type: "text/html", Value: htmlBody})
	}
	if len(content) == 0 {
		content = append(content, sendGridContent{Type: "text/plain", Value: subject})
	}
	if subject == "" {
		subject = brand.Name
	}

	body, err := json.Marshal(sendGridPayload{
		Personalizations: []sendGridPersonalization{{To: []sendGridAddress{{Email: e.To}}}},
		From:             sendGridAddress{Email: s.fromEmail, Name: s.fromName},
		Subject:          subject,
		Content:          content,
	})
	if err != nil {
		return fmt.Errorf("sendgrid: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiBase+"/v3/mail/send", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sendgrid: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sendgrid: send to %s: %w", e.To, err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}

	// SendGrid explains itself in the body — "The from address does not match
	// a verified Sender Identity" is the usual one, and it is worth carrying
	// through rather than reporting a bare status code.
	detail, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	return fmt.Errorf("sendgrid: send to %s failed: %s: %s",
		e.To, res.Status, strings.TrimSpace(string(detail)))
}
