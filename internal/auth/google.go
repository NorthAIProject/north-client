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

	authdb "github.com/NorthAIProject/north-client/internal/auth/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
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
}

func newGoogleOAuth(clientID, clientSecret, baseURL string) *googleOAuth {
	clientID = strings.TrimSpace(clientID)
	clientSecret = strings.TrimSpace(clientSecret)
	if clientID == "" || clientSecret == "" {
		return &googleOAuth{}
	}
	return &googleOAuth{
		cfg: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  strings.TrimRight(baseURL, "/") + "/auth/google/callback",
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		},
	}
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
// profile, finds or creates a North account, and issues a session token.
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

// FindOrCreateGoogleUser resolves a Google profile to a North user:
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
			profile.Name = "North user"
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
