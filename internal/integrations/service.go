package integrations

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Calendar is the part of an adapter the service uses. An interface so a test
// can drive Connect and Collect without an MCP server.
type Calendar interface {
	Upcoming(ctx context.Context, endpoint, token string, now time.Time) ([]string, error)
}

type Service struct {
	repo     *Repository
	calendar Calendar
	now      func() time.Time
}

func NewService(repo *Repository, calendar Calendar) *Service {
	return &Service{repo: repo, calendar: calendar, now: time.Now}
}

// WithClock fixes now, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Status is what the settings page shows. Never carries the token.
func (s *Service) Status(ctx context.Context, userID uuid.UUID) (Connection, bool, error) {
	conn, err := s.repo.Get(ctx, userID, ProviderCalendar)
	switch {
	case err == nil:
		return conn, true, nil
	case apperr.Is(err, apperr.ErrNotFound):
		return Connection{}, false, nil
	default:
		return Connection{}, false, err
	}
}

// Connect stores a calendar server and immediately proves it works.
//
// Verifying here rather than trusting the input is the difference between
// "connected" meaning something and meaning "a URL was typed": somebody who
// pastes the wrong endpoint finds out now, not a week later when the coach has
// quietly been answering without their calendar.
func (s *Service) Connect(ctx context.Context, userID uuid.UUID, endpoint, token string) error {
	endpoint = strings.TrimSpace(endpoint)
	if err := validateEndpoint(endpoint); err != nil {
		return err
	}

	if _, err := s.repo.Upsert(ctx, userID, ProviderCalendar, endpoint, strings.TrimSpace(token)); err != nil {
		return err
	}

	if _, err := s.calendar.Upcoming(ctx, endpoint, strings.TrimSpace(token), s.now()); err != nil {
		// Kept rather than rolled back: the credentials are probably right and
		// the server is probably down, and deleting the row would make somebody
		// retype a token to find out. Recorded as failed so the page says so.
		_ = s.repo.MarkChecked(ctx, userID, ProviderCalendar, StatusFailed, userFacing(err))
		return apperr.Wrap(apperr.ErrUnavailable, "connected, but that server did not answer")
	}

	return s.repo.MarkChecked(ctx, userID, ProviderCalendar, StatusOK, "")
}

func (s *Service) Disconnect(ctx context.Context, userID uuid.UUID) error {
	return s.repo.Delete(ctx, userID, ProviderCalendar)
}

// Upcoming is the coach's view: the next seven days as plain strings.
//
// Returns nothing at all, and no error, when there is no connection. Not being
// connected is the normal state, not a failure.
func (s *Service) Upcoming(ctx context.Context, userID uuid.UUID) ([]string, error) {
	conn, err := s.repo.Get(ctx, userID, ProviderCalendar)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if !conn.Connected() {
		return nil, nil
	}

	token, err := s.repo.Token(ctx, userID, ProviderCalendar)
	if err != nil {
		return nil, err
	}

	lines, err := s.calendar.Upcoming(ctx, conn.Endpoint, token, s.now())
	if err != nil {
		// Recorded so the settings page can show it, then returned so the
		// context builder counts it. The reply still happens either way.
		_ = s.repo.MarkChecked(ctx, userID, ProviderCalendar, StatusFailed, userFacing(err))
		return nil, err
	}

	if conn.Status != StatusOK {
		_ = s.repo.MarkChecked(ctx, userID, ProviderCalendar, StatusOK, "")
	}
	return lines, nil
}

// validateEndpoint refuses anything that is not an absolute https URL.
//
// http is allowed only for loopback, which is what makes a locally-run MCP
// server usable in development without letting somebody send their token to a
// plaintext host on the internet.
func validateEndpoint(endpoint string) error {
	if endpoint == "" {
		return apperr.Wrap(apperr.ErrValidation, "Paste the server's address.")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return apperr.Wrap(apperr.ErrValidation, "That does not look like a web address.")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if host := u.Hostname(); host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return apperr.Wrap(apperr.ErrValidation, "Use https: an http address would send your token in the clear.")
	default:
		return apperr.Wrap(apperr.ErrValidation, "That does not look like a web address.")
	}
}

// userFacing is what gets stored in last_error.
//
// Deliberately not the provider's own message: a failing server can echo the
// Authorization header back in its response body, and last_error is rendered on
// a page.
func userFacing(err error) string {
	switch {
	case apperr.Is(err, apperr.ErrValidation):
		return err.Error()
	default:
		return "That server could not be reached."
	}
}
