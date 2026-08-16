package telegram

import (
	"crypto/subtle"
	"io"
	"log/slog"
	"net/http"
)

// secretHeader is the header Telegram echoes back the secret_token given to
// setWebhook. It is the only thing distinguishing a real delivery from anyone
// who guesses the URL, so the endpoint is worthless without it.
const secretHeader = "X-Telegram-Bot-Api-Secret-Token" //nolint:gosec // header name, not a credential

// maxUpdateBytes bounds one update. Telegram's are small; this is well above
// the largest real one and far below anything worth buffering from a stranger.
const maxUpdateBytes = 1 << 20

// Webhook serves Telegram's POSTs.
//
// Mounted outside the session and CSRF middleware, for the reasons /mcp and
// /ingest/health are: the caller is not a browser, holds no cookie, and
// authenticates with a shared secret instead.
type Webhook struct {
	bridge bridge
	secret string
	log    *slog.Logger
}

type WebhookConfig struct {
	Messages Handler
	Client   *Client

	// Secret is the value given to Telegram's setWebhook. Required: an empty
	// secret would make the endpoint an open door, so NewWebhook refuses one.
	Secret string

	Log *slog.Logger
}

// NewWebhook builds the handler. Returns nil when no secret is configured,
// which is the caller's signal not to mount the route at all.
func NewWebhook(cfg WebhookConfig) *Webhook {
	if cfg.Secret == "" || cfg.Messages == nil || cfg.Client == nil {
		return nil
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Webhook{
		bridge: bridge{messages: cfg.Messages, client: cfg.Client, log: log},
		secret: cfg.Secret,
		log:    log,
	}
}

func (h *Webhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Constant time, because a byte-at-a-time comparison against a secret is
	// measurable over enough requests and this endpoint is public.
	presented := r.Header.Get(secretHeader)
	if subtle.ConstantTimeCompare([]byte(presented), []byte(h.secret)) != 1 {
		h.log.Warn("telegram webhook rejected an unauthenticated delivery")
		// No body: anything descriptive here tells a prober how close they are.
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxUpdateBytes))
	if err != nil {
		http.Error(w, "could not read update", http.StatusBadRequest)
		return
	}

	// Acknowledged before it is answered. Telegram retries anything that is not
	// a prompt 200 and a coach turn takes minutes, so holding the request open
	// would guarantee both a timeout and a redelivery. The watermark in
	// messaging_links is what makes that redelivery harmless if it happens
	// anyway.
	h.bridge.dispatchRaw(r.Context(), raw)
	w.WriteHeader(http.StatusOK)
}
