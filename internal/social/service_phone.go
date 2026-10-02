package social

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/social/phone"
)

// Phone verification limits. A text costs money and a code is six digits,
// so both sending and guessing are bounded here, below whatever Twilio
// enforces itself.
const (
	// CodeTTL matches how long Twilio Verify keeps a code.
	CodeTTL = 10 * time.Minute
	// MaxStartsPerUserHour bounds texts one account can trigger.
	MaxStartsPerUserHour = 5
	// MaxStartsPerNumberDay bounds texts to one number, whoever asks, so an
	// account cannot be used to flood somebody else's phone.
	MaxStartsPerNumberDay = 5
	// MaxCodeChecks bounds guesses against one text.
	MaxCodeChecks = 5
)

// ErrPhoneRateLimited is a start or a check refused by the limits above or by
// the provider's own.
var ErrPhoneRateLimited = errors.New("too many phone verification attempts")

var codePattern = regexp.MustCompile(`^[0-9]{4,10}$`)

// WithPhoneVerifier turns verified phone numbers on. Without one the feature
// is off: every phone method is not found, and no surface offers it.
func (s *Service) WithPhoneVerifier(v phone.Verifier) *Service { s.verifier = v; return s }

// PhoneEnabled reports whether this deployment can text a code.
func (s *Service) PhoneEnabled() bool { return s.verifier != nil }

func (s *Service) requirePhone() error {
	if s.verifier == nil {
		return apperr.Wrap(apperr.ErrNotFound, "phone verification is not configured")
	}
	return nil
}

// Phone is the account's verified number and any code it is waiting on.
//
// It answers with verification switched off too, so a number stored while it
// was on can still be seen and removed.
func (s *Service) Phone(ctx context.Context, userID uuid.UUID) (Phone, error) {
	var out Phone
	var err error
	if out.Number, out.VerifiedAt, err = s.repo.VerifiedPhone(ctx, userID); err != nil {
		return Phone{}, err
	}
	pending, err := s.repo.PendingVerification(ctx, userID, time.Now().Add(-CodeTTL))
	switch {
	case err == nil:
		out.Pending = pending.Number
	case !apperr.Is(err, apperr.ErrNotFound):
		return Phone{}, err
	}
	return out, nil
}

// StartPhoneVerification texts a code to the number. raw is what was typed;
// dial is the calling code to read a national number in, and may be empty
// when raw starts with + or 00.
func (s *Service) StartPhoneVerification(ctx context.Context, userID uuid.UUID, raw, dial string) (Phone, error) {
	if err := s.requirePhone(); err != nil {
		return Phone{}, err
	}
	number, err := phone.Normalize(raw, dial)
	switch {
	case errors.Is(err, phone.ErrNeedsCountry):
		return Phone{}, apperr.FieldErrors{}.Add("countryCode", "Choose a country, or start the number with +.")
	case err != nil:
		return Phone{}, apperr.FieldErrors{}.Add("phone", "That does not look like a phone number.")
	}
	current, _, err := s.repo.VerifiedPhone(ctx, userID)
	if err != nil {
		return Phone{}, err
	}
	if current == number {
		return Phone{}, apperr.FieldErrors{}.Add("phone", "That number is already verified.")
	}

	now := time.Now()
	id, err := s.repo.ReserveVerification(ctx, userID, number,
		StartLimit{Since: now.Add(-time.Hour), Max: MaxStartsPerUserHour},
		StartLimit{Since: now.Add(-24 * time.Hour), Max: MaxStartsPerNumberDay},
		now.Add(-48*time.Hour))
	if errors.Is(err, errTooManyStarts) {
		return Phone{}, ErrPhoneRateLimited
	}
	if err != nil {
		return Phone{}, err
	}
	if err := s.verifier.Start(ctx, number); err != nil {
		// A text that never went out is not waiting for a code. It still
		// counts toward the limits: the attempt was made.
		if closeErr := s.repo.CloseVerification(ctx, id); closeErr != nil {
			return Phone{}, closeErr
		}
		switch {
		case errors.Is(err, phone.ErrUndeliverable):
			return Phone{}, apperr.FieldErrors{}.Add("phone", "That number cannot receive a text. Check it and try again.")
		case errors.Is(err, phone.ErrTooManyAttempts):
			return Phone{}, ErrPhoneRateLimited
		}
		return Phone{}, apperr.Wrap(err, "start phone verification")
	}
	return s.Phone(ctx, userID)
}

// CheckPhoneCode verifies the waiting number with the code texted to it.
// The number then belongs to this account, and leaves any other that had it.
func (s *Service) CheckPhoneCode(ctx context.Context, userID uuid.UUID, code string) (Phone, error) {
	if err := s.requirePhone(); err != nil {
		return Phone{}, err
	}
	if !codePattern.MatchString(code) {
		return Phone{}, apperr.FieldErrors{}.Add("code", "Enter the code from the text.")
	}
	pending, err := s.repo.PendingVerification(ctx, userID, time.Now().Add(-CodeTTL))
	if apperr.Is(err, apperr.ErrNotFound) {
		return Phone{}, apperr.FieldErrors{}.Add("code", "That code has expired. Send a new one.")
	}
	if err != nil {
		return Phone{}, err
	}
	checks, err := s.repo.CountCheck(ctx, pending.ID)
	if err != nil {
		return Phone{}, err
	}
	if checks > MaxCodeChecks {
		return Phone{}, ErrPhoneRateLimited
	}

	ok, err := s.verifier.Check(ctx, pending.Number, code)
	switch {
	case errors.Is(err, phone.ErrExpired):
		if closeErr := s.repo.CloseVerification(ctx, pending.ID); closeErr != nil {
			return Phone{}, closeErr
		}
		return Phone{}, apperr.FieldErrors{}.Add("code", "That code has expired. Send a new one.")
	case errors.Is(err, phone.ErrTooManyAttempts):
		return Phone{}, ErrPhoneRateLimited
	case err != nil:
		return Phone{}, apperr.Wrap(err, "check phone code")
	case !ok:
		return Phone{}, apperr.FieldErrors{}.Add("code", "That code is not right.")
	}
	if err := s.repo.VerifyPhone(ctx, userID, pending.ID, pending.Number); err != nil {
		return Phone{}, err
	}
	return s.Phone(ctx, userID)
}

// CancelPhoneVerification forgets the code being waited on, so a different
// number can be entered. The verified number, if any, stays.
func (s *Service) CancelPhoneVerification(ctx context.Context, userID uuid.UUID) error {
	if err := s.requirePhone(); err != nil {
		return err
	}
	return s.repo.CloseVerifications(ctx, userID)
}

// RemovePhone removes the verified number: nobody finds the account by it
// any more. It works with verification switched off too: a number already
// stored must always be removable.
func (s *Service) RemovePhone(ctx context.Context, userID uuid.UUID) error {
	if err := s.repo.CloseVerifications(ctx, userID); err != nil {
		return err
	}
	return s.repo.ClearPhone(ctx, userID)
}
