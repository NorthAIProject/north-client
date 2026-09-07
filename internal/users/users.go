// Package users owns the account record: who someone is, how they want to be
// coached, and what timezone their day runs in.
//
// Credentials and sessions live in internal/auth. This package deliberately
// knows nothing about passwords beyond storing an already-hashed value, so that
// the hashing policy has exactly one home.
package users

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	usersdb "github.com/NorthAIProject/north-client/internal/users/db"
)

// Tier decides which chain of AI providers serves a user. It is deliberately
// not a billing state: what North needs to know at coaching time is which
// backends to spend on, and whatever eventually manages subscriptions can set
// this without the coach learning anything about payments.
type Tier string

const (
	TierFree Tier = "free"
	TierPro  Tier = "pro"

	// TierDefault matches the column default in the migration.
	TierDefault = TierFree
)

// Tiers is every tier an account may hold, free first.
var Tiers = []Tier{TierFree, TierPro}

// Label is the human name for the tier.
func (t Tier) Label() string {
	if t == TierPro {
		return "Pro"
	}
	return "Free"
}

// Valid reports whether the tier is one this build knows.
//
// Checked in Go as well as by the column's CHECK constraint, so a bad value is
// a clear error from the service rather than a constraint violation surfacing
// from three layers down.
func (t Tier) Valid() bool {
	return slices.Contains(Tiers, t)
}

// Tone is the voice the coach speaks in. A closed set, unlike CoachingStyle,
// so the prompt builder always has something to render and a new account has a
// voice before anyone opens settings.
type Tone string

const (
	ToneDirect     Tone = "direct"
	ToneWarm       Tone = "warm"
	ToneAnalytical Tone = "analytical"
	ToneToughLove  Tone = "tough_love"

	// ToneDefault matches the column default in the migration.
	ToneDefault = ToneDirect
)

// Tones is every tone a user may choose, in the order settings shows them.
var Tones = []Tone{ToneDirect, ToneWarm, ToneAnalytical, ToneToughLove}

// Label is the human name for the tone, for a form or a summary row.
func (t Tone) Label() string {
	switch t {
	case ToneWarm:
		return "Warm"
	case ToneAnalytical:
		return "Analytical"
	case ToneToughLove:
		return "Tough love"
	default:
		return "Direct"
	}
}

// Valid reports whether the tone is one this build knows.
func (t Tone) Valid() bool {
	return slices.Contains(Tones, t)
}

// Key names the tone in the message catalogue.
//
// A method rather than an i18n call, because internal/shared/i18n imports this
// package for users.Locale and cannot be imported back. The web layer, which
// imports both, does the lookup.
func (t Tone) Key() string {
	if !t.Valid() {
		return "tone." + string(ToneDefault)
	}
	return "tone." + string(t)
}

// Locale is the language Khepri speaks to a user in.
//
// A closed set, like Tone: the prompt builder and every template need a value
// they can render, and an unrecognised tag has to resolve to something rather
// than render nothing.
//
// Portuguese is carried as two locales rather than one. pt-PT and pt-BR diverge
// most in exactly the vocabulary this product uses all day — a Brazilian
// reading European gym copy notices in the first sentence — so folding them
// into "pt" would make the feature worse for whichever half lost the coin toss.
type Locale string

const (
	LocaleEN   Locale = "en"
	LocalePTPT Locale = "pt-PT"
	LocalePTBR Locale = "pt-BR"
	LocaleES   Locale = "es"

	// LocaleDefault matches the column default in the migration.
	LocaleDefault = LocaleEN
)

// Locales is every language a user may choose, in the order settings shows them.
var Locales = []Locale{LocaleEN, LocalePTPT, LocalePTBR, LocaleES}

// Label is the language's own name for itself, which is the only name a person
// looking for it will recognise. Someone who has landed in the wrong language
// cannot read "Portuguese (Brazil)" to escape it.
func (l Locale) Label() string {
	switch l {
	case LocalePTPT:
		return "Português (Portugal)"
	case LocalePTBR:
		return "Português (Brasil)"
	case LocaleES:
		return "Español"
	default:
		return "English"
	}
}

// Language is the name of the language for a prompt, in English, so the model
// is told plainly which variety to answer in.
func (l Locale) Language() string {
	switch l {
	case LocalePTPT:
		return "European Portuguese (pt-PT)"
	case LocalePTBR:
		return "Brazilian Portuguese (pt-BR)"
	case LocaleES:
		return "Spanish (es)"
	default:
		return "English (en)"
	}
}

// Valid reports whether the locale is one this build knows.
func (l Locale) Valid() bool {
	return slices.Contains(Locales, l)
}

// ResolveLocale maps anything stored or submitted onto a locale this build
// serves, falling back to English.
//
// Case and separator are both normalised: browsers and hand-written requests
// send "pt-br", "pt_BR" and "PT-BR" for the same thing, and a person should not
// lose their language to a hyphen.
func ResolveLocale(s string) Locale {
	normalised := strings.ReplaceAll(strings.TrimSpace(s), "_", "-")
	for _, l := range Locales {
		if strings.EqualFold(normalised, string(l)) {
			return l
		}
	}
	// A bare "pt" is ambiguous by design, but refusing to answer it would be
	// worse than picking: European Portuguese is the older tag's usual meaning.
	if strings.EqualFold(normalised, "pt") {
		return LocalePTPT
	}
	return LocaleDefault
}

// User is the domain view of an account. It excludes the password hash: nothing
// outside internal/auth has any business reading it, and leaving it off the
// type means it cannot be leaked into a template or a log line by accident.
type User struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	Timezone    string

	// Locale is the language every surface renders in and the coach answers in.
	Locale Locale

	// CoachingStyle is the user's own description of how they want to be
	// coached. Empty until they set one.
	CoachingStyle string

	// CoachingTone is the voice the coach uses. CoachingStyle refines it;
	// this decides it.
	CoachingTone Tone

	Tier Tier

	// OnboardedAt is set when the user completes or skips first-run onboarding.
	// Nil means the web app should show the onboarding flow once.
	OnboardedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// NeedsOnboarding reports whether the web app should gate this account behind
// the first-run questionnaire.
func (u User) NeedsOnboarding() bool {
	return u.OnboardedAt == nil
}

// Location resolves the user's timezone, falling back to UTC. Scheduling a
// briefing must never fail because a stored timezone name became invalid.
func (u User) Location() *time.Location {
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// FirstName is the informal address the coach uses.
func (u User) FirstName() string {
	name := strings.TrimSpace(u.DisplayName)
	if name == "" {
		return "there"
	}
	if first, _, found := strings.Cut(name, " "); found {
		return first
	}
	return name
}

func fromDB(row usersdb.User) User {
	u := User{
		ID:          row.ID,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Timezone:    row.Timezone,
		// Resolved rather than cast: a tag this build no longer serves must
		// still render as something, and English is the safe read.
		Locale:       ResolveLocale(row.Locale),
		CoachingTone: Tone(row.CoachingTone),
		Tier:         Tier(row.Tier),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
	if row.CoachingStyle != nil {
		u.CoachingStyle = *row.CoachingStyle
	}
	u.OnboardedAt = row.OnboardedAt
	return u
}
