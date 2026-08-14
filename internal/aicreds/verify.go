package aicreds

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai/providers"
)

// verifyTimeout bounds the check. Somebody is watching a form submit, so this
// is short: a provider that cannot answer in ten seconds is not a reason to
// refuse to store a key that may be perfectly good.
const verifyTimeout = 10 * time.Second

// ErrKeyRejected means the provider itself said no. Distinct from every other
// failure, because it is the only one the user can act on.
var ErrKeyRejected = errors.New("the provider rejected this key")

// KeyVerifier checks a credential against its provider before it is stored.
//
// An interface so the check can be stubbed: the alternative is tests that make
// real calls to five vendors, which would be slow, flaky, and would need real
// keys to mean anything.
type KeyVerifier interface {
	// Verify returns ErrKeyRejected when the provider refuses the credential,
	// nil when it accepts it, and any other error when the question could not
	// be asked. That third case must not block a save — a provider having a
	// bad minute is not evidence about the key.
	Verify(ctx context.Context, entry providers.BYOProvider, key string) error
}

// NewHTTPVerifier is the real check: a cheap authenticated GET against the
// provider's own API, using whichever path the catalogue says answers 401 to a
// bad key.
//
// Which path that is had to be probed per provider rather than assumed — see
// providers.BYOProvider.VerifyPath. Two of the five have one.
//
// Exported so the behaviour can be tested against a stand-in provider without
// a shim, and so a caller with its own transport can supply one.
func NewHTTPVerifier(client *http.Client) KeyVerifier {
	if client == nil {
		client = &http.Client{Timeout: verifyTimeout}
	}
	return httpVerifier{client: client}
}

type httpVerifier struct {
	client *http.Client
}

func (v httpVerifier) Verify(ctx context.Context, entry providers.BYOProvider, key string) error {
	if entry.VerifyPath == "" || entry.BaseURL == "" {
		// Nothing to ask. Storing unverified is the honest outcome; the coach's
		// own fallback and last_error still cover a bad key.
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.BaseURL+entry.VerifyPath, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)

	res, err := v.client.Do(req)
	if err != nil {
		// Network trouble is not the key's fault.
		return err
	}
	defer func() { _ = res.Body.Close() }()

	switch {
	case res.StatusCode == http.StatusUnauthorized, res.StatusCode == http.StatusForbidden:
		return ErrKeyRejected
	case res.StatusCode >= 500:
		// The provider is unwell. Not evidence either way.
		return errors.New("provider could not answer")
	default:
		// Anything else — including a 404 from a backend that routes /models
		// differently — is treated as acceptance rather than rejection. The
		// check exists to catch a mistyped key, and refusing on an unexpected
		// status would block a working one.
		//
		// The body is deliberately not read: it can echo the credential back,
		// and nothing here needs it.
		return nil
	}
}
