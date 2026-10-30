// Package settings composes users.Service, preferences.Service,
// meals.DietPreferenceService, and connections.Service into the account
// settings pages. It owns no repository of its own — every field on these
// pages already has a home in one of those services.
package settings

import (
	"time"

	"github.com/NorthAIProject/north-client/internal/account"
	"github.com/NorthAIProject/north-client/internal/aicreds"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/connections"
	"github.com/NorthAIProject/north-client/internal/integrations"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/preferences"
	"github.com/NorthAIProject/north-client/internal/push"
	"github.com/NorthAIProject/north-client/internal/toolaudit"
	"github.com/NorthAIProject/north-client/internal/users"
)

// probeBudget bounds the background check of a newly saved provider. Longer
// than the probe's own timeout so a slow gateway is given its full chance.
const probeBudget = 2 * time.Minute

type Handler struct {
	users         *users.Service
	preferences   *preferences.Service
	notifications *notifications.Service
	diets         *meals.DietPreferenceService
	connections   *connections.Service
	aicreds       *aicreds.Service
	audit         *toolaudit.Service

	// messaging issues and revokes platform link codes. Nil is not a valid
	// state — the service is built whether or not a bot token exists, because
	// this page is where a link begins.
	messaging *messaging.Service

	// telegram is configuration, not a service: the page needs to know whether
	// a bot exists and what it is called, and neither is a decision.
	telegram config.TelegramConfig

	// integrations is North's outbound MCP connections. Nil leaves the calendar
	// card off, which is what a process with no encryption key gets.
	integrations *integrations.Service

	// account and mw are here rather than in a handler of their own because
	// deleting is refused by re-rendering this page with the reason on it, and
	// that render already lives here. A separate package would have to export
	// the whole of it to say one thing back.
	account *account.Service
	mw      *auth.Middleware

	// push is Web Push. Nil, or a service without keys, leaves the row off
	// the notifications card.
	push *push.Service
}

// WithIntegrations wires the calendar connection card.
//
// A setter rather than a tenth positional argument to NewHandler, which already
// takes nine.
func (h *Handler) WithIntegrations(svc *integrations.Service) *Handler {
	h.integrations = svc
	return h
}

// WithPush wires the "nudges on this device" row.
func (h *Handler) WithPush(svc *push.Service) *Handler {
	h.push = svc
	return h
}

func NewHandler(
	u *users.Service,
	p *preferences.Service,
	n *notifications.Service,
	d *meals.DietPreferenceService,
	c *connections.Service,
	a *aicreds.Service,
	audit *toolaudit.Service,
	m *messaging.Service,
	telegram config.TelegramConfig,
	acct *account.Service,
	mw *auth.Middleware,
) *Handler {
	return &Handler{
		users: u, preferences: p, notifications: n, diets: d, connections: c, aicreds: a,
		audit: audit, messaging: m, telegram: telegram, account: acct, mw: mw,
	}
}
