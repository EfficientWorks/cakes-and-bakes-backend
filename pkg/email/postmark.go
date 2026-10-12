package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const postmarkEndpoint = "https://api.postmarkapp.com/email/withTemplate"

// PostmarkEmailer sends mail through Postmark's template API.
type PostmarkEmailer struct {
	serverToken string
	fromEmail   string
	httpClient  *http.Client
}

func NewPostmarkEmailer(serverToken, fromEmail string) *PostmarkEmailer {
	return &PostmarkEmailer{
		serverToken: serverToken,
		fromEmail:   fromEmail,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

type postmarkRequest struct {
	From          string                 `json:"From"`
	To            string                 `json:"To"`
	TemplateAlias string                 `json:"TemplateAlias"`
	TemplateModel map[string]interface{} `json:"TemplateModel"`
	MessageStream string                 `json:"MessageStream"`
}

func (p *PostmarkEmailer) Send(ctx context.Context, email Email) error {
	body, err := json.Marshal(postmarkRequest{
		From:          p.fromEmail,
		To:            email.To,
		TemplateAlias: email.Template,
		TemplateModel: email.Data,
		MessageStream: "outbound",
	})
	if err != nil {
		return fmt.Errorf("marshal postmark request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, postmarkEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build postmark request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Postmark-Server-Token", p.serverToken)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("postmark request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		// Postmark puts the actual reason in the body, and the status code
		// alone is close to useless — a pending-approval account rejecting
		// every recipient outside its own domain and a bad template alias
		// both surface as the same code. Reading the body is the difference
		// between a diagnosable log line and "postmark responded 422".
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("postmark responded %d: %s", resp.StatusCode, bytes.TrimSpace(detail))
	}
	return nil
}

// LoggingEmailer logs the email instead of sending it — used in local dev
// when no Postmark server token is configured, so every flow (OTP, receipts)
// stays testable via Postman without a real mail provider.
type LoggingEmailer struct {
	log *slog.Logger
}

func NewLoggingEmailer(log *slog.Logger) *LoggingEmailer {
	return &LoggingEmailer{log: log}
}

func (l *LoggingEmailer) Send(ctx context.Context, email Email) error {
	l.log.Info("email (dev, not sent)",
		"to", email.To,
		"template", email.Template,
		"data", email.Data,
	)
	return nil
}
