package aicreds

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/providers"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/secret"
)

// cacheTTL bounds how stale a cached client may be.
//
// A write in this process invalidates immediately, so the TTL exists for the
// other processes — cmd/worker has its own cache and never sees the settings
// form. Five minutes is how long a key change can take to reach a background
// job, which is worth saying out loud because it is the one surprising
// consequence of caching at all.
const cacheTTL = 5 * time.Minute

// maxKeyLen bounds what will be accepted as an API key. Generous next to every
// real one, and short enough that the sealed value stays inside the column's
// own limit.
const maxKeyLen = 4096

// hintLen is how much of the key the settings page may echo back.
const hintLen = 4

type cached struct {
	client  ai.Client
	version time.Time
	expires time.Time
}

type Service struct {
	repo *Repository
	log  *slog.Logger

	// sealer is nil when the deployment has no ENCRYPTION_KEY. That is not an
	// error: it means this feature reports itself unavailable rather than
	// storing somebody's key in the clear.
	sealer *secret.Sealer

	// http is shared by every user's client, so a per-user provider reuses
	// connections instead of paying a TLS handshake per coach turn. This is
	// what makes constructing clients outside the registry affordable.
	http *http.Client

	mu    sync.RWMutex
	cache map[uuid.UUID]cached
}

func NewService(repo *Repository, sealer *secret.Sealer, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		repo:   repo,
		log:    log,
		sealer: sealer,
		http:   &http.Client{Timeout: 5 * time.Minute},
		cache:  make(map[uuid.UUID]cached),
	}
}

// Enabled reports whether keys can be stored at all.
func (s *Service) Enabled() bool { return s != nil && s.sealer != nil }

// Providers is the catalogue the settings page offers.
func (s *Service) Providers() []providers.BYOProvider { return providers.Catalog }

// Get returns the stored credential, or ErrNotFound when the user is on
// North's own providers.
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (Credential, error) {
	return s.repo.Get(ctx, userID)
}

// Save stores a credential.
//
// An empty APIKey with a credential already stored is a model-only change; an
// empty APIKey with nothing stored is a validation failure. The key is never
// read back to confirm it, so this is the only moment North sees it.
func (s *Service) Save(ctx context.Context, userID uuid.UUID, in Input) (Credential, error) {
	if !s.Enabled() {
		return Credential{}, apperr.Wrap(apperr.ErrUnavailable, "this deployment cannot store provider keys")
	}

	provider := strings.TrimSpace(in.Provider)
	key := strings.TrimSpace(in.APIKey)
	model := strings.TrimSpace(in.Model)

	var errs apperr.FieldErrors

	entry, known := providers.ByName(provider)
	if !known {
		errs = errs.Add("provider", "Choose one of the providers listed.")
	}
	if utf8.RuneCountInString(key) > maxKeyLen {
		errs = errs.Add("api_key", "That key is longer than any real one; check what was pasted.")
	}
	if utf8.RuneCountInString(model) > 200 {
		errs = errs.Add("model", "That model name is too long.")
	}

	existing, err := s.repo.Get(ctx, userID)
	switch {
	case err == nil:
	case apperr.Is(err, apperr.ErrNotFound):
		if key == "" {
			errs = errs.Add("api_key", "Paste your key from "+entryLabel(entry, provider)+".")
		}
	default:
		return Credential{}, err
	}

	if err := errs.OrNil(); err != nil {
		return Credential{}, err
	}

	defer s.invalidate(userID)

	// Model-only save: the stored key stays where it is, and is not decrypted
	// on the way past — there is no reason for this path to hold it.
	if key == "" && existing.Provider == provider {
		return s.repo.UpdateModel(ctx, userID, model)
	}

	sealed, err := s.sealer.Seal(userID[:], []byte(key))
	if err != nil {
		return Credential{}, apperr.Wrap(err, "seal user ai credential")
	}

	return s.repo.Upsert(ctx, userID, provider, Sealed(sealed), hint(key), model)
}

// Delete removes the credential and puts the user back on North's providers.
func (s *Service) Delete(ctx context.Context, userID uuid.UUID) error {
	defer s.invalidate(userID)
	return s.repo.Delete(ctx, userID)
}

// For satisfies coach.ClientSource.
//
// It returns (nil, nil) when the user has no credential, which is the normal
// case and not an error — that distinction is what the coach's fallback rests
// on. A credential that exists but cannot be turned into a client is an error,
// and the coach logs it and serves from North's chain rather than refusing.
func (s *Service) For(ctx context.Context, userID uuid.UUID) (ai.Client, error) {
	if !s.Enabled() {
		return nil, nil
	}

	cred, sealed, err := s.repo.Key(ctx, userID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	if client, ok := s.cachedClient(userID, cred.UpdatedAt); ok {
		return client, nil
	}

	// Bound to the user id, so a row copied between users fails here rather
	// than serving one person from another's account.
	key, err := s.sealer.Open(userID[:], sealed)
	if err != nil {
		return nil, apperr.Wrap(err, "open user ai credential")
	}

	client, err := providers.User(ctx, providers.UserSpec{
		Provider: cred.Provider, APIKey: string(key), Model: cred.Model, HTTPClient: s.http,
	})
	if err != nil {
		return nil, err
	}

	s.store(userID, client, cred.UpdatedAt)
	return client, nil
}

// NoteFailure records that the user's provider refused, so the settings page
// can say so.
//
// Takes a reason rather than the provider's error: a response body can echo
// the key back, and this string is rendered in a template and stored in a
// column that outlives the request.
func (s *Service) NoteFailure(ctx context.Context, userID uuid.UUID, reason string) {
	if reason == "" {
		return
	}
	if len(reason) > 500 {
		reason = reason[:500]
	}
	if err := s.repo.RecordError(ctx, userID, reason); err != nil {
		s.log.Warn("could not record a provider failure against the user's key",
			slog.String("user_id", userID.String()), slog.Any("error", err))
	}
	s.invalidate(userID)
}

func (s *Service) cachedClient(userID uuid.UUID, version time.Time) (ai.Client, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.cache[userID]
	if !ok || time.Now().After(entry.expires) || !entry.version.Equal(version) {
		return nil, false
	}
	return entry.client, true
}

func (s *Service) store(userID uuid.UUID, client ai.Client, version time.Time) {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Swept here rather than on a ticker: a service with no goroutine behind
	// it cannot leak one, and each entry holds a plaintext key in memory, so
	// the map must not grow without bound.
	for id, entry := range s.cache {
		if now.After(entry.expires) {
			delete(s.cache, id)
		}
	}

	s.cache[userID] = cached{client: client, version: version, expires: now.Add(cacheTTL)}
}

func (s *Service) invalidate(userID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, userID)
}

// hint is the tail of the key, for a page that must prove which key is stored
// without being able to show it.
func hint(key string) string {
	runes := []rune(key)
	if len(runes) <= hintLen {
		return ""
	}
	return string(runes[len(runes)-hintLen:])
}

func entryLabel(entry providers.BYOProvider, fallback string) string {
	if entry.Label != "" {
		return entry.Label
	}
	return fallback
}
