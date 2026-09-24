package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"

	"github.com/NorthAIProject/north-client/internal/analytics"
	authdb "github.com/NorthAIProject/north-client/internal/auth/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

const (
	googleProvider    = "google"
	googleUserInfoURL = "https://www.googleapis.com/oauth2/v3/userinfo"
	googleStateCookie = "north_google_oauth"
	googleStateBytes  = 32
	googleStateTTL    = 10 * time.Minute
	googleHTTPTimeout = 15 * time.Second
)

// GoogleProfile is the subset of Google userinfo used for sign-in.
type GoogleProfile struct {
	Subject string
	Email   string
	Name    string
}

type googleOAuth struct {
	cfg *oauth2.Config
	// nativeClients are the iOS OAuth client IDs a native ID token may be
	// issued for. Google binds an iOS client to one bundle ID, so the Beta and
	// App Store builds each have their own.
	nativeClients []string
}

// newGoogleOAuth configures the web redirect flow and native ID-token sign-in
// independently: either works without the other. nativeClients is a
// comma-separated list.
func newGoogleOAuth(clientID, clientSecret, nativeClients, baseURL string) *googleOAuth {
	clientID = strings.TrimSpace(clientID)
	clientSecret = strings.TrimSpace(clientSecret)
	g := &googleOAuth{nativeClients: splitList(nativeClients)}
	if clientID == "" || clientSecret == "" {
		return g
	}
	g.cfg = &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  strings.TrimRight(baseURL, "/") + "/auth/google/callback",
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
	return g
}

func (s *Service) CompleteGoogleIDToken(ctx context.Context, rawToken string, meta Metadata) (users.User, string, time.Time, error) {
	if s.google == nil || len(s.google.nativeClients) == 0 {
		return users.User{}, "", time.Time{}, apperr.New("google native sign-in is not configured")
	}
	payload, err := s.google.validateNative(ctx, strings.TrimSpace(rawToken))
	if err != nil || payload.Claims["email_verified"] != true {
		return users.User{}, "", time.Time{}, apperr.ErrUnauthenticated
	}
	subject, _ := payload.Claims["sub"].(string)
	email, _ := payload.Claims["email"].(string)
	name, _ := payload.Claims["name"].(string)
	user, err := s.FindOrCreateGoogleUser(ctx, GoogleProfile{Subject: subject, Email: email, Name: name})
	if err != nil {
		return users.User{}, "", time.Time{}, err
	}
	token, expiresAt, err := s.sessions.Create(ctx, user.ID, meta)
	return user, token, expiresAt, err
}

// validateNative accepts an ID token issued for any configured iOS client.
func (g *googleOAuth) validateNative(ctx context.Context, rawToken string) (*idtoken.Payload, error) {
	var err error
	for _, audience := range g.nativeClients {
		var payload *idtoken.Payload
		payload, err = idtoken.Validate(ctx, rawToken, audience)
		if err == nil {
			return payload, nil
		}
	}
	return nil, err
}

func (g *googleOAuth) enabled() bool { return g != nil && g.cfg != nil }

// AuthCodeURL builds the Google consent URL for the given opaque state.
func (s *Service) GoogleAuthCodeURL(state string) (string, error) {
	if !s.GoogleEnabled() {
		return "", apperr.New("google oauth is not configured")
	}
	return s.google.cfg.AuthCodeURL(state, oauth2.AccessTypeOnline, oauth2.SetAuthURLParam("prompt", "select_account")), nil
}

// CompleteGoogleOAuth exchanges the authorization code, loads the Google
// profile, finds or creates a Khepri account, and issues a session token.
func (s *Service) CompleteGoogleOAuth(ctx context.Context, code string, meta Metadata) (users.User, string, error) {
	if !s.GoogleEnabled() {
		return users.User{}, "", apperr.New("google oauth is not configured")
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return users.User{}, "", apperr.New("missing authorization code")
	}

	profile, err := s.google.exchange(ctx, code)
	if err != nil {
		return users.User{}, "", err
	}

	user, err := s.FindOrCreateGoogleUser(ctx, profile)
	if err != nil {
		return users.User{}, "", err
	}

	token, _, err := s.sessions.Create(ctx, user.ID, meta)
	if err != nil {
		return users.User{}, "", err
	}
	return user, token, nil
}

// FindOrCreateGoogleUser resolves a Google profile to a Khepri user:
//  1. existing auth_identities row for provider+subject
//  2. else existing user by email (link identity)
//  3. else create user with null password_hash + identity
func (s *Service) FindOrCreateGoogleUser(ctx context.Context, profile GoogleProfile) (users.User, error) {
	profile.Subject = strings.TrimSpace(profile.Subject)
	profile.Email = strings.ToLower(strings.TrimSpace(profile.Email))
	profile.Name = strings.TrimSpace(profile.Name)

	if profile.Subject == "" {
		return users.User{}, apperr.New("google profile missing subject")
	}
	if profile.Email == "" {
		return users.User{}, apperr.New("google profile missing email")
	}
	if profile.Name == "" {
		// Display name is required by users.ValidateRegistration; fall back to
		// the local part of the email so a sparse Google profile still works.
		if local, _, ok := strings.Cut(profile.Email, "@"); ok && local != "" {
			profile.Name = local
		} else {
			profile.Name = "Khepri user"
		}
	}

	if row, err := s.sessions.q.GetAuthIdentity(ctx, authdb.GetAuthIdentityParams{
		Provider:        googleProvider,
		ProviderSubject: profile.Subject,
	}); err == nil {
		return s.users.ByID(ctx, row.UserID)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return users.User{}, apperr.Wrap(err, "lookup google identity")
	}

	// Link to an existing password account with the same email when possible.
	if existing, err := s.users.ByEmail(ctx, profile.Email); err == nil {
		if linkErr := s.linkGoogleIdentity(ctx, existing.ID, profile); linkErr != nil {
			return users.User{}, linkErr
		}
		return existing, nil
	} else if !apperr.Is(err, apperr.ErrNotFound) {
		return users.User{}, err
	}

	user, err := s.users.Register(ctx, users.Registration{
		Email:       profile.Email,
		DisplayName: profile.Name,
		Timezone:    "UTC",
		// The language the visitor was reading when they pressed Continue with
		// Google. The callback is a request like any other, so middleware.Locale
		// has already resolved one.
		Locale: users.ResolveLocale(i18n.LocaleFrom(ctx)),
		// No password: this account signs in with Google (or a later passkey).
	})
	if err != nil {
		if apperr.Is(err, apperr.ErrConflict) {
			// Race: another request created the email between ByEmail and Register.
			existing, byErr := s.users.ByEmail(ctx, profile.Email)
			if byErr != nil {
				return users.User{}, err
			}
			if linkErr := s.linkGoogleIdentity(ctx, existing.ID, profile); linkErr != nil {
				return users.User{}, linkErr
			}
			return existing, nil
		}
		return users.User{}, err
	}

	if err := s.linkGoogleIdentity(ctx, user.ID, profile); err != nil {
		return users.User{}, err
	}

	// Only here. Every earlier return in this function resolved to an account
	// that already existed — a linked identity, an email match, or the
	// ErrConflict race below the Register call — and counting those would
	// overstate the top of the funnel in exchange for fixing the undercount.
	s.funnel.Registered(ctx, user.ID, analytics.ViaGoogle)

	return user, nil
}

func (s *Service) linkGoogleIdentity(ctx context.Context, userID uuid.UUID, profile GoogleProfile) error {
	email := profile.Email
	_, err := s.sessions.q.CreateAuthIdentity(ctx, authdb.CreateAuthIdentityParams{
		UserID:          userID,
		Provider:        googleProvider,
		ProviderSubject: profile.Subject,
		Email:           &email,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// Already linked (concurrent callback or re-link). Treat as success
			// when the existing row points at the same user.
			row, getErr := s.sessions.q.GetAuthIdentity(ctx, authdb.GetAuthIdentityParams{
				Provider:        googleProvider,
				ProviderSubject: profile.Subject,
			})
			if getErr != nil {
				return apperr.Wrap(err, "link google identity")
			}
			if row.UserID != userID {
				return apperr.Wrap(apperr.ErrConflict, "google account already linked to another user")
			}
			return nil
		}
		return apperr.Wrap(err, "link google identity")
	}
	return nil
}

func (g *googleOAuth) exchange(ctx context.Context, code string) (GoogleProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, googleHTTPTimeout)
	defer cancel()

	tok, err := g.cfg.Exchange(ctx, code)
	if err != nil {
		return GoogleProfile{}, apperr.Wrap(err, "exchange google auth code")
	}

	client := g.cfg.Client(ctx, tok)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleUserInfoURL, nil)
	if err != nil {
		return GoogleProfile{}, apperr.Wrap(err, "build google userinfo request")
	}
	resp, err := client.Do(req)
	if err != nil {
		return GoogleProfile{}, apperr.Wrap(err, "fetch google userinfo")
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return GoogleProfile{}, apperr.Wrap(err, "read google userinfo")
	}
	if resp.StatusCode != http.StatusOK {
		return GoogleProfile{}, apperr.New("google userinfo returned " + resp.Status)
	}

	var raw struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return GoogleProfile{}, apperr.Wrap(err, "decode google userinfo")
	}
	return GoogleProfile{Subject: raw.Sub, Email: raw.Email, Name: raw.Name}, nil
}
