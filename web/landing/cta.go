package landing

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/auth"
)

// The landing page is public, but it is not only seen by strangers. `/` is
// mounted inside LoadUser (cmd/web/main.go), so a signed-in visitor arrives
// with a session in context — and until this existed, the page told them to
// create an account they already had. Clicking it bounced through /signup,
// which refuses to serve a form to an authenticated user, and landed them on
// /app: an "automatic login" that was really a redirect.
//
// templ.Handler renders with the request's context, so reading it here is
// enough. Nothing has to be threaded through hero(), finalCTA(), planCard(),
// and the demo panels.

func signedIn(ctx context.Context) bool {
	_, ok := auth.UserFrom(ctx)
	return ok
}

// ctaHref sends a stranger to sign up and a returning user back into the app.
func ctaHref(ctx context.Context) string {
	if signedIn(ctx) {
		return "/app"
	}
	return "/signup"
}

// ctaLabel is the primary call to action. "Create your account" is wrong for
// someone who has one.
func ctaLabel(ctx context.Context) string {
	if signedIn(ctx) {
		return "Open North"
	}
	return "Create your account"
}

// planCTA keeps a pricing card's own wording for strangers — "Start free",
// "Go Pro" — and replaces it for a signed-in visitor, for whom every plan
// button otherwise reads like a second signup.
func planCTA(ctx context.Context, cta string) string {
	if signedIn(ctx) {
		return "Open North"
	}
	return cta
}
