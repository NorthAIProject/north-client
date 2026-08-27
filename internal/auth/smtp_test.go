package auth_test

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/auth"
)

// A recipient that could break out of its header is refused before anything is
// dialled.
//
// The address reaches the mailer from a form. Registration validates it, but a
// mailer that trusts its caller is one refactor away from being wrong — and the
// thing being sent is an account recovery link from a domain this deployment
// has authorised and signed, which is exactly what an injected Bcc would want.
func TestAnInjectedHeaderIsRefused(t *testing.T) {
	t.Parallel()

	// Pointed at a live listener on purpose. With an unresolvable host the dial
	// fails first and every case below "passes" without the guard ever running
	// — which is exactly what happened when this test was first written.
	addr := plainSMTPServer(t)
	host, port := splitHostPort(t, addr)
	m := auth.SMTPMailer{Host: host, Port: port, From: "no-reply@khepri.test"}

	for _, tc := range []struct {
		name string
		msg  auth.Message
	}{
		{"newline in recipient", auth.Message{
			To:      "victim@example.test\r\nBcc: attacker@evil.test",
			Subject: "Reset your password",
		}},
		{"bare linefeed in recipient", auth.Message{
			To:      "victim@example.test\nBcc: attacker@evil.test",
			Subject: "Reset your password",
		}},
		{"newline in subject", auth.Message{
			To:      "victim@example.test",
			Subject: "Reset\r\nBcc: attacker@evil.test",
		}},
		{"not an address at all", auth.Message{
			To:      "not-an-address",
			Subject: "Reset your password",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := m.Send(context.Background(), tc.msg)
			if err == nil {
				t.Fatal("the message was accepted; a header could be injected into signed mail")
			}
			// The error has to come from the guard, not from the connection.
			// Anything reaching the wire has already been composed.
			if !strings.Contains(err.Error(), "line break") &&
				!strings.Contains(err.Error(), "not a valid address") {
				t.Errorf("error = %q, want the injection guard to have rejected it", err)
			}
		})
	}
}

// An unconfigured mailer says so rather than dialling an empty address.
func TestAnUnconfiguredMailerRefuses(t *testing.T) {
	t.Parallel()

	var m auth.SMTPMailer
	if err := m.Send(context.Background(), auth.Message{To: "someone@example.test"}); err == nil {
		t.Error("an unconfigured mailer reported success")
	}
}

// A server that will not upgrade to TLS is refused, not fallen back to.
//
// The alternative is a password reset link crossing the internet in plaintext,
// which is the same failure the whole mailer exists to fix — so a downgrade has
// to be an error rather than a warning.
func TestSendRefusesAServerThatWillNotUpgradeToTLS(t *testing.T) {
	t.Parallel()

	addr := plainSMTPServer(t)
	host, port := splitHostPort(t, addr)

	m := auth.SMTPMailer{
		Host: host,
		Port: port,
		From: "no-reply@khepri.test",
	}

	err := m.Send(context.Background(), auth.Message{
		To:      "someone@example.test",
		Subject: "Reset your password",
		Body:    "https://khepri.test/reset-password?token=secret",
	})
	if err == nil {
		t.Fatal("a reset link was sent to a server offering no encryption")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("error = %q, want it to name STARTTLS so the cause is obvious", err)
	}
}

// A cancelled context stops the send rather than running to the full timeout.
func TestSendHonoursACancelledContext(t *testing.T) {
	t.Parallel()

	addr := plainSMTPServer(t)
	host, port := splitHostPort(t, addr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := auth.SMTPMailer{Host: host, Port: port, From: "no-reply@khepri.test"}
	if err := m.Send(ctx, auth.Message{To: "someone@example.test"}); err == nil {
		t.Error("the send proceeded on a cancelled context")
	}
}

// plainSMTPServer speaks just enough SMTP to be greeted and to answer EHLO
// without advertising STARTTLS. It never upgrades, which is the case under
// test.
func plainSMTPServer(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

				w := bufio.NewWriter(conn)
				r := bufio.NewReader(conn)

				_, _ = w.WriteString("220 plain.test ESMTP\r\n")
				_ = w.Flush()

				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						// Deliberately no STARTTLS in the extension list.
						_, _ = w.WriteString("250-plain.test\r\n250 SIZE 10240000\r\n")
					case strings.HasPrefix(line, "HELO"):
						_, _ = w.WriteString("250 plain.test\r\n")
					case strings.HasPrefix(line, "QUIT"):
						_, _ = w.WriteString("221 Bye\r\n")
						_ = w.Flush()
						return
					default:
						_, _ = w.WriteString("250 OK\r\n")
					}
					_ = w.Flush()
				}
			}()
		}
	}()

	return ln.Addr().String()
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	return host, port
}
