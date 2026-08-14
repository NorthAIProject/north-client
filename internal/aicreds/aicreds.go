// Package aicreds stores the AI provider key a user brought themselves, and
// turns it into a client the coach tries before North's own providers.
//
// Separate from internal/connections, which issues the tokens outside agents
// present to North. The two are opposite directions and share no type: one is
// a credential North mints and verifies, the other is a credential North holds
// on somebody's behalf and must never show again.
package aicreds

import (
	"time"

	"github.com/google/uuid"
)

// Credential is a stored key as the settings page sees it.
//
// No field can hold the key. Same discipline as connections.Connection and
// strava.Status: a projection that cannot carry a secret cannot leak one, and
// that is a property of the type rather than of whoever writes the template.
type Credential struct {
	UserID   uuid.UUID
	Provider string

	// KeyHint is the last few characters of the key, enough to recognise which
	// one is stored and useless to anybody else.
	KeyHint string

	// Model is empty when the user accepted the provider's default.
	Model string

	// BaseURL is the user's Hermes gateway, empty for catalogue providers.
	BaseURL string

	// LastError is why the most recent attempt failed, if it did. Written so
	// the page can say the key was rejected rather than letting somebody
	// discover it as a coach that quietly got worse.
	LastError   string
	LastErrorAt *time.Time

	UpdatedAt time.Time
}

// Failing reports whether the last attempt to use this key was refused.
func (c Credential) Failing() bool { return c.LastError != "" }

// Input is a settings-page submission.
type Input struct {
	Provider string

	// APIKey empty on a model-only save means "keep the stored key". The field
	// renders empty on every load, so blank is the normal state of the form
	// and cannot be read as "clear it" — removing a key is its own action.
	APIKey string

	Model string

	// BaseURL is required when the provider is Hermes.
	BaseURL string
}
