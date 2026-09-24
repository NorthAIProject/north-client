package onboarding_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/onboarding"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestOnboardingResponseShape(t *testing.T) {
	t.Parallel()

	apitest.AssertGolden(t, "onboarding.golden.json", onboarding.Response{
		User: auth.APIUser{
			ID:          uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			Email:       "ana@example.com",
			DisplayName: "Ana",
			Timezone:    "Europe/Lisbon",
		},
		ThreadID: "33333333-3333-3333-3333-333333333333",
	})
}
