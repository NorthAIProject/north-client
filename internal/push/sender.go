package push

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/NorthAIProject/north-client/internal/config"
)

// Sender delivers one encrypted payload to one browser and reports the push
// service's answer. An interface so the service can be tested against a fake
// that returns 201, 410 and 500 without a push service on the other end.
type Sender interface {
	// Send returns the HTTP status the push service answered with. A non-nil
	// error means no answer at all: a network failure, or a payload the
	// library refused to encrypt.
	Send(ctx context.Context, sub Subscription, payload []byte) (status int, err error)
}

// VAPID is the key pair that signs every push request, so the push service can
// tell one application server from another. Generated once per deployment
// with `main vapid-keygen` and never rotated casually: a new public key
// invalidates every existing subscription.
type VAPID struct {
	PublicKey  string
	PrivateKey string

	// Subject is who to contact about this sender, as a mailto: or https URL.
	// Push services require it in the signed token.
	Subject string
}

// Enabled reports whether a key pair is configured.
func (v VAPID) Enabled() bool {
	return v.PublicKey != "" && v.PrivateKey != ""
}

// VAPIDFrom lifts the configured key pair into the shape the sender wants.
func VAPIDFrom(cfg config.PushConfig) VAPID {
	return VAPID{
		PublicKey:  cfg.VAPIDPublicKey,
		PrivateKey: cfg.VAPIDPrivateKey,
		Subject:    cfg.Subject,
	}
}

// GenerateKeys makes a fresh VAPID pair, public first.
func GenerateKeys() (publicKey, privateKey string, err error) {
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", fmt.Errorf("generate vapid keys: %w", err)
	}
	return public, private, nil
}

// Deliveries are fire-and-forget from the person's point of view, so a slow
// push service must not hold a worker sweep for long.
const sendTimeout = 10 * time.Second

// A nudge that arrives an hour late is still a nudge; one that arrives
// tomorrow is noise. The push service holds the message this long for a
// device that is offline, then drops it.
const ttlSeconds = 3600

type webpushSender struct {
	vapid  VAPID
	client *http.Client
}

// NewSender builds the real sender, or nil when no key pair is configured.
// A nil Sender is how Service knows push is off.
func NewSender(v VAPID) Sender {
	if !v.Enabled() {
		return nil
	}
	return &webpushSender{
		vapid:  v,
		client: &http.Client{Timeout: sendTimeout},
	}
}

func (s *webpushSender) Send(ctx context.Context, sub Subscription, payload []byte) (int, error) {
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			Auth:   sub.Auth,
			P256dh: sub.P256dh,
		},
	}, &webpush.Options{
		HTTPClient:      s.client,
		Subscriber:      s.vapid.Subject,
		VAPIDPublicKey:  s.vapid.PublicKey,
		VAPIDPrivateKey: s.vapid.PrivateKey,
		TTL:             ttlSeconds,
		Urgency:         webpush.UrgencyNormal,
	})
	if err != nil {
		return 0, fmt.Errorf("send web push: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// The body is a short error description at most. Drained so the
	// connection can be reused; never logged, because it echoes the endpoint.
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}
