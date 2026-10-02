package phone

import (
	"context"
	"errors"
)

// Verifier texts a one-time code to a number and checks what comes back.
// TwilioVerifier is the production implementation; DevVerifier stands in for
// it in local development.
//
// Numbers are E.164 and never logged by an implementation: an error names
// what went wrong, not whose number it was.
type Verifier interface {
	// Start texts a fresh code to number.
	Start(ctx context.Context, number string) error
	// Check reports whether code is the one most recently texted to number.
	Check(ctx context.Context, number, code string) (bool, error)
}

// ErrUndeliverable is a number the provider will not text: malformed in a way
// Normalize cannot see, a landline, or a blocked prefix.
var ErrUndeliverable = errors.New("number cannot receive a code")

// ErrTooManyAttempts is the provider's own limit on sends or checks.
var ErrTooManyAttempts = errors.New("too many verification attempts")

// ErrExpired is a check against a code that is no longer waiting: it timed
// out, was already used, or its attempts ran out.
var ErrExpired = errors.New("verification expired")

// DevCode is the only code DevVerifier accepts.
const DevCode = "000000"

// DevVerifier sends nothing and accepts DevCode for any number. It exists so
// the whole journey can be walked locally without an SMS account, the way
// auth.LogMailer stands in for SMTP. Never wired in production.
type DevVerifier struct{}

func (DevVerifier) Start(context.Context, string) error { return nil }

func (DevVerifier) Check(_ context.Context, _ string, code string) (bool, error) {
	return code == DevCode, nil
}
