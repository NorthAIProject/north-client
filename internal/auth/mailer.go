package auth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// Mailer delivers transactional account email. Auth depends on the interface
// so production can plug in SMTP/SES without the service knowing how.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// Message is a plain-text transactional email.
type Message struct {
	To      string
	Subject string
	Body    string
}

// LogMailer writes messages to the structured logger. Used in development and
// tests when no real SMTP is configured — the reset link is visible in logs.
type LogMailer struct {
	Log *slog.Logger
}

// Send implements Mailer.
func (m LogMailer) Send(ctx context.Context, msg Message) error {
	log := m.Log
	if log == nil {
		log = slog.Default()
	}
	log.InfoContext(ctx, "transactional email",
		slog.String("to", msg.To),
		slog.String("subject", msg.Subject),
		slog.String("body", msg.Body),
	)
	return nil
}

// CaptureMailer records messages for tests.
type CaptureMailer struct {
	Messages []Message
}

// Send implements Mailer.
func (m *CaptureMailer) Send(_ context.Context, msg Message) error {
	m.Messages = append(m.Messages, msg)
	return nil
}

// passwordResetEmail builds the body of a forget-password message in the
// recipient's language. tag is the account's locale, not the request's: the
// email is read later, in an inbox, by the person who owns the account.
func passwordResetEmail(tag, displayName, resetURL string) Message {
	greeting := i18n.Translate(tag, "auth.email.hi")
	if displayName != "" {
		greeting = fmt.Sprintf(i18n.Translate(tag, "auth.email.hi.name"), displayName)
	}
	return Message{
		Subject: i18n.Translate(tag, "auth.email.subject"),
		Body:    fmt.Sprintf(i18n.Translate(tag, "auth.email.body"), greeting, resetURL),
	}
}
