package mcpauth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	mcpauthdb "github.com/NorthAIProject/north-client/internal/mcpauth/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *mcpauthdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: mcpauthdb.New(pool)}
}

// --- clients ---------------------------------------------------------------

func (r *Repository) InsertClient(ctx context.Context, c Client, dedupeKey []byte) (Client, error) {
	row, err := r.q.InsertOAuthClient(ctx, mcpauthdb.InsertOAuthClientParams{
		ID:           c.ID,
		ClientName:   c.Name,
		RedirectUris: c.RedirectURIs,
		GrantTypes:   c.GrantTypes,
		SoftwareID:   c.SoftwareID,
		DedupeKey:    dedupeKey,
	})
	if err != nil {
		return Client{}, apperr.Wrap(err, "insert oauth client")
	}
	return clientFromDB(row), nil
}

func (r *Repository) ClientByDedupeKey(ctx context.Context, dedupeKey []byte) (Client, error) {
	row, err := r.q.GetOAuthClientByDedupeKey(ctx, dedupeKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Client{}, apperr.ErrNotFound
		}
		return Client{}, apperr.Wrap(err, "get oauth client by dedupe key")
	}
	return clientFromDB(row), nil
}

func (r *Repository) Client(ctx context.Context, id string) (Client, error) {
	row, err := r.q.GetOAuthClient(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Client{}, apperr.ErrNotFound
		}
		return Client{}, apperr.Wrap(err, "get oauth client")
	}
	return clientFromDB(row), nil
}

func (r *Repository) CountClients(ctx context.Context) (int64, error) {
	n, err := r.q.CountOAuthClients(ctx)
	if err != nil {
		return 0, apperr.Wrap(err, "count oauth clients")
	}
	return n, nil
}

func (r *Repository) TouchClient(ctx context.Context, id string) error {
	if err := r.q.TouchOAuthClient(ctx, id); err != nil {
		return apperr.Wrap(err, "touch oauth client")
	}
	return nil
}

// --- authorization requests ------------------------------------------------

// InsertRequest parks a validated authorization request.
func (r *Repository) InsertRequest(ctx context.Context, req Request, challenge, method string, verifierHash []byte) (Request, error) {
	row, err := r.q.InsertAuthorizationRequest(ctx, mcpauthdb.InsertAuthorizationRequestParams{
		ClientID:            req.ClientID,
		RedirectUri:         req.RedirectURI,
		State:               req.State,
		CodeChallenge:       challenge,
		CodeChallengeMethod: method,
		Scope:               req.Scope,
		Resource:            req.Resource,
		VerifierHash:        verifierHash,
		ExpiresAt:           req.ExpiresAt,
	})
	if err != nil {
		return Request{}, apperr.Wrap(err, "insert authorization request")
	}
	return requestFromDB(row), nil
}

// storedRequest is a parked request with the PKCE challenge it recorded.
type storedRequest struct {
	Request

	CodeChallenge       string
	CodeChallengeMethod string
}

func (r *Repository) Request(ctx context.Context, id uuid.UUID, verifierHash []byte) (storedRequest, error) {
	row, err := r.q.GetAuthorizationRequest(ctx, mcpauthdb.GetAuthorizationRequestParams{
		ID:           id,
		VerifierHash: verifierHash,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storedRequest{}, apperr.ErrNotFound
		}
		return storedRequest{}, apperr.Wrap(err, "get authorization request")
	}
	return storedRequest{
		Request:             requestFromDB(row),
		CodeChallenge:       row.CodeChallenge,
		CodeChallengeMethod: row.CodeChallengeMethod,
	}, nil
}

func (r *Repository) MarkRequestAccountCreated(ctx context.Context, id uuid.UUID, verifierHash []byte) error {
	err := r.q.MarkAuthorizationRequestAccountCreated(ctx, mcpauthdb.MarkAuthorizationRequestAccountCreatedParams{
		ID:           id,
		VerifierHash: verifierHash,
	})
	if err != nil {
		return apperr.Wrap(err, "mark authorization request account created")
	}
	return nil
}

// ConsumeRequest closes a request. ErrNotFound means somebody else already
// did, which is what a double-clicked Approve looks like.
func (r *Repository) ConsumeRequest(ctx context.Context, id uuid.UUID, verifierHash []byte) error {
	rows, err := r.q.ConsumeAuthorizationRequest(ctx, mcpauthdb.ConsumeAuthorizationRequestParams{
		ID:           id,
		VerifierHash: verifierHash,
	})
	if err != nil {
		return apperr.Wrap(err, "consume authorization request")
	}
	if rows == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// --- authorization codes ---------------------------------------------------

func (r *Repository) InsertCode(ctx context.Context, code storedCode) error {
	_, err := r.q.InsertAuthorizationCode(ctx, mcpauthdb.InsertAuthorizationCodeParams{
		CodeHash:            code.Hash,
		ClientID:            code.ClientID,
		UserID:              code.UserID,
		RedirectUri:         code.RedirectURI,
		CodeChallenge:       code.CodeChallenge,
		CodeChallengeMethod: code.CodeChallengeMethod,
		Scope:               code.Scope,
		Resource:            code.Resource,
		ExpiresAt:           code.ExpiresAt,
	})
	if err != nil {
		return apperr.Wrap(err, "insert authorization code")
	}
	return nil
}

// storedCode is an authorization code as it is stored. The code itself is
// never held — only its hash.
type storedCode struct {
	Hash     []byte
	ClientID string
	UserID   uuid.UUID

	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	Scope               string
	Resource            string

	ExpiresAt time.Time

	// Consumed is when the code was exchanged, and nil while it is
	// outstanding. A code presented twice is a replay, and the grant it
	// created is revoked — which is what ConnectionID is for.
	Consumed     *time.Time
	ConnectionID *uuid.UUID
}

func (r *Repository) Code(ctx context.Context, hash []byte) (storedCode, error) {
	row, err := r.q.GetAuthorizationCode(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storedCode{}, apperr.ErrNotFound
		}
		return storedCode{}, apperr.Wrap(err, "get authorization code")
	}
	return storedCode{
		Hash:                row.CodeHash,
		ClientID:            row.ClientID,
		UserID:              row.UserID,
		RedirectURI:         row.RedirectUri,
		CodeChallenge:       row.CodeChallenge,
		CodeChallengeMethod: row.CodeChallengeMethod,
		Scope:               row.Scope,
		Resource:            row.Resource,
		ExpiresAt:           row.ExpiresAt,
		Consumed:            row.ConsumedAt,
		ConnectionID:        row.ConnectionID,
	}, nil
}

func (r *Repository) ConsumeCode(ctx context.Context, hash []byte, connectionID uuid.UUID) error {
	rows, err := r.q.ConsumeAuthorizationCode(ctx, mcpauthdb.ConsumeAuthorizationCodeParams{
		CodeHash:     hash,
		ConnectionID: &connectionID,
	})
	if err != nil {
		return apperr.Wrap(err, "consume authorization code")
	}
	if rows == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// --- refresh tokens --------------------------------------------------------

func (r *Repository) InsertRefreshToken(ctx context.Context, connectionID uuid.UUID, hash []byte, expiresAt time.Time) (uuid.UUID, error) {
	row, err := r.q.InsertRefreshToken(ctx, mcpauthdb.InsertRefreshTokenParams{
		ConnectionID: connectionID,
		TokenHash:    hash,
		ExpiresAt:    expiresAt,
	})
	if err != nil {
		return uuid.Nil, apperr.Wrap(err, "insert refresh token")
	}
	return row.ID, nil
}

// storedRefresh is a refresh token row.
type storedRefresh struct {
	ID           uuid.UUID
	ConnectionID uuid.UUID
	ExpiresAt    time.Time
	Consumed     *time.Time
}

func (r *Repository) RefreshToken(ctx context.Context, hash []byte) (storedRefresh, error) {
	row, err := r.q.GetRefreshToken(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storedRefresh{}, apperr.ErrNotFound
		}
		return storedRefresh{}, apperr.Wrap(err, "get refresh token")
	}
	return storedRefresh{
		ID:           row.ID,
		ConnectionID: row.ConnectionID,
		ExpiresAt:    row.ExpiresAt,
		Consumed:     row.ConsumedAt,
	}, nil
}

func (r *Repository) ConsumeRefreshToken(ctx context.Context, hash []byte, replacedBy uuid.UUID) error {
	var replacement *uuid.UUID
	if replacedBy != uuid.Nil {
		replacement = &replacedBy
	}
	rows, err := r.q.ConsumeRefreshToken(ctx, mcpauthdb.ConsumeRefreshTokenParams{
		TokenHash:  hash,
		ReplacedBy: replacement,
	})
	if err != nil {
		return apperr.Wrap(err, "consume refresh token")
	}
	if rows == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// LinkReplaced records which token superseded a spent one, for forensics on a
// stolen-token chain. Best effort at the call site: a missing audit link must
// not fail a refresh that already succeeded.
func (r *Repository) LinkReplaced(ctx context.Context, hash []byte, replacedBy uuid.UUID) error {
	err := r.q.LinkReplacedRefreshToken(ctx, mcpauthdb.LinkReplacedRefreshTokenParams{
		TokenHash:  hash,
		ReplacedBy: &replacedBy,
	})
	if err != nil {
		return apperr.Wrap(err, "link replaced refresh token")
	}
	return nil
}

// ConsumeAllRefreshTokens retires every refresh token a grant holds. Used when
// a replay is detected: the grant is being revoked, and leaving a live refresh
// token behind would let the attacker keep the connection alive.
func (r *Repository) ConsumeAllRefreshTokens(ctx context.Context, connectionID uuid.UUID) error {
	if _, err := r.q.ConsumeRefreshTokensForConnection(ctx, connectionID); err != nil {
		return apperr.Wrap(err, "consume refresh tokens for connection")
	}
	return nil
}

// --- sweeps ----------------------------------------------------------------

// Swept is what one pass of the sweep removed, for the log line.
type Swept struct {
	Requests, Codes, RefreshTokens, Clients int64
}

func (r *Repository) Sweep(ctx context.Context) (Swept, error) {
	var out Swept
	var err error

	if out.Requests, err = r.q.DeleteExpiredAuthorizationRequests(ctx); err != nil {
		return out, apperr.Wrap(err, "sweep authorization requests")
	}
	if out.Codes, err = r.q.DeleteExpiredAuthorizationCodes(ctx); err != nil {
		return out, apperr.Wrap(err, "sweep authorization codes")
	}
	if out.RefreshTokens, err = r.q.DeleteExpiredRefreshTokens(ctx); err != nil {
		return out, apperr.Wrap(err, "sweep refresh tokens")
	}
	if out.Clients, err = r.q.DeleteUnusedOAuthClients(ctx); err != nil {
		return out, apperr.Wrap(err, "sweep unused oauth clients")
	}
	return out, nil
}

// --- mapping ---------------------------------------------------------------

func clientFromDB(row mcpauthdb.McpOauthClient) Client {
	return Client{
		ID:           row.ID,
		Name:         row.ClientName,
		RedirectURIs: row.RedirectUris,
		GrantTypes:   row.GrantTypes,
		SoftwareID:   row.SoftwareID,
		CreatedAt:    row.CreatedAt,
	}
}

func requestFromDB(row mcpauthdb.McpAuthorizationRequest) Request {
	return Request{
		ID:             row.ID,
		ClientID:       row.ClientID,
		RedirectURI:    row.RedirectUri,
		State:          row.State,
		Scope:          row.Scope,
		Resource:       row.Resource,
		AccountCreated: row.AccountCreated,
		ExpiresAt:      row.ExpiresAt,
	}
}
