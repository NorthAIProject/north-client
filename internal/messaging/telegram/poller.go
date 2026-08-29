package telegram

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// pollTimeoutSeconds is how long Telegram holds a getUpdates call open waiting
// for something to happen. Long polling: one request per half minute of
// silence rather than a request per second of it.
const pollTimeoutSeconds = 30

// pollErrorBackoff is the pause after a failed poll, so a Telegram outage or a
// bad token produces a slow trickle of log lines rather than a flood.
const pollErrorBackoff = 5 * time.Second

// Poller reads updates by asking for them, instead of being told.
//
// This is what makes the feature developable. A webhook needs a public HTTPS
// URL, which localhost does not have, so without this every local change would
// need a tunnel. Polling needs nothing but an outbound connection.
//
// It is not merely a development convenience — it is a legitimate production
// mode for a single instance — but it does not scale past one: two pollers on
// one bot each receive half the updates.
type Poller struct {
	bridge bridge
	client *Client
	log    *slog.Logger
}

type PollerConfig struct {
	Messages Handler
	Client   *Client
	Log      *slog.Logger
}

// NewPoller builds the poller, or returns nil when there is nothing to poll
// with — the caller's signal not to start it.
func NewPoller(cfg PollerConfig) *Poller {
	if cfg.Messages == nil || cfg.Client == nil {
		return nil
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Poller{
		bridge: bridge{messages: cfg.Messages, client: cfg.Client, log: log},
		client: cfg.Client,
		log:    log,
	}
}

// Run polls until the context is cancelled.
//
// The offset is held in memory rather than persisted, and that is deliberate:
// Telegram already forgets an update once the next offset acknowledges it, and
// the few that a restart replays are caught by the last_update_id watermark in
// messaging_links. Two mechanisms for one guarantee would be one more place
// for them to disagree.
func (p *Poller) Run(ctx context.Context) error {
	p.log.Info("telegram poller started")

	var (
		offset int64
		// Latched so the conflict is reported once rather than on every poll.
		// Reset by any different failure, so a later recurrence is heard again.
		conflictReported bool
	)
	for {
		if ctx.Err() != nil {
			p.log.Info("telegram poller stopped")
			return nil
		}

		updates, err := p.client.getUpdates(ctx, offset, pollTimeoutSeconds)
		if err != nil {
			if ctx.Err() != nil {
				p.log.Info("telegram poller stopped")
				return nil
			}

			// A registered webhook is a configuration mistake, not an outage:
			// Telegram will refuse every poll for as long as it is set. Said
			// once at error level with the fix in it, because the alternative
			// is a warning every five seconds forever that reads like a network
			// problem and never names the cause.
			if errors.Is(err, ErrWebhookActive) {
				if !conflictReported {
					conflictReported = true
					p.log.Error("telegram polling is blocked by a registered webhook",
						"error", err,
						"fix", "delete the webhook (deleteWebhook) to poll, or run in webhook mode instead",
					)
				}
			} else {
				conflictReported = false
				p.log.Warn("telegram poll failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(pollErrorBackoff):
			}
			continue
		}

		for _, u := range updates {
			// Advanced before the update is answered, not after: acknowledging
			// is what stops Telegram resending it, and an update that fails to
			// produce a reply must not be retried forever.
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}

			p.bridge.dispatch(ctx, u)
		}
	}
}
