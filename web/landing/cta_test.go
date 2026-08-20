package landing

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The landing page is public but not stranger-only: `/` renders inside
// LoadUser, so a signed-in visitor used to be told to create the account they
// already had. Clicking it bounced through /signup — which correctly refuses a
// form to an authenticated user — and landed on /app, which reads as being
// logged in automatically.
func TestLandingSendsSignedInVisitorsIntoTheApp(t *testing.T) {
	ctx := auth.ContextWithUser(context.Background(), users.User{DisplayName: "Fernando"})

	var b strings.Builder
	if err := Page().Render(ctx, &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()

	if strings.Contains(out, `href="/signup"`) {
		t.Error("landing offers signup to a signed-in visitor")
	}
	if strings.Contains(out, "Create your account") {
		t.Error("landing tells a signed-in visitor to create an account")
	}
	if !strings.Contains(out, `href="/app"`) {
		t.Error("landing gives a signed-in visitor no way into the app")
	}
	// Sign in is the one link that makes no sense at all when you already are.
	if strings.Contains(out, `href="/login"`) {
		t.Error("landing offers sign-in to a signed-in visitor")
	}
}

// The stranger case is the one that pays the bills, so it is worth pinning that
// the fix above did not quietly hide signup from everyone.
func TestLandingStillSellsToStrangers(t *testing.T) {
	var b strings.Builder
	if err := Page().Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()

	for _, want := range []string{`href="/signup"`, "Create your account", `href="/login"`} {
		if !strings.Contains(out, want) {
			t.Errorf("landing is missing %q for a signed-out visitor", want)
		}
	}
	if strings.Contains(out, `href="/app"`) {
		t.Error("landing points a stranger straight at the app")
	}
}
