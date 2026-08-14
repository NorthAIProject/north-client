package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	authdb "github.com/NorthAIProject/north-client/internal/auth/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

const (
	webauthnChallengeTTL = 5 * time.Minute
)

// passkeyAuth wraps go-webauthn and challenge persistence.
type passkeyAuth struct {
	wa  *webauthn.WebAuthn
	log *slog.Logger
}

func newPasskeyAuth(sessions *SessionStore, opts ServiceOptions, baseURL string, log *slog.Logger) *passkeyAuth {
	_ = sessions // challenges live on SessionStore's queries via Service

	rpID := relyingPartyID(opts.WebAuthnRPID, baseURL)

	origins := opts.WebAuthnRPOrigins
	if len(origins) == 0 {
		origins = []string{baseURL}
	}
	display := strings.TrimSpace(opts.WebAuthnDisplayName)
	if display == "" {
		display = "North"
	}

	wa, err := webauthn.New(&webauthn.Config{
		RPDisplayName: display,
		RPID:          rpID,
		RPOrigins:     origins,
	})
	if err != nil {
		log.Error("webauthn config invalid; passkeys disabled", slog.Any("error", err))
		return &passkeyAuth{log: log}
	}
	return &passkeyAuth{wa: wa, log: log}
}

func (p *passkeyAuth) enabled() bool { return p != nil && p.wa != nil }

// relyingPartyID is the WebAuthn RP ID: a hostname, never a URL.
//
// Inline comments in .env (`WEBAUTHN_RP_ID=  # defaults to host`) survive
// godotenv and become the value. A string starting with # is a URL fragment,
// which go-webauthn rejects with "the fragment component must be empty".
func relyingPartyID(configured, baseURL string) string {
	rpID := strings.TrimSpace(configured)
	if i := strings.Index(rpID, "#"); i >= 0 {
		rpID = strings.TrimSpace(rpID[:i])
	}
	if u, err := url.Parse(rpID); err == nil && u.Hostname() != "" {
		rpID = u.Hostname()
	}
	if rpID == "" {
		if u, err := url.Parse(baseURL); err == nil {
			rpID = u.Hostname()
		}
	}
	if rpID == "" {
		return "localhost"
	}
	return rpID
}

// PasskeyRegisterBeginInput is the body for POST /auth/passkey/register/begin.
type PasskeyRegisterBeginInput struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Timezone    string `json:"timezone"`
}

// PasskeyCeremony is returned to the browser for navigator.credentials.
type PasskeyCeremony struct {
	ChallengeID string `json:"challenge_id"`
	// PublicKey is the WebAuthn options object (creation or request).
	PublicKey any `json:"publicKey"`
}

// PasskeyRegisterBegin validates signup fields, starts a WebAuthn registration
// ceremony, and returns options for the browser. No user row is written yet.
func (s *Service) PasskeyRegisterBegin(ctx context.Context, in PasskeyRegisterBeginInput) (PasskeyCeremony, error) {
	if !s.PasskeyEnabled() {
		return PasskeyCeremony{}, apperr.New("passkeys are not configured")
	}

	// Pre-generate the account id so the authenticator stores a stable user
	// handle that we can look up on discoverable-credential login.
	userID := uuid.New()
	reg := users.Registration{
		ID:          userID,
		Email:       in.Email,
		DisplayName: in.DisplayName,
		Timezone:    in.Timezone,
	}
	clean, err := s.users.ValidateRegistration(reg)
	if err != nil {
		return PasskeyCeremony{}, err
	}

	// Surface email conflicts before the browser ceremony so the user does not
	// complete a passkey only to learn the address is taken.
	existing, byEmailErr := s.users.ByEmail(ctx, clean.Email)
	if byEmailErr == nil && existing.ID != uuid.Nil {
		return PasskeyCeremony{}, apperr.FieldErrors{{
			Field:   "email",
			Message: "An account with that email already exists.",
		}}
	} else if byEmailErr != nil && !apperr.Is(byEmailErr, apperr.ErrNotFound) {
		return PasskeyCeremony{}, byEmailErr
	}

	waUser := &webAuthnUser{
		id:          userID[:],
		name:        clean.Email,
		displayName: clean.DisplayName,
	}

	creation, sessionData, err := s.webauthn.wa.BeginRegistration(waUser,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationPreferred,
		}),
	)
	if err != nil {
		return PasskeyCeremony{}, apperr.Wrap(err, "begin passkey registration")
	}

	payload := challengePayload{
		Kind:        "register",
		SessionData: *sessionData,
		UserID:      userID,
		Email:       clean.Email,
		DisplayName: clean.DisplayName,
		Timezone:    clean.Timezone,
	}
	challengeID, err := s.storeChallenge(ctx, payload)
	if err != nil {
		return PasskeyCeremony{}, err
	}

	return PasskeyCeremony{
		ChallengeID: challengeID.String(),
		PublicKey:   creation.Response,
	}, nil
}

// PasskeyRegisterFinishInput completes registration.
type PasskeyRegisterFinishInput struct {
	ChallengeID string          `json:"challenge_id"`
	Credential  json.RawMessage `json:"credential"`
}

// PasskeyRegisterFinish verifies the attestation, creates the user and
// credential, and issues a session.
func (s *Service) PasskeyRegisterFinish(ctx context.Context, in PasskeyRegisterFinishInput, meta Metadata) (users.User, string, error) {
	if !s.PasskeyEnabled() {
		return users.User{}, "", apperr.New("passkeys are not configured")
	}

	payload, err := s.takeChallenge(ctx, in.ChallengeID)
	if err != nil {
		return users.User{}, "", err
	}
	if payload.Kind != "register" {
		return users.User{}, "", apperr.New("invalid passkey ceremony")
	}

	waUser := &webAuthnUser{
		id:          payload.UserID[:],
		name:        payload.Email,
		displayName: payload.DisplayName,
	}

	parsed, err := protocol.ParseCredentialCreationResponseBody(strings.NewReader(string(in.Credential)))
	if err != nil {
		return users.User{}, "", apperr.Wrap(err, "parse registration credential")
	}

	cred, err := s.webauthn.wa.CreateCredential(waUser, payload.SessionData, parsed)
	if err != nil {
		return users.User{}, "", apperr.Wrap(err, "verify registration")
	}

	user, err := s.users.Register(ctx, users.Registration{
		ID:          payload.UserID,
		Email:       payload.Email,
		DisplayName: payload.DisplayName,
		Timezone:    payload.Timezone,
	})
	if err != nil {
		if apperr.Is(err, apperr.ErrConflict) {
			return users.User{}, "", apperr.FieldErrors{{
				Field:   "email",
				Message: "An account with that email already exists.",
			}}
		}
		return users.User{}, "", err
	}

	if saveErr := s.saveCredential(ctx, user.ID, cred, "Passkey"); saveErr != nil {
		return users.User{}, "", saveErr
	}

	token, _, err := s.sessions.Create(ctx, user.ID, meta)
	if err != nil {
		return users.User{}, "", err
	}
	return user, token, nil
}

// PasskeyLoginBeginInput optionally hints which account is signing in.
// Empty email starts a discoverable-credential ceremony.
type PasskeyLoginBeginInput struct {
	Email string `json:"email"`
}

// PasskeyLoginBegin starts an authentication ceremony.
func (s *Service) PasskeyLoginBegin(ctx context.Context, in PasskeyLoginBeginInput) (PasskeyCeremony, error) {
	if !s.PasskeyEnabled() {
		return PasskeyCeremony{}, apperr.New("passkeys are not configured")
	}

	email := strings.ToLower(strings.TrimSpace(in.Email))
	var (
		assertion   *protocol.CredentialAssertion
		sessionData *webauthn.SessionData
		err         error
		userID      uuid.UUID
	)

	if email != "" {
		user, byErr := s.users.ByEmail(ctx, email)
		if byErr != nil {
			if apperr.Is(byErr, apperr.ErrNotFound) {
				// Do not reveal whether the email exists; start a discoverable
				// ceremony that will fail at finish if there is no credential.
				assertion, sessionData, err = s.webauthn.wa.BeginDiscoverableLogin(
					webauthn.WithUserVerification(protocol.VerificationPreferred),
				)
			} else {
				return PasskeyCeremony{}, byErr
			}
		} else {
			creds, listErr := s.listCredentials(ctx, user.ID)
			if listErr != nil {
				return PasskeyCeremony{}, listErr
			}
			waUser := &webAuthnUser{
				id:          user.ID[:],
				name:        user.Email,
				displayName: user.DisplayName,
				credentials: creds,
			}
			assertion, sessionData, err = s.webauthn.wa.BeginLogin(waUser,
				webauthn.WithUserVerification(protocol.VerificationPreferred),
			)
			userID = user.ID
		}
	} else {
		assertion, sessionData, err = s.webauthn.wa.BeginDiscoverableLogin(
			webauthn.WithUserVerification(protocol.VerificationPreferred),
		)
	}
	if err != nil {
		return PasskeyCeremony{}, apperr.Wrap(err, "begin passkey login")
	}

	payload := challengePayload{
		Kind:        "login",
		SessionData: *sessionData,
		UserID:      userID,
		Email:       email,
	}
	challengeID, err := s.storeChallenge(ctx, payload)
	if err != nil {
		return PasskeyCeremony{}, err
	}

	return PasskeyCeremony{
		ChallengeID: challengeID.String(),
		PublicKey:   assertion.Response,
	}, nil
}

// PasskeyLoginFinishInput completes authentication.
type PasskeyLoginFinishInput struct {
	ChallengeID string          `json:"challenge_id"`
	Credential  json.RawMessage `json:"credential"`
}

// PasskeyLoginFinish verifies the assertion and issues a session.
func (s *Service) PasskeyLoginFinish(ctx context.Context, in PasskeyLoginFinishInput, meta Metadata) (users.User, string, error) {
	if !s.PasskeyEnabled() {
		return users.User{}, "", apperr.New("passkeys are not configured")
	}

	payload, err := s.takeChallenge(ctx, in.ChallengeID)
	if err != nil {
		return users.User{}, "", err
	}
	if payload.Kind != "login" {
		return users.User{}, "", apperr.New("invalid passkey ceremony")
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(strings.NewReader(string(in.Credential)))
	if err != nil {
		return users.User{}, "", apperr.Wrap(err, "parse login credential")
	}

	var (
		user users.User
		cred *webauthn.Credential
	)

	// Discoverable login: user handle in the assertion identifies the account.
	if payload.UserID == uuid.Nil {
		handler := func(rawID, userHandle []byte) (webauthn.User, error) {
			id, parseErr := uuidFromBytes(userHandle)
			if parseErr != nil {
				return nil, parseErr
			}
			u, byErr := s.users.ByID(ctx, id)
			if byErr != nil {
				return nil, byErr
			}
			user = u
			creds, listErr := s.listCredentials(ctx, u.ID)
			if listErr != nil {
				return nil, listErr
			}
			return &webAuthnUser{
				id:          u.ID[:],
				name:        u.Email,
				displayName: u.DisplayName,
				credentials: creds,
			}, nil
		}
		cred, err = s.webauthn.wa.ValidateDiscoverableLogin(handler, payload.SessionData, parsed)
		if err != nil {
			return users.User{}, "", apperr.Wrap(ErrInvalidCredentials, "passkey login failed")
		}
	} else {
		u, byErr := s.users.ByID(ctx, payload.UserID)
		if byErr != nil {
			return users.User{}, "", byErr
		}
		user = u
		creds, listErr := s.listCredentials(ctx, user.ID)
		if listErr != nil {
			return users.User{}, "", listErr
		}
		waUser := &webAuthnUser{
			id:          user.ID[:],
			name:        user.Email,
			displayName: user.DisplayName,
			credentials: creds,
		}
		cred, err = s.webauthn.wa.ValidateLogin(waUser, payload.SessionData, parsed)
		if err != nil {
			return users.User{}, "", apperr.Wrap(ErrInvalidCredentials, "passkey login failed")
		}
	}

	if err = s.touchCredential(ctx, cred); err != nil {
		s.log.ErrorContext(ctx, "update passkey sign count", slog.Any("error", err))
	}

	token, _, err := s.sessions.Create(ctx, user.ID, meta)
	if err != nil {
		return users.User{}, "", err
	}
	return user, token, nil
}

// ---------------------------------------------------------------------------
// Persistence helpers
// ---------------------------------------------------------------------------

type challengePayload struct {
	Kind        string               `json:"kind"`
	SessionData webauthn.SessionData `json:"session_data"`
	UserID      uuid.UUID            `json:"user_id"`
	Email       string               `json:"email"`
	DisplayName string               `json:"display_name"`
	Timezone    string               `json:"timezone"`
}

func (s *Service) storeChallenge(ctx context.Context, payload challengePayload) (uuid.UUID, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, apperr.Wrap(err, "encode webauthn challenge")
	}
	row, err := s.sessions.q.CreateWebAuthnChallenge(ctx, authdb.CreateWebAuthnChallengeParams{
		Data:      raw,
		ExpiresAt: time.Now().Add(webauthnChallengeTTL),
	})
	if err != nil {
		return uuid.Nil, apperr.Wrap(err, "store webauthn challenge")
	}
	return row.ID, nil
}

func (s *Service) takeChallenge(ctx context.Context, idStr string) (challengePayload, error) {
	id, err := uuid.Parse(strings.TrimSpace(idStr))
	if err != nil {
		return challengePayload{}, apperr.New("invalid or expired passkey challenge")
	}
	row, err := s.sessions.q.GetWebAuthnChallenge(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return challengePayload{}, apperr.New("invalid or expired passkey challenge")
		}
		return challengePayload{}, apperr.Wrap(err, "load webauthn challenge")
	}
	// Single-use: delete before verification so concurrent finishes cannot both succeed.
	_ = s.sessions.q.DeleteWebAuthnChallenge(ctx, id)

	var payload challengePayload
	if err := json.Unmarshal(row.Data, &payload); err != nil {
		return challengePayload{}, apperr.Wrap(err, "decode webauthn challenge")
	}
	return payload, nil
}

func (s *Service) listCredentials(ctx context.Context, userID uuid.UUID) ([]webauthn.Credential, error) {
	rows, err := s.sessions.q.ListWebAuthnCredentialsByUser(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list passkeys")
	}
	out := make([]webauthn.Credential, 0, len(rows))
	for _, row := range rows {
		out = append(out, credentialFromDB(row))
	}
	return out, nil
}

func (s *Service) saveCredential(ctx context.Context, userID uuid.UUID, cred *webauthn.Credential, name string) error {
	if cred == nil {
		return apperr.New("missing credential")
	}
	transports := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		transports = append(transports, string(t))
	}
	var aaguid []byte
	if len(cred.Authenticator.AAGUID) > 0 {
		aaguid = cred.Authenticator.AAGUID
	}
	_, err := s.sessions.q.CreateWebAuthnCredential(ctx, authdb.CreateWebAuthnCredentialParams{
		CredentialID:    cred.ID,
		UserID:          userID,
		PublicKey:       cred.PublicKey,
		AttestationType: cred.AttestationType,
		Transport:       transports,
		SignCount:       int64(cred.Authenticator.SignCount),
		Name:            name,
		Aaguid:          aaguid,
		BackupEligible:  cred.Flags.BackupEligible,
		BackupState:     cred.Flags.BackupState,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperr.Wrap(apperr.ErrConflict, "passkey already registered")
		}
		return apperr.Wrap(err, "store passkey")
	}
	return nil
}

func (s *Service) touchCredential(ctx context.Context, cred *webauthn.Credential) error {
	if cred == nil {
		return nil
	}
	return apperr.Wrap(s.sessions.q.UpdateWebAuthnCredentialSignCount(ctx, authdb.UpdateWebAuthnCredentialSignCountParams{
		CredentialID:   cred.ID,
		SignCount:      int64(cred.Authenticator.SignCount),
		BackupEligible: cred.Flags.BackupEligible,
		BackupState:    cred.Flags.BackupState,
	}), "update passkey")
}

func credentialFromDB(row authdb.WebauthnCredential) webauthn.Credential {
	transports := make([]protocol.AuthenticatorTransport, 0, len(row.Transport))
	for _, t := range row.Transport {
		transports = append(transports, protocol.AuthenticatorTransport(t))
	}
	return webauthn.Credential{
		ID:              row.CredentialID,
		PublicKey:       row.PublicKey,
		AttestationType: row.AttestationType,
		Transport:       transports,
		Flags: webauthn.CredentialFlags{
			BackupEligible: row.BackupEligible,
			BackupState:    row.BackupState,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:    row.Aaguid,
			SignCount: uint32(row.SignCount),
		},
	}
}

// webAuthnUser implements webauthn.User for a North account.
type webAuthnUser struct {
	id          []byte
	name        string
	displayName string
	credentials []webauthn.Credential
}

func (u *webAuthnUser) WebAuthnID() []byte                         { return u.id }
func (u *webAuthnUser) WebAuthnName() string                       { return u.name }
func (u *webAuthnUser) WebAuthnDisplayName() string                { return u.displayName }
func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func uuidFromBytes(b []byte) (uuid.UUID, error) {
	if len(b) != 16 {
		return uuid.Nil, apperr.New("invalid webauthn user handle")
	}
	var id uuid.UUID
	copy(id[:], b)
	return id, nil
}
