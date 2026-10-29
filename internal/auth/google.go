package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
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
	return s.GoogleAuthCodeURLFor(state, "")
}

// GoogleAuthCodeURLFor is GoogleAuthCodeURL for a front end that serves the
// callback itself (the SvelteKit web app). redirectURI must be a
// /auth/google/callback URL; empty means this server's own. Google refuses a
// redirect URI its console does not list, so the client ID and secret stay
// here and the console needs no change while the callback path is the same.
func (s *Service) GoogleAuthCodeURLFor(state, redirectURI string) (string, error) {
	cfg, err := s.googleConfig(redirectURI)
	if err != nil {
		return "", err
	}
	return cfg.AuthCodeURL(state, oauth2.AccessTypeOnline, oauth2.SetAuthURLParam("prompt", "select_account")), nil
}

// googleConfig is the web OAuth config, pointed at redirectURI when one is
// given. A copy: the shared config is never mutated per request.
func (s *Service) googleConfig(redirectURI string) (*oauth2.Config, error) {
	if !s.GoogleEnabled() {
		return nil, apperr.New("google oauth is not configured")
	}
	cfg := *s.google.cfg
	redirectURI = strings.TrimSpace(redirectURI)
	if redirectURI == "" {
		return &cfg, nil
	}
	if !validGoogleRedirect(redirectURI) {
		return nil, apperr.FieldErrors{}.Add("redirectUri", "redirectUri must be an absolute /auth/google/callback URL.")
	}
	cfg.RedirectURL = redirectURI
	return &cfg, nil
}

// validGoogleRedirect accepts https://<host>/auth/google/callback, and plain
// http only on a loopback host for local development. Google enforces the
// registered list on top of this; the check here keeps an arbitrary URL out
// of the exchange even before Google sees it.
func validGoogleRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Path != "/auth/google/callback" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		host := u.Hostname()
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	default:
		return false
	}
}

// CompleteGoogleOAuth exchanges the authorization code, loads the Google
// profile, finds or creates a Khepri account, and issues a session token.
func (s *Service) CompleteGoogleOAuth(ctx context.Context, code string, meta Metadata) (users.User, string, error) {
	user, token, _, err := s.CompleteGoogleOAuthFor(ctx, code, "", meta)
	return user, token, err
}

// CompleteGoogleOAuthFor is CompleteGoogleOAuth for a code issued to
// redirectURI (see GoogleAuthCodeURLFor), returning the session's expiry too.
// Linking and account creation are FindOrCreateGoogleUser's, unchanged.
func (s *Service) CompleteGoogleOAuthFor(ctx context.Context, code, redirectURI string, meta Metadata) (users.User, string, time.Time, error) {
	cfg, err := s.googleConfig(redirectURI)
	if err != nil {
		return users.User{}, "", time.Time{}, err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return users.User{}, "", time.Time{}, apperr.FieldErrors{}.Add("code", "code is required.")
	}

	profile, err := exchangeGoogleCode(ctx, cfg, code)
	if err != nil {
		return users.User{}, "", time.Time{}, err
	}

	user, err := s.FindOrCreateGoogleUser(ctx, profile)
	if err != nil {
		return users.User{}, "", time.Time{}, err
	}

	token, expiresAt, err := s.sessions.Create(ctx, user.ID, meta)
	if err != nil {
		return users.User{}, "", time.Time{}, err
	}
	return user, token, expiresAt, nil
}

// NewGoogleState is the opaque value a web front end stores beside the
// Google redirect and compares on the callback.
func NewGoogleState() (string, error) { return newOpaqueToken(googleStateBytes) }

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

func exchangeGoogleCode(ctx context.Context, cfg *oauth2.Config, code string) (GoogleProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, googleHTTPTimeout)
	defer cancel()

	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return GoogleProfile{}, apperr.Wrap(err, "exchange google auth code")
	}

	client := cfg.Client(ctx, tok)
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
