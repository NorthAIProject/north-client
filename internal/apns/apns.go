// Package apns delivers nudges to the iOS app through Apple's push service.
//
// It is the native twin of internal/push, which reaches browsers. The nudge
// engine decides what North says and when; this package keeps the device
// tokens the app hands over and sends one notification per token. Nothing here
// decides whether a nudge is warranted.
package apns

import (
	"encoding/hex"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// The two hosts Apple runs. A token is only good on the one that issued it.
const (
	EnvironmentProduction = "production"
	EnvironmentSandbox    = "sandbox"
)

// Device is one app install that agreed to receive nudges.
type Device struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Token       string
	Topic       string
	Environment string

	CreatedAt  time.Time
	LastUsedAt *time.Time
	FailedAt   *time.Time
}

// Input is what the app sends after iOS hands it a device token.
type Input struct {
	// Token is the device token as lowercase hex.
	Token string
	// Topic is the app's bundle identifier.
	Topic string
	// Environment is production for TestFlight and App Store builds, sandbox
	// for a build run from Xcode.
	Environment string
}

// normalized trims the input and lowercases the token, so the same install
// never stores two rows that differ only in case.
func (in Input) normalized() Input {
	return Input{
		Token:       strings.ToLower(strings.TrimSpace(in.Token)),
		Topic:       strings.TrimSpace(in.Topic),
		Environment: strings.TrimSpace(in.Environment),
	}
}

// validate refuses anything that is not a device token for one of our apps.
// The topic allow-list matters: the key signs for every app on the team, so
// without it a caller could make this server push under another app's name.
func (in Input) validate(topics []string) error {
	var errs apperr.FieldErrors
	if b, err := hex.DecodeString(in.Token); err != nil || len(b) != 32 {
		errs = errs.Add("token", "The device token must be 64 hexadecimal characters.")
	}
	if !slices.Contains(topics, in.Topic) {
		errs = errs.Add("topic", "That app is not one this server sends to.")
	}
	if in.Environment != EnvironmentProduction && in.Environment != EnvironmentSandbox {
		errs = errs.Add("environment", "The environment must be production or sandbox.")
	}
	return errs.OrNil()
}

// Notification is what one send carries. Href is the web path the nudge
// leads to; the app maps it onto a screen the way the bell does.
type Notification struct {
	Title string
	Body  string
	Href  string
}
