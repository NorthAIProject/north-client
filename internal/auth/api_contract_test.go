package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

var contractUser = auth.APIUser{
	ID:              uuid.MustParse("22222222-2222-2222-2222-222222222222"),
	Email:           "ana@example.com",
	DisplayName:     "Ana",
	Timezone:        "Europe/Lisbon",
	NeedsOnboarding: true,
}

func TestAuthResponseShape(t *testing.T) {
	t.Parallel()

	apitest.AssertGolden(t, "auth.golden.json", auth.AuthResponse{
		Token:     "opaque-session-token",
		ExpiresAt: time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC),
		User:      contractUser,
	})
}

func TestMeResponseShape(t *testing.T) {
	t.Parallel()

	apitest.AssertGolden(t, "me.golden.json", auth.MeResponse{User: contractUser})
}
