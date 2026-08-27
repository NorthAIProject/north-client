package auth

import (
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// smtpTimeout bounds the whole conversation — dial, handshake, auth, send.
//
// Transactional mail is sent on a request path: somebody is watching a form
// spinner while this runs. A provider having a slow morning must not become a
// page that never answers.
const smtpTimeout = 20 * time.Second

// SMTPMailer delivers over SMTP.
//
// Written against net/smtp rather than a provider SDK because SMTP is the one
// interface every transactional provider offers — Resend, SES, Postmark,
// Mailgun, Fastmail all speak it — so choosing or changing provider is a change
// to configuration rather than to code. It also keeps a dependency out of
// go.mod for something the standard library already does.
type SMTPMailer struct {
	// Host and Port are the submission endpoint. 587 is the usual submission
	// port and upgrades with STARTTLS; 465 is implicit TLS and is dialled
	// encrypted from the first byte. Both are handled.
	Host string
	Port int

	Username string
	Password string

	// From is the envelope sender and the From header. Providers reject mail
	// from a domain they have not been configured to send for, so this is not
	// free-form — it has to match whatever was verified with them.
	From string

	// FromName is the display name. Optional.
	FromName string
}

// Send implements Mailer.
func (m SMTPMailer) Send(ctx context.Context, msg Message) error {
	if m.Host == "" || m.From == "" {
		return fmt.Errorf("smtp: not configured")
	}

	// Header injection guard.
	//
	// The recipient reaches here from a form. Registration validates the
	// address, but a mailer that trusts its caller is one refactor away from
	// being wrong: a newline in an address or subject would let whoever typed
	// it append headers of their own — a second Bcc, a replaced body — to mail
	// that this domain has authorised and signed.
	if err := rejectHeaderInjection(msg); err != nil {
		return err
	}

	addr := net.JoinHostPort(m.Host, fmt.Sprint(m.Port))

	// The deadline is derived from ctx when it has one, so a caller that has
	// already spent most of its budget does not get a fresh twenty seconds.
	deadline := time.Now().Add(smtpTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp: dial %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	if deadlineErr := conn.SetDeadline(deadline); deadlineErr != nil {
		return fmt.Errorf("smtp: set deadline: %w", deadlineErr)
	}

	// Port 465 is TLS from the first byte, with no plaintext greeting to
	// upgrade from. Everything else starts in the clear and is upgraded below.
	if m.Port == 465 {
		conn = tls.Client(conn, &tls.Config{ServerName: m.Host, MinVersion: tls.VersionTLS12})
	}

	client, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		return fmt.Errorf("smtp: greet %s: %w", m.Host, err)
	}
	defer func() { _ = client.Close() }()

	if m.Port != 465 {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			// Refused rather than sent in the clear. The alternative is a
			// password reset link crossing the internet as plaintext, which is
			// the same failure this mailer exists to fix.
			return fmt.Errorf("smtp: %s does not offer STARTTLS; refusing to send credentials or reset links unencrypted", m.Host)
		}
		if tlsErr := client.StartTLS(&tls.Config{ServerName: m.Host, MinVersion: tls.VersionTLS12}); tlsErr != nil {
			return fmt.Errorf("smtp: starttls: %w", tlsErr)
		}
	}

	if m.Username != "" {
		// PlainAuth refuses to hand credentials to a server it is not talking
		// to over TLS, which is why this comes after the upgrade above.
		if authErr := client.Auth(smtp.PlainAuth("", m.Username, m.Password, m.Host)); authErr != nil {
			return fmt.Errorf("smtp: authenticate as %s: %w", m.Username, authErr)
		}
	}

	if mailErr := client.Mail(m.From); mailErr != nil {
		return fmt.Errorf("smtp: sender rejected: %w", mailErr)
	}
	if rcptErr := client.Rcpt(msg.To); rcptErr != nil {
		return fmt.Errorf("smtp: recipient rejected: %w", rcptErr)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp: data: %w", err)
	}
	if _, err := w.Write([]byte(m.compose(msg))); err != nil {
		return fmt.Errorf("smtp: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: complete: %w", err)
	}

	return client.Quit()
}

// compose renders the message as RFC 5322.
//
// Line endings are CRLF throughout, including inside the body: a bare LF is
// what makes some servers treat the rest of the message as a continuation and
// silently mangle it.
func (m SMTPMailer) compose(msg Message) string {
	from := m.From
	if m.FromName != "" {
		// Q-encoded so a display name with an accent in it does not arrive as
		// mojibake, and cannot smuggle raw bytes into the header.
		from = fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", m.FromName), m.From)
	}

	headers := []string{
		"From: " + from,
		"To: " + msg.To,
		"Subject: " + mime.QEncoding.Encode("utf-8", msg.Subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		// Tells well-behaved autoresponders not to reply to this, which keeps
		// an out-of-office loop from bouncing off the sending address.
		"Auto-Submitted: auto-generated",
	}

	body := strings.ReplaceAll(msg.Body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", "\r\n")

	return strings.Join(headers, "\r\n") + "\r\n\r\n" + body
}

// rejectHeaderInjection refuses a message whose fields could break out of their
// header. See the call site for why this does not trust the caller.
func rejectHeaderInjection(msg Message) error {
	if strings.ContainsAny(msg.To, "\r\n") {
		return fmt.Errorf("smtp: recipient contains a line break")
	}
	if strings.ContainsAny(msg.Subject, "\r\n") {
		return fmt.Errorf("smtp: subject contains a line break")
	}
	if _, err := mail.ParseAddress(msg.To); err != nil {
		return fmt.Errorf("smtp: recipient is not a valid address: %w", err)
	}
	return nil
}
