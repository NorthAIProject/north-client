package account

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Service ends accounts.
type Service struct {
	repo    *Repository
	storage Storage
	log     *slog.Logger
}

func NewService(repo *Repository, storage Storage, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, storage: storage, log: log}
}

// ConfirmField names the form field a mismatch is reported against.
const ConfirmField = "confirm_email"

// Delete erases an account. There is no undo, and nothing in North reverses it.
//
// The person types their own email address to get here. Not their password:
// an account can be held entirely by a passkey or by Google, and a confirmation
// step that half the accounts cannot satisfy is not a confirmation step. What
// typing the address buys is the thing a password would not anyway — a moment
// where the hand has to agree with the mouse.
func (s *Service) Delete(ctx context.Context, user users.User, confirmEmail string) (Erasure, error) {
	if !sameEmail(confirmEmail, user.Email) {
		var errs apperr.FieldErrors
		return Erasure{}, errs.Add(ConfirmField,
			"Type your email address exactly as it appears above to confirm.").OrNil()
	}

	// Before anything is deleted, because the keys live on the rows that are
	// about to go and there is no second chance to read them.
	keys, err := s.repo.StorageKeys(ctx, user.ID)
	if err != nil {
		return Erasure{}, err
	}

	eventID, jobs, err := s.repo.Erase(ctx, user.ID, len(keys))
	if err != nil {
		return Erasure{}, err
	}

	// From here the account is already gone and the person is already signed
	// out of every session they had. Nothing below can fail in a way that
	// should be reported to them as a failed deletion.
	result := Erasure{
		UserID:         user.ID,
		StorageObjects: len(keys),
		Jobs:           jobs,
		At:             time.Now().UTC(),
	}
	result.StorageFailures = s.dropObjects(ctx, user.ID, keys)

	if err := s.repo.CloseErasure(ctx, eventID, result); err != nil {
		s.log.Error("could not record what an account erasure removed",
			slog.String("user_id", user.ID.String()), slog.Any("error", err))
	}
	return result, nil
}

// dropObjects removes the account's stored bytes, one key at a time, and
// reports how many refused to go.
//
// The bucket exposes a single-key delete and nothing else, so this is a loop.
// A failure is counted and logged rather than returned: the account is already
// unreachable by the time this runs, and turning a stuck object into an error
// would tell the person their deletion failed when it did not. What it would
// cost instead is the count, which is the only way anyone finds out later that
// bytes were left behind.
func (s *Service) dropObjects(ctx context.Context, userID uuid.UUID, keys []string) int {
	if s.storage == nil {
		return 0
	}

	var failures int
	for _, key := range keys {
		if err := s.storage.Delete(ctx, key); err != nil {
			failures++
			s.log.Error("could not delete a stored object for a deleted account",
				slog.String("user_id", userID.String()),
				slog.String("key", key),
				slog.Any("error", err))
		}
	}
	return failures
}

// RecordExport notes that someone took a copy of their data.
//
// Separate from the export itself, which streams and cannot report a late
// failure, so this is written before the first byte goes out.
func (s *Service) RecordExport(ctx context.Context, userID uuid.UUID) error {
	return s.repo.RecordEvent(ctx, userID, EventExport)
}

// sameEmail compares the typed confirmation with the account's address.
//
// Case-insensitively, because the column is citext and the address the person
// reads on the page is the one they will retype — asking them to match the
// stored casing of something the database itself does not distinguish would be
// a puzzle, not a safeguard.
func sameEmail(typed, actual string) bool {
	return strings.EqualFold(strings.TrimSpace(typed), strings.TrimSpace(actual))
}
