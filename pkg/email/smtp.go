package email

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
)

// SMTPEmailer sends mail through a standard SMTP server (e.g. Gmail).
// It renders built-in HTML/text templates for all known template aliases
// and sends them as multipart/alternative messages.
type SMTPEmailer struct {
	host      string
	port      string
	username  string
	password  string
	fromEmail string
}

// NewSMTPEmailer creates a new SMTPEmailer.
//
//	host     — SMTP server hostname, e.g. "smtp.gmail.com"
//	port     — SMTP server port as string, e.g. "587"
//	username — SMTP login username (usually the sender address)
//	password — SMTP login password or app password
//	fromEmail — the From: address used in every outgoing message
func NewSMTPEmailer(host, port, username, password, fromEmail string) *SMTPEmailer {
	return &SMTPEmailer{
		host:      host,
		port:      port,
		username:  username,
		password:  password,
		fromEmail: fromEmail,
	}
}

// Send implements email.Emailer. If the Email carries a Template alias, the
// built-in renderer is used to produce subject/HTML/text. Otherwise the
// Email's Subject field is used with an empty body.
func (s *SMTPEmailer) Send(ctx context.Context, e Email) error {
	subject := e.Subject
	htmlBody := ""
	textBody := ""

	if e.Template != "" {
		rendered, err := RenderBranded(BrandFor(ctx, e), e.Template, e.Data)
		if err != nil {
			return fmt.Errorf("smtp emailer: render template %q: %w", e.Template, err)
		}
		if subject == "" {
			subject = rendered.Subject
		}
		htmlBody = rendered.HTMLBody
		textBody = rendered.TextBody
	}

	return s.sendMail(e.To, subject, htmlBody, textBody)
}

// sendMail composes and delivers a multipart/alternative message.
func (s *SMTPEmailer) sendMail(to, subject, htmlBody, textBody string) error {
	auth := smtp.PlainAuth("", s.username, s.password, s.host)
	addr := s.host + ":" + s.port

	msg := s.buildMessage(to, subject, htmlBody, textBody)

	if err := smtp.SendMail(addr, auth, s.fromEmail, []string{to}, []byte(msg)); err != nil {
		return fmt.Errorf("smtp send to %s: %w", to, err)
	}
	return nil
}

// buildMessage constructs a minimal MIME email. When both HTML and plain-text
// bodies are provided it uses multipart/alternative so mail clients can pick
// the best representation.
func (s *SMTPEmailer) buildMessage(to, subject, htmlBody, textBody string) string {
	boundary := "==BakehouseMIMEBoundary=="

	var sb strings.Builder

	// Headers
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString(fmt.Sprintf("From: %s\r\n", s.fromEmail))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", to))
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))

	if htmlBody != "" {
		sb.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
		sb.WriteString("\r\n")

		// Plain-text part
		sb.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		sb.WriteString("\r\n")
		sb.WriteString(textBody)
		sb.WriteString("\r\n")

		// HTML part (preferred by clients — must come last in multipart/alternative)
		sb.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		sb.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		sb.WriteString("\r\n")
		sb.WriteString(htmlBody)
		sb.WriteString("\r\n")

		sb.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else {
		// Plain-text only
		sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		sb.WriteString("\r\n")
		sb.WriteString(textBody)
	}

	return sb.String()
}
