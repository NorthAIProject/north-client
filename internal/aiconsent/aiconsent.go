// Package aiconsent records whether a person allows their data to go to
// third-party AI providers.
//
// App Review guidelines 5.1.1(i) and 5.1.2(i) require the app to say what is
// sent and to whom, and to ask first. The answer lives here; internal/ai refuses
// a call for anyone who has not said yes, so the promise holds for background
// work as well as for the screens that show the question.
package aiconsent

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/ai"
	aiconsentdb "github.com/NorthAIProject/north-client/internal/aiconsent/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Version is the wording people answer. Bump it when the data sent changes in a
// way that needs asking again.
const Version = 1

// The answers. Unset is never stored: it is the absence of a row.
const (
	StatusUnset    = "unset"
	StatusGranted  = "granted"
	StatusDeclined = "declined"
)

// Consent is one person's answer.
type Consent struct {
	Status    string
	Version   int
	DecidedAt *time.Time
}

// Service reads and records answers. It satisfies ai.ConsentChecker.
type Service struct {
	q *aiconsentdb.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{q: aiconsentdb.New(pool)}
}

// Get returns the answer, or StatusUnset for someone never asked.
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (Consent, error) {
	row, err := s.q.GetAIConsent(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Consent{Status: StatusUnset}, nil
	}
	if err != nil {
		return Consent{}, apperr.Wrap(err, "read ai consent")
	}
	return project(row), nil
}

// Set records a decision, replacing any earlier one.
func (s *Service) Set(ctx context.Context, userID uuid.UUID, status string) (Consent, error) {
	if status != StatusGranted && status != StatusDeclined {
		return Consent{}, apperr.Wrap(apperr.ErrValidation, "decision must be granted or declined")
	}
	row, err := s.q.SetAIConsent(ctx, aiconsentdb.SetAIConsentParams{
		UserID:  userID,
		Status:  status,
		Version: Version,
	})
	if err != nil {
		return Consent{}, apperr.Wrap(err, "save ai consent")
	}
	return project(row), nil
}

// Checker is what the registry gates on: this service when consent is required,
// or nil, which leaves every call allowed, while AI_CONSENT_REQUIRED is off.
func Checker(pool *pgxpool.Pool, required bool) ai.ConsentChecker {
	if !required {
		return nil
	}
	return NewService(pool)
}

// AllowsAI reports whether this person said yes.
func (s *Service) AllowsAI(ctx context.Context, userID uuid.UUID) (bool, error) {
	c, err := s.Get(ctx, userID)
	if err != nil {
		return false, err
	}
	return c.Status == StatusGranted, nil
}

func project(row aiconsentdb.AiConsent) Consent {
	at := row.DecidedAt
	return Consent{Status: row.Status, Version: int(row.Version), DecidedAt: &at}
}

// providerLabels are the companies people know, by registry name. The test
// model is left out: it runs in this process and sends nothing anywhere.
var providerLabels = map[string]string{
	"anthropic":  "Anthropic",
	"gemini":     "Google (Gemini)",
	"hermes":     "Hermes",
	"nvidia":     "NVIDIA",
	"openrouter": "OpenRouter",
}

// Recipients names the companies a consenting person's data can reach, from the
// providers this deployment actually has. Derived rather than written down so
// the screen cannot fall behind a configuration change.
func Recipients(providerNames []string) []string {
	out := make([]string, 0, len(providerNames))
	for _, name := range providerNames {
		if name == "fake" {
			continue
		}
		label, known := providerLabels[name]
		if !known {
			label = name
		}
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}
