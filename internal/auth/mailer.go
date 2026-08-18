package auth

import (
	"context"
	"fmt"
	"log/slog"
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

// passwordResetEmail builds the body of a forget-password message.
func passwordResetEmail(displayName, resetURL string) Message {
	greeting := "Hi"
	if displayName != "" {
		greeting = "Hi " + displayName
	}
	return Message{
		Subject: "Reset your DuxAI password",
		Body: fmt.Sprintf(`%s,

Someone asked to reset the password on your DuxAI account.

Open this link to choose a new password (it expires in one hour):

%s

If you did not ask for this, you can ignore this email. Your password will stay the same.

— DuxAI
`, greeting, resetURL),
	}
}
