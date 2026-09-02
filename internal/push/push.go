// Package push delivers nudges to browsers over Web Push.
//
// The nudge engine (internal/nudges) decides what North says and when. This
// package is only the last mile: it keeps the subscriptions browsers hand
// over, encrypts one payload per subscription, and reports back whether the
// push service accepted it. Nothing here decides whether a nudge is warranted,
// and nothing here knows what a nudge is beyond a title, a body, and a link.
//
// It exists to answer one product question before anybody builds a native
// app: does a note on the lock screen bring a person back? The only thing a
// native app would add over the installed PWA is exactly this channel, so
// this is the cheapest way to find out whether that channel matters.
package push

import (
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Limits on what a browser may register. A push endpoint is a URL the push
// service issued; a key is a base64url string of fixed size. Anything far
// beyond these is not a subscription.
const (
	maxEndpointLength = 2048
	maxKeyLength      = 512
	maxUserAgent      = 512

	// maxBodyRunes keeps the encrypted payload under the 4 KiB record the
	// push services accept. Nudge bodies are a sentence; this is a guard.
	maxBodyRunes = 500
)

// Subscription is one browser that agreed to receive nudges.
type Subscription struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Endpoint  string
	P256dh    string
	Auth      string
	UserAgent string

	CreatedAt time.Time
	// LastUsedAt is the last send the push service accepted.
	LastUsedAt *time.Time
	// FailedAt is the last send refused for a reason other than the
	// subscription being gone. Gone subscriptions are deleted, not marked.
	FailedAt *time.Time
}

// Input is what PushSubscription.toJSON() produces in the browser, plus the
// User-Agent the request arrived with so a settings page can name the device.
type Input struct {
	Endpoint  string
	P256dh    string
	Auth      string
	UserAgent string
}

// validate refuses anything that is not a push subscription. The push service
// is the only party that issues endpoints, and it issues https URLs.
func (in Input) validate() error {
	var errs apperr.FieldErrors

	endpoint := strings.TrimSpace(in.Endpoint)
	switch u, err := url.Parse(endpoint); {
	case endpoint == "":
		errs = errs.Add("endpoint", "An endpoint is required.")
	case len(endpoint) > maxEndpointLength:
		errs = errs.Add("endpoint", "That endpoint is too long.")
	case err != nil || u.Scheme != "https" || u.Host == "":
		errs = errs.Add("endpoint", "The endpoint must be an https URL.")
	}

	if k := strings.TrimSpace(in.P256dh); k == "" || len(k) > maxKeyLength {
		errs = errs.Add("p256dh", "The browser's public key is required.")
	}
	if k := strings.TrimSpace(in.Auth); k == "" || len(k) > maxKeyLength {
		errs = errs.Add("auth", "The browser's auth secret is required.")
	}

	return errs.OrNil()
}

// message is the JSON the service worker reads out of a push event.
type message struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Href  string `json:"href,omitempty"`
}

// clip bounds a body so the encrypted record stays within what push services
// accept. Cutting on a rune boundary rather than a byte keeps the last word
// readable.
func clip(s string, runes int) string {
	if utf8.RuneCountInString(s) <= runes {
		return s
	}
	r := []rune(s)
	return string(r[:runes-1]) + "…"
}
