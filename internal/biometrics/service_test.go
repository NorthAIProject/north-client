package biometrics_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func newService(t *testing.T) (*biometrics.Service, users.User) {
	t.Helper()

	pool := testdb.New(t)
	userSvc := users.NewService(users.NewRepository(pool))

	user, err := userSvc.Register(context.Background(), users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	return biometrics.NewService(biometrics.NewRepository(pool)), user
}

func validInput() biometrics.Input {
	return biometrics.Input{
		WeightKg:    80,
		HeightCm:    180,
		DateOfBirth: time.Now().AddDate(-30, 0, 0),
		Sex:         biometrics.SexMale,
	}
}

func TestRecordAndCurrent(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	recorded, err := svc.Record(ctx, user.ID, validInput())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if !recorded.IsCurrent {
		t.Fatal("a newly recorded measurement should be current")
	}

	current, err := svc.Current(ctx, user.ID)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current.WeightKg != 80 {
		t.Fatalf("weight_kg = %v", current.WeightKg)
	}
}

func TestCurrentIsNotFoundBeforeAnyRecord(t *testing.T) {
	svc, user := newService(t)

	if _, err := svc.Current(context.Background(), user.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRecordRetiresThePreviousCurrentRow(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	if _, err := svc.Record(ctx, user.ID, validInput()); err != nil {
		t.Fatalf("first record: %v", err)
	}

	second := validInput()
	second.WeightKg = 82
	if _, err := svc.Record(ctx, user.ID, second); err != nil {
		t.Fatalf("second record: %v", err)
	}

	current, err := svc.Current(ctx, user.ID)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current.WeightKg != 82 {
		t.Fatalf("current weight_kg = %v, want the second measurement", current.WeightKg)
	}

	history, err := svc.History(ctx, user.ID, 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected both measurements in history, got %d", len(history))
	}
}

func TestValidationRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field string
		in    biometrics.Input
	}{
		{"weight too low", "weight_kg", func() biometrics.Input { in := validInput(); in.WeightKg = 5; return in }()},
		{"height too high", "height_cm", func() biometrics.Input { in := validInput(); in.HeightCm = 300; return in }()},
		{"future date of birth", "date_of_birth", func() biometrics.Input { in := validInput(); in.DateOfBirth = time.Now().AddDate(0, 0, 1); return in }()},
		{"unknown sex", "sex", func() biometrics.Input { in := validInput(); in.Sex = "unspecified"; return in }()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := biometrics.Validate(tt.in)
			if err == nil {
				t.Fatalf("%s should be rejected", tt.name)
			}

			var fieldErrs apperr.FieldErrors
			if !apperr.As(err, &fieldErrs) {
				t.Fatalf("expected field errors, got %T", err)
			}
			if _, ok := fieldErrs.Messages()[tt.field]; !ok {
				t.Fatalf("expected the failure on %q, got %v", tt.field, fieldErrs.Messages())
			}
		})
	}
}

func TestBMIAndAgeAreComputedFromStoredValues(t *testing.T) {
	b := biometrics.Biometric{
		WeightKg:    80,
		HeightCm:    200,
		DateOfBirth: time.Date(1990, time.January, 1, 0, 0, 0, 0, time.UTC),
	}

	if got, want := b.BMI(), 20.0; got != want {
		t.Fatalf("BMI = %v, want %v", got, want)
	}

	age := b.AgeYears(time.Date(2020, time.February, 1, 0, 0, 0, 0, time.UTC))
	if age != 30 {
		t.Fatalf("age = %d, want 30", age)
	}
}
