package biometrics

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// Input is a measurement as submitted.
type Input struct {
	WeightKg    float64
	HeightCm    float64
	DateOfBirth time.Time
	Sex         string
}

// Validate checks a measurement before it is stored.
func Validate(in Input) (Input, error) {
	var errs apperr.FieldErrors

	switch {
	case in.WeightKg <= 0:
		errs = errs.Add("weight_kg", "Enter a weight.")
	case in.WeightKg < 20 || in.WeightKg > 400:
		errs = errs.Add("weight_kg", "Weight should be between 20 and 400 kg.")
	}

	switch {
	case in.HeightCm <= 0:
		errs = errs.Add("height_cm", "Enter a height.")
	case in.HeightCm < 50 || in.HeightCm > 250:
		errs = errs.Add("height_cm", "Height should be between 50 and 250 cm.")
	}

	switch {
	case in.DateOfBirth.IsZero():
		errs = errs.Add("date_of_birth", "Enter a date of birth.")
	case in.DateOfBirth.After(time.Now()):
		errs = errs.Add("date_of_birth", "That date is in the future.")
	case in.DateOfBirth.Before(time.Now().AddDate(-120, 0, 0)):
		errs = errs.Add("date_of_birth", "That date is more than 120 years ago.")
	}

	in.Sex = strings.TrimSpace(in.Sex)
	if !slices.Contains(Sexes, in.Sex) {
		errs = errs.Add("sex", "Choose one of the listed options.")
	}

	return in, errs.OrNil()
}

// Record stores a new current measurement, retiring the previous one.
func (s *Service) Record(ctx context.Context, userID uuid.UUID, in Input) (Biometric, error) {
	clean, err := Validate(in)
	if err != nil {
		return Biometric{}, err
	}

	return s.repo.RecordCurrent(ctx, userID, clean)
}

// Current returns the most recent measurement. apperr.ErrNotFound until the
// user has recorded one.
func (s *Service) Current(ctx context.Context, userID uuid.UUID) (Biometric, error) {
	return s.repo.Current(ctx, userID)
}

func (s *Service) History(ctx context.Context, userID uuid.UUID, limit int) ([]Biometric, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.repo.History(ctx, userID, limit)
}
